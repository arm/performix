# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import tempfile
import unittest
from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import patch


MODULE_DIR = Path(__file__).resolve().parent
RUNNER_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_evaluation_runner",
    MODULE_DIR / "run_ai_insights_evaluation.py",
)
runner = importlib.util.module_from_spec(RUNNER_SPEC)
RUNNER_SPEC.loader.exec_module(runner)


class AiInsightsEvaluationRunnerTests(unittest.TestCase):
    def test_reporting_dir_follows_ai_results_dir(self):
        with tempfile.TemporaryDirectory() as tmp:
            results_dir = Path(tmp) / "evaluation-results"

            reporting_dir = runner.reporting_dir_from_pytest_args(
                ["--ai-results-dir", str(results_dir), "-k", "test_case_03"]
            )

        self.assertEqual(results_dir / "reporting", reporting_dir)

    def test_relative_results_dir_is_relative_to_harness(self):
        reporting_dir = runner.reporting_dir_from_pytest_args(
            ["--ai-results-dir=custom-results"]
        )

        self.assertEqual(runner.HARNESS_DIR / "custom-results" / "reporting", reporting_dir)

    def test_default_reporting_dir_is_unchanged(self):
        self.assertEqual(
            runner.LOCAL_REPORTING_DIR,
            runner.reporting_dir_from_pytest_args(["-k", "test_case_03"]),
        )

    def test_clear_reporting_dir_reuses_existing_directory(self):
        with tempfile.TemporaryDirectory() as tmp:
            reporting_dir = Path(tmp) / "reporting"
            nested_dir = reporting_dir / "payload"
            nested_dir.mkdir(parents=True)
            (reporting_dir / "stale.txt").write_text("stale", encoding="utf-8")
            (nested_dir / "stale.json").write_text("{}", encoding="utf-8")

            runner.clear_reporting_dir(reporting_dir)

            self.assertTrue(reporting_dir.is_dir())
            self.assertEqual([], list(reporting_dir.iterdir()))

    def test_success_generates_reports_and_replaces_stale_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            reporting_dir = Path(tmp) / "reporting"
            reporting_dir.mkdir()
            resolved_reporting_dir = reporting_dir.resolve()
            stale_file = reporting_dir / "stale.txt"
            stale_file.write_text("stale", encoding="utf-8")

            def run(command, **kwargs):
                if command[1:3] == ["-m", "pytest"]:
                    (reporting_dir / runner.JUNIT_XML_NAME).write_text("<testsuites />", encoding="utf-8")
                return CompletedProcess(command, 0)

            with patch.object(runner.subprocess, "run", side_effect=run) as mock_run:
                result = runner.run_evaluation(
                    ["-k", "test_case_03"],
                    reporting_dir=reporting_dir,
                    python_executable="python",
                )
                stale_output_was_removed = not stale_file.exists()

        self.assertEqual(0, result)
        self.assertTrue(stale_output_was_removed)
        self.assertEqual(2, mock_run.call_count)
        pytest_command = mock_run.call_args_list[0].args[0]
        self.assertIn("-k", pytest_command)
        self.assertIn("test_case_03", pytest_command)
        self.assertIn(
            f"--junitxml={resolved_reporting_dir / runner.JUNIT_XML_NAME}",
            pytest_command,
        )
        self.assertEqual(["-o", "junit_family=legacy"], pytest_command[-2:])

        report_command = mock_run.call_args_list[1].args[0]
        self.assertEqual(str(runner.REPORT_GENERATOR), report_command[1])
        self.assertIn(str(resolved_reporting_dir / "payload"), report_command)
        self.assertIn(str(resolved_reporting_dir / runner.DASHBOARD_REPORT_NAME), report_command)

    def test_pytest_failure_still_generates_report_and_preserves_pytest_status(self):
        with tempfile.TemporaryDirectory() as tmp:
            reporting_dir = Path(tmp) / "reporting"

            def run(command, **kwargs):
                if command[1:3] == ["-m", "pytest"]:
                    (reporting_dir / runner.JUNIT_XML_NAME).write_text("<testsuites />", encoding="utf-8")
                    return CompletedProcess(command, 5)
                return CompletedProcess(command, 0)

            with patch.object(runner.subprocess, "run", side_effect=run) as mock_run:
                result = runner.run_evaluation(
                    [],
                    reporting_dir=reporting_dir,
                    python_executable="python",
                )

        self.assertEqual(5, result)
        self.assertEqual(2, mock_run.call_count)

    def test_missing_junit_preserves_pytest_failure_without_running_report_generator(self):
        with tempfile.TemporaryDirectory() as tmp:
            reporting_dir = Path(tmp) / "reporting"
            pytest_result = CompletedProcess(["pytest"], 2)

            with patch.object(runner.subprocess, "run", return_value=pytest_result) as mock_run:
                result = runner.run_evaluation(
                    [],
                    reporting_dir=reporting_dir,
                    python_executable="python",
                )

        self.assertEqual(2, result)
        mock_run.assert_called_once()

    def test_missing_junit_fails_an_otherwise_successful_run(self):
        with tempfile.TemporaryDirectory() as tmp:
            reporting_dir = Path(tmp) / "reporting"
            pytest_result = CompletedProcess(["pytest"], 0)

            with patch.object(runner.subprocess, "run", return_value=pytest_result) as mock_run:
                result = runner.run_evaluation(
                    [],
                    reporting_dir=reporting_dir,
                    python_executable="python",
                )

        self.assertEqual(1, result)
        mock_run.assert_called_once()

    def test_report_failure_fails_an_otherwise_successful_run(self):
        with tempfile.TemporaryDirectory() as tmp:
            reporting_dir = Path(tmp) / "reporting"

            def run(command, **kwargs):
                if command[1:3] == ["-m", "pytest"]:
                    (reporting_dir / runner.JUNIT_XML_NAME).write_text("<testsuites />", encoding="utf-8")
                    return CompletedProcess(command, 0)
                return CompletedProcess(command, 3)

            with patch.object(runner.subprocess, "run", side_effect=run):
                result = runner.run_evaluation(
                    [],
                    reporting_dir=reporting_dir,
                    python_executable="python",
                )

        self.assertEqual(3, result)

    def test_main_uses_reporting_dir_for_ai_results_dir(self):
        with tempfile.TemporaryDirectory() as tmp:
            results_dir = Path(tmp) / "results"
            args = ["--ai-results-dir", str(results_dir), "-k", "test_case_03"]

            with patch.object(runner, "run_evaluation", return_value=0) as run_evaluation:
                result = runner.main(args)

        self.assertEqual(0, result)
        run_evaluation.assert_called_once_with(
            args,
            reporting_dir=results_dir / "reporting",
        )


if __name__ == "__main__":
    unittest.main()
