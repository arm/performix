# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import json
import os
import sys
import tempfile
import textwrap
import unittest
import xml.etree.ElementTree as ET
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

    def test_environment_results_root_is_used_for_reports(self):
        with patch.dict(os.environ, {"AI_INSIGHTS_RESULTS_DIR": "custom"}):
            self.assertEqual(runner.HARNESS_DIR / "custom/reporting", runner.reporting_dir_from_pytest_args([]))

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

    def test_collect_only_does_not_clear_existing_reports(self):
        with tempfile.TemporaryDirectory() as tmp:
            report = Path(tmp) / "existing.txt"
            report.write_text("keep")
            with patch.object(runner.subprocess, "run", return_value=CompletedProcess([], 0)):
                self.assertEqual(0, runner.run_evaluation(["--collect-only"], reporting_dir=Path(tmp)))
            self.assertEqual("keep", report.read_text())


class PathwaysSelectionGuardTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.report = self.root / "results.xml"
        # Use the actual collection/filter/guard hooks, without downloading fixtures.
        (self.root / "conftest.py").write_text(textwrap.dedent(f'''
            import importlib.util
            import sys
            sys.path.insert(0, {str(MODULE_DIR)!r})
            spec = importlib.util.spec_from_file_location("scheduling_hooks", {str(MODULE_DIR / 'conftest.py')!r})
            hooks = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(hooks)
            hooks._prepare_selected_testcases = lambda *args: None
            pytest_addoption = hooks.pytest_addoption
            pytest_collection_modifyitems = hooks.pytest_collection_modifyitems
            pytest_xdist_node_collection_finished = hooks.pytest_xdist_node_collection_finished
        '''), encoding="utf-8")
        (self.root / "test_fake.py").write_text(textwrap.dedent('''
            import json
            import os
            import pytest
            from pathlib import Path

            cases = json.loads(Path("manifest.json").read_text())["tests"]

            @pytest.fixture
            def lifecycle(request):
                case = request.node.callspec.params["test_case"]
                record = {"results_dir": request.config.getoption("--ai-results-dir"),
                          "id": request.node.nodeid, "pathways": bool(case.get("pathways_execution")),
                          "worker": os.environ.get("PYTEST_XDIST_WORKER")}
                with (Path(__file__).parent / ("events-" + str(os.getpid()) + ".jsonl")).open("a") as output:
                    output.write(json.dumps(record) + "\\n")

            @pytest.mark.parametrize("attempt", [1, 2])
            @pytest.mark.parametrize("mode", ["performix_mcp", "other_mcp"])
            @pytest.mark.parametrize("test_case", cases, ids=lambda case: case["id"])
            def test_ai_insights(test_case, mode, attempt, lifecycle):
                pass
        '''), encoding="utf-8")
        manifest = {"defaults": {"prerecord": {"mode": "launch"}}, "tests": [
            {"id": name, "run_artifact": "fixture", "recipe": "code_hotspots",
             **({"pathways_execution": {"mode": "host_ssh"}} if name != "ordinary" else {})}
            for name in ("ordinary", "path-a", "path-b")
        ]}
        manifest["tests"][2].pop("run_artifact")
        (self.root / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.command = [sys.executable, "-m", "pytest", "test_fake.py", "-q",
                        f"--ai-manifest={self.root / 'manifest.json'}",
                        "--ai-openai-api-key=fake", f"--ai-prerecorded-run-cache={self.root}",
                        f"--ai-results-dir={self.root / 'results'}"]
        environment = patch.dict(os.environ, {"PYTEST_ADDOPTS": ""})
        environment.start()
        self.addCleanup(environment.stop)

    def read_events(self):
        return [json.loads(line) for path in self.root.glob("events-*.jsonl")
                for line in path.read_text().splitlines()]

    def run_pytest(self, *args):
        return runner.subprocess.run(
            [*self.command, *args], cwd=self.root, capture_output=True, text=True,
        )

    def test_multiple_pathways_cases_or_attempts_are_rejected_before_setup(self):
        for selection in ("path-a and performix_mcp", "path and performix_mcp and 1"):
            with self.subTest(selection=selection):
                result = self.run_pytest("-n", "0", "-k", selection)
                self.assertEqual(4, result.returncode, result.stdout + result.stderr)
                self.assertIn("fresh disposable target", result.stderr)
        self.assertEqual([], self.read_events())

    def test_parallel_pathways_is_rejected_before_any_setup(self):
        for selection in ("path-a-performix_mcp-1 or ordinary", "path-b-performix_mcp-1"):
            with self.subTest(selection=selection):
                result = self.run_pytest("-n", "2", "-k", selection)
                self.assertNotEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertIn("Pathways attempts require serial execution", result.stdout + result.stderr)
                self.assertEqual([], self.read_events())

    def test_worker_defaults_require_an_explicit_serial_override(self):
        (self.root / "pytest.ini").write_text("[pytest]\naddopts = -n 2\n")
        result = self.run_pytest("-k", "path-a-performix_mcp-1")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("pass -n 0", result.stdout + result.stderr)
        self.assertEqual([], self.read_events())
        result = self.run_pytest("-n", "0", "-k", "path-a-performix_mcp-1")
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)

    def test_repeated_local_runs_keep_separate_attempt_evidence(self):
        for _ in range(2):
            result = self.run_pytest("-n", "0", "-k", "path-a-performix_mcp-1")
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        events = self.read_events()
        self.assertEqual(2, len(events))
        self.assertEqual(2, len({event["results_dir"] for event in events}))
        self.assertTrue(all(event["worker"] is None for event in events))

    def test_ci_can_collect_repetitions_and_execute_each_exact_id(self):
        selection = self.root / "selection.json"
        result = self.run_pytest("--collect-only", "-n", "0", "-k", "path-a and performix_mcp",
                                 f"--ai-collect-selection={selection}")
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertEqual([], self.read_events())
        items = json.loads(selection.read_text())["items"]
        self.assertEqual(2, len(items))
        for index, item in enumerate(items, 1):
            selected = self.root / "selected.json"
            selected.write_text(json.dumps([item["nodeid"]]))
            report = self.root / f"attempt-{index}.xml"
            result = self.run_pytest("-n", "0", f"--ai-selected-nodeids={selected}", f"--junitxml={report}")
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual(1, len(ET.parse(report).findall(".//testcase")))
        self.assertEqual({item["nodeid"] for item in items},
                         {event["id"] for event in self.read_events()})

    def test_ci_selection_cannot_bypass_the_one_attempt_guard(self):
        selection = self.root / "selection.json"
        self.run_pytest("--collect-only", "-k", "path-a", f"--ai-collect-selection={selection}")
        items = json.loads(selection.read_text())["items"]
        selection.write_text(json.dumps([item["nodeid"] for item in items]))
        result = self.run_pytest("-n", "0", f"--ai-selected-nodeids={selection}")
        self.assertEqual(4, result.returncode)
        self.assertEqual([], self.read_events())

    def test_ordinary_tests_keep_parallel_execution(self):
        result = self.run_pytest("-n", "2", "-k", "ordinary", f"--junitxml={self.report}")
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertEqual(4, len(ET.parse(self.report).findall(".//testcase")))
        self.assertTrue(all(event["worker"] for event in self.read_events()))


if __name__ == "__main__":
    unittest.main()
