# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import MagicMock, patch

MODULE_DIR = Path(__file__).resolve().parent
GHR_SPEC = importlib.util.spec_from_file_location(
    "generate_historical_runs",
    MODULE_DIR / "generate_historical_runs.py"
)
ghr = importlib.util.module_from_spec(GHR_SPEC)
GHR_SPEC.loader.exec_module(ghr)


class LoadConfigTests(unittest.TestCase):
    def test_load_config_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            cfg_path = Path(tmp) / "cfg.json"
            cfg_path.write_text(json.dumps([{"uid": "a"}]))
            self.assertEqual(ghr.load_config(cfg_path), [{"uid": "a"}])

    def test_load_config_missing(self):
        with tempfile.TemporaryDirectory() as tmp, \
                self.assertRaises(SystemExit), \
                patch.object(ghr.logger, "error"):
            ghr.load_config(Path(tmp) / "cfg.json")

    def test_load_config_not_list(self):
        with tempfile.TemporaryDirectory() as tmp, \
                self.assertRaises(SystemExit), \
                patch.object(ghr.logger, "error"):
            cfg_path = Path(tmp) / "cfg.json"
            cfg_path.write_text(json.dumps({"uid": "a"}))
            ghr.load_config(cfg_path)


class RunCmdTests(unittest.TestCase):
    def test_run_cmd_success(self):
        fake_process = CompletedProcess(args=["x"], returncode=0, stdout="", stderr="")
        with patch.object(ghr.run_export_helper, "run_cli", return_value=fake_process) as mock_run:
            result = ghr.run_cmd(["x"], Path("/tmp"))
        self.assertIs(result, fake_process)
        mock_run.assert_called_once_with(["x"], Path("/tmp"))

    def test_run_cmd_failure(self):
        fake_process = CompletedProcess(args=["x"], returncode=1, stdout="out", stderr="err")
        with patch.object(
            ghr.run_export_helper,
            "run_cli",
            side_effect=ghr.run_export_helper.CommandFailure(fake_process),
        ), \
                patch.object(ghr.logger, "error"), \
                self.assertRaises(SystemExit) as ctx:
            ghr.run_cmd(["x"], Path("/tmp"))
        self.assertEqual(ctx.exception.code, 1)


class HelpersTests(unittest.TestCase):
    def test_find_cli_bin_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            exe = repo / "apap-cli" / "apx"
            exe.parent.mkdir(parents=True)
            exe.write_text("")
            self.assertEqual(ghr.find_cli_bin(repo, None), exe.resolve())

    def test_find_cli_bin_custom_missing(self):
        with tempfile.TemporaryDirectory() as tmp, \
                patch.object(ghr.logger, "error"), \
                self.assertRaises(SystemExit):
            ghr.find_cli_bin(Path(tmp), "/missing/file")

    def test_parse_run_id_success(self):
        self.assertEqual(
            ghr.parse_run_id("Run ID: abc-123"),
            "abc-123"
        )

    def test_parse_run_id_failure(self):
        with patch.object(ghr.logger, "error"), self.assertRaises(SystemExit):
            ghr.parse_run_id("no id here")

    def test_get_engine_version_success(self):
        fake_process = CompletedProcess(
            args=["x"], returncode=0,
            stdout="CLI version: 0.34.0\n", stderr=""
        )
        with patch.object(ghr, "run_cmd", return_value=fake_process):
            version = ghr.get_engine_version(Path("/tmp/apx"))
        self.assertEqual(version, "0.34.0")


class ProcessEntryTests(unittest.TestCase):
    def test_process_entry_dry_run(self):
        manifest = []
        with patch.object(ghr.logger, "info"):
            ghr.process_entry(
                {"uid": "1", "cmd_args": ["recipe", "--flag"]},
                Path("/tmp/bin"),
                "0.34.0",
                target=None,
                output=Path("/tmp/out"),
                workload_override=None,
                dry=True,
                manifest=manifest
            )
        self.assertEqual(manifest, [])

    def test_process_entry_happy_path(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            cli_bin = tmp_path / "apap-cli" / "apx"
            cli_bin.parent.mkdir(parents=True)
            cli_bin.write_text("")
            output_dir = tmp_path / "output"

            run_results = [
                CompletedProcess(
                    args=["recipe run"], returncode=0,
                    stdout="Run ID: abc-123", stderr=""
                ),
                CompletedProcess(
                    args=["run export"], returncode=0,
                    stdout="", stderr=""
                )
            ]

            def side_effect(cmd, cwd):
                result = run_results.pop(0)
                if "export" in cmd:
                    zip_path = cli_bin.parent.parent / "abc-123.zip"
                    zip_path.write_text("data")
                return result

            manifest = []
            with patch.object(ghr, "run_cmd", side_effect=side_effect), \
                    patch.object(ghr, "parse_run_id", return_value="abc-123"):
                ghr.process_entry(
                    {"uid": "uid1", "cmd_args": ["recipe", "--workload=foo"]},
                    cli_bin,
                    "0.34.0",
                    target="tgt",
                    output=output_dir,
                    workload_override=None,
                    dry=False,
                    manifest=manifest
                )

            self.assertEqual(len(manifest), 1)
            entry = manifest[0]
            self.assertEqual(entry["zip_path"], "tgt/uid1/abc-123.zip")
            expected_zip = output_dir / "0.34.0" / "tgt" / "uid1" / "abc-123.zip"
            self.assertTrue(expected_zip.exists())


if __name__ == "__main__":
    unittest.main()
