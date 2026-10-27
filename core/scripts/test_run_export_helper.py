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
HELPER_SPEC = importlib.util.spec_from_file_location(
    "run_export_helper",
    MODULE_DIR / "run_export_helper.py"
)
helper = importlib.util.module_from_spec(HELPER_SPEC)
HELPER_SPEC.loader.exec_module(helper)


class RunCliTests(unittest.TestCase):
    def test_run_cli_success(self):
        fake_process = CompletedProcess(args=["x"], returncode=0, stdout="out", stderr="")
        with patch.object(helper.subprocess, "run", return_value=fake_process) as mock_run:
            result = helper.run_cli(["x"], Path("/tmp"))
        self.assertIs(result, fake_process)
        mock_run.assert_called_once_with(["x"], cwd=Path("/tmp"), env=None, timeout=None, capture_output=True, text=True)

    def test_run_cli_failure_includes_context(self):
        fake_process = CompletedProcess(args=["x"], returncode=7, stdout="out", stderr="err")
        with patch.object(helper.subprocess, "run", return_value=fake_process), \
                self.assertRaises(helper.CommandFailure) as ctx:
            helper.run_cli(["x"], Path("/tmp"))
        self.assertIn("exit code 7", str(ctx.exception))
        self.assertIn("STDOUT:\nout", str(ctx.exception))
        self.assertIn("STDERR:\nerr", str(ctx.exception))


class ImportRunTests(unittest.TestCase):
    def test_import_and_source_update_use_selected_environment_and_timeout(self):
        cli, archive, source = Path("/tmp/apx"), Path("fixture.zip"), Path("source")
        env = {"APXD_CONFIG_DIR": "private"}
        result = CompletedProcess([], 0, stdout='{"data":{"new_id":{"value":"run-123"}}}')
        with patch.object(helper.subprocess, "run", return_value=result) as run:
            run_id = helper.import_run(cli, archive, env=env, timeout=120)
            helper.update_run_source(cli, run_id, source, env=env, timeout=120)
        self.assertEqual(run_id, "run-123")
        self.assertEqual([call.args[0] for call in run.call_args_list], [
            [str(cli), "run", "import", str(archive), "--json"],
            [str(cli), "run", "update", "run-123", "--source", str(source)],
        ])
        for call in run.call_args_list:
            self.assertEqual(call.kwargs, dict(cwd=cli.parent, env=env, timeout=120, capture_output=True, text=True))

    def test_import_rejects_invalid_json_and_missing_run_id(self):
        outputs = ["not json", "null", "{}"] + [
            json.dumps({"data": {"new_id": {"value": value}}}) for value in (None, "", 123)
        ]
        for output in outputs:
            with self.subTest(output=output), patch.object(helper, "run_cli", return_value=CompletedProcess([], 0, stdout=output)):
                with self.assertRaisesRegex(ValueError, "apx run import --json output"):
                    helper.import_run(Path("apx"), Path("fixture.zip"))


class ParseRecipeRunIDTests(unittest.TestCase):
    def test_parse_text_run_id(self):
        self.assertEqual(helper.parse_recipe_run_id("Run ID: abc-123"), "abc-123")

    def test_parse_missing_run_id(self):
        with self.assertRaises(ValueError):
            helper.parse_recipe_run_id("no run id")


class GetCliVersionTests(unittest.TestCase):
    def test_get_cli_version(self):
        fake_process = CompletedProcess(
            args=["apx", "version"],
            returncode=0,
            stdout="CLI version: 1.2.3\n",
            stderr="",
        )
        with patch.object(helper, "run_cli", return_value=fake_process):
            self.assertEqual(helper.get_cli_version(Path("/tmp/apx")), "1.2.3")

    def test_get_cli_version_fails_for_unexpected_output(self):
        fake_process = CompletedProcess(
            args=["apx", "version"],
            returncode=0,
            stdout="unexpected\n",
            stderr="",
        )
        with patch.object(helper, "run_cli", return_value=fake_process):
            with self.assertRaises(ValueError):
                helper.get_cli_version(Path("/tmp/apx"))


class ExportRunTests(unittest.TestCase):
    def test_export_run_returns_expected_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            cli_bin = tmp_path / "apap-cli" / "apx"
            cli_bin.parent.mkdir()
            cli_bin.write_text("")
            output_dir = tmp_path / "exports"

            def fake_run(cmd, cwd):
                (output_dir / "run-1.zip").write_text("data")
                return CompletedProcess(args=cmd, returncode=0, stdout="", stderr="")

            with patch.object(helper, "run_cli", side_effect=fake_run):
                archive = helper.export_run(cli_bin, "run-1", output_dir)
            self.assertEqual(archive, output_dir / "run-1.zip")

    def test_export_run_fails_when_archive_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            cli_bin = tmp_path / "apap-cli" / "apx"
            cli_bin.parent.mkdir()
            cli_bin.write_text("")
            with patch.object(helper, "run_cli", return_value=CompletedProcess(args=[], returncode=0)):
                with self.assertRaises(FileNotFoundError):
                    helper.export_run(cli_bin, "missing", tmp_path / "exports")


class Sha256FileTests(unittest.TestCase):
    def test_sha256_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "data.bin"
            path.write_bytes(b"abc")
            self.assertEqual(
                helper.sha256_file(path),
                "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
            )


if __name__ == "__main__":
    unittest.main()
