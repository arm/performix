# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import io
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import patch

MODULE_DIR = Path(__file__).resolve().parent
PRERECORD_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_prerecord_run",
    MODULE_DIR / "ai-insights-evaluation" / "prerecord-run.py",
)
prerecord = importlib.util.module_from_spec(PRERECORD_SPEC)
PRERECORD_SPEC.loader.exec_module(prerecord)


class QueryRenderRowsTests(unittest.TestCase):
    def test_non_list_rows_error_includes_value(self):
        fake_process = CompletedProcess(
            args=["apx"],
            returncode=0,
            stdout='{"data":{"rows":null}}',
            stderr="",
        )

        with patch.object(prerecord, "run_cli", return_value=fake_process):
            with self.assertRaises(ValueError) as ctx:
                prerecord.query_render_rows(Path("/tmp/apx"), "s1", "SELECT 1")

        self.assertIn("render query rows are not a list", str(ctx.exception))
        self.assertIn("rows=None", str(ctx.exception))


class PrintRunInfoTests(unittest.TestCase):
    def test_print_run_info_reports_json_output(self):
        fake_process = CompletedProcess(
            args=["apx"],
            returncode=0,
            stdout='{"data":{"id":"run-1","run_result":"success"}}\n',
            stderr="",
        )

        with patch.object(prerecord, "run_cli", return_value=fake_process) as mock_run:
            output = io.StringIO()
            with redirect_stdout(output):
                prerecord.print_run_info(Path("/tmp/apx"), "run-1")

        mock_run.assert_called_once_with(
            ["/tmp/apx", "run", "info", "run-1", "--json"],
            Path("/tmp"),
        )
        self.assertIn("===== apx run info run-1 --json =====", output.getvalue())
        self.assertIn('"run_result":"success"', output.getvalue())
        self.assertIn("===== end apx run info =====", output.getvalue())


if __name__ == "__main__":
    unittest.main()
