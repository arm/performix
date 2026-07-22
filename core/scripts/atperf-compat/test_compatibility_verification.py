# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import patch

MODULE_DIR = Path(__file__).resolve().parent
CV_SPEC = importlib.util.spec_from_file_location(
    "compatibility_verification",
    MODULE_DIR / "compatibility_verification.py"
)
cv = importlib.util.module_from_spec(CV_SPEC)
CV_SPEC.loader.exec_module(cv)


class LoadMatrixManifestTests(unittest.TestCase):
    def test_load_matrix_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            matrix_path = Path(tmp) / "matrix.json"
            matrix_path.write_text(json.dumps({"compatibility": {"recipe": {}}}))
            result = cv.load_matrix(matrix_path)
            self.assertEqual(result, {"recipe": {}})

    def test_load_matrix_missing_file_exits(self):
        with tempfile.TemporaryDirectory() as tmp, \
                self.assertRaises(SystemExit) as ctx, \
                patch.object(cv.logger, "error") as mock_error:
            cv.load_matrix(Path(tmp) / "missing.json")
        self.assertEqual(ctx.exception.code, 1)
        mock_error.assert_called_once()

    def test_load_matrix_bad_json_exits(self):
        with tempfile.TemporaryDirectory() as tmp, \
                self.assertRaises(SystemExit), \
                patch.object(cv.logger, "error"):
            path = Path(tmp) / "matrix.json"
            path.write_text("{bad json")
            cv.load_matrix(path)

    def test_load_manifest_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            manifest_path = Path(tmp) / "manifest.json"
            manifest_path.write_text(json.dumps([{"run_id": "1"}]))
            result = cv.load_manifest(manifest_path)
            self.assertEqual(result, [{"run_id": "1"}])

    def test_load_manifest_bad_json(self):
        with tempfile.TemporaryDirectory() as tmp, \
                self.assertRaises(SystemExit), \
                patch.object(cv.logger, "error"):
            path = Path(tmp) / "manifest.json"
            path.write_text("not json")
            cv.load_manifest(path)


class MainPathResolutionTests(unittest.TestCase):
    def test_missing_matrix_exits(self):
        with tempfile.TemporaryDirectory() as tmp:
            hist_dir = Path(tmp) / "hist"
            hist_dir.mkdir()
            atperf = Path(tmp) / "apap-cli" / "atperf"
            atperf.parent.mkdir(parents=True, exist_ok=True)
            atperf.write_text("#!/bin/true")
            atperf.chmod(0o755)
            migrations = Path(tmp) / "migrations.json"
            migrations.write_text("[]")

            args = [
                "prog",
                "--historical", str(hist_dir),
                "--matrix", str(Path(tmp) / "missing.json"),
                "--migrations", str(migrations),
                "--atperf-bin", str(atperf),
                "--dry-run",
            ]

            with patch("sys.argv", args), \
                    self.assertRaises(SystemExit) as ctx, \
                    patch.object(cv.logger, "error") as mock_error:
                cv.main()

            self.assertEqual(ctx.exception.code, 1)
            self.assertTrue(mock_error.called)


class RenderSessionCleanupTests(unittest.TestCase):
    def _invoke_verifier(
            self,
            minimum_version,
            render_returncode,
            render_stdout=None,
            render_stderr="",
            close_returncode=0,
            close_stderr=""):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            hist_dir = tmp_path / "historical"
            version_dir = hist_dir / "1.0.0"
            version_dir.mkdir(parents=True)
            (version_dir / "manifest.json").write_text(json.dumps([{
                "recipe": "code_hotspots",
                "run_id": "abc123",
                "zip_path": "abc123.zip",
            }]))

            matrix = tmp_path / "matrix.json"
            matrix.write_text(json.dumps({
                "compatibility": {
                    "code_hotspots": {
                        "2.0.0": {"minimum_version": minimum_version},
                    },
                },
            }))
            migrations = tmp_path / "migrations.json"
            migrations.write_text("[]")
            atperf = tmp_path / "apx"
            atperf.write_text("#!/bin/true")
            resolved_atperf = str(atperf.resolve(strict=False))
            args = [
                "prog",
                "--historical", str(hist_dir),
                "--matrix", str(matrix),
                "--migrations", str(migrations),
                "--atperf-bin", str(atperf),
            ]
            if render_stdout is None:
                render_stdout = json.dumps({"data": {"invocation": {"session_id": "session-1"}}})
            completed = [
                CompletedProcess([str(atperf), "version"], 0, stdout="version: 2.0.0\n", stderr=""),
                CompletedProcess([], 0, stdout="", stderr=""),
                CompletedProcess([], render_returncode, stdout=render_stdout, stderr=render_stderr),
                CompletedProcess([], close_returncode, stdout="", stderr=close_stderr),
            ]

            with patch("sys.argv", args), \
                    patch.object(cv.subprocess, "run", side_effect=completed) as mock_run, \
                    self.assertRaises(SystemExit) as ctx:
                cv.main()

            self.assertEqual(ctx.exception.code, 0)
            return [call.args[0] for call in mock_run.call_args_list], resolved_atperf

    def test_successful_render_session_is_closed(self):
        commands, atperf = self._invoke_verifier(
            minimum_version="1.0.0",
            render_returncode=0,
        )

        self.assertEqual(commands[2], [atperf, "run", "render", "abc123", "--json"])
        self.assertEqual(commands[3], [atperf, "render", "close", "session-1"])

    def test_expected_failed_render_session_is_closed(self):
        commands, atperf = self._invoke_verifier(
            minimum_version="2.0.0",
            render_returncode=1,
            render_stderr="render failed",
        )

        self.assertEqual(commands[3], [atperf, "render", "close", "session-1"])

    def test_successful_render_without_session_id_logs_and_continues(self):
        with patch.object(cv.logger, "warning") as mock_warning:
            commands, _ = self._invoke_verifier(
                minimum_version="1.0.0",
                render_returncode=0,
                render_stdout=json.dumps({"data": {"invocation": {}}}),
            )

        self.assertEqual(len(commands), 3)
        mock_warning.assert_called_with(
            "No render session id found for run %s; skipping render session cleanup",
            "abc123",
        )

    def test_render_close_failure_logs_and_continues(self):
        with patch.object(cv.logger, "warning") as mock_warning:
            commands, atperf = self._invoke_verifier(
                minimum_version="1.0.0",
                render_returncode=0,
                close_returncode=1,
                close_stderr="close failed",
            )

        self.assertEqual(commands[3], [atperf, "render", "close", "session-1"])
        mock_warning.assert_called_with(
            "Failed to close render session %s for run %s: stderr=%s stdout=%s",
            "session-1",
            "abc123",
            "close failed",
            "<no stdout>",
        )


if __name__ == "__main__":
    unittest.main()
