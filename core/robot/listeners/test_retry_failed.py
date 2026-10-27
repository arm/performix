# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Integration tests for the immediate Robot test retry listener."""

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
from types import SimpleNamespace
import unittest

from robot.api import ExecutionResult


LISTENER_DIR = Path(__file__).resolve().parent
ROBOT_DIR = LISTENER_DIR.parent
RUN_ROBOT_PATH = ROBOT_DIR.parent / "scripts" / "run-robot.py"


def load_run_robot():
    spec = importlib.util.spec_from_file_location("run_robot", RUN_ROBOT_PATH)
    module = importlib.util.module_from_spec(spec)
    sys.path.insert(0, str(RUN_ROBOT_PATH.parent))
    try:
        spec.loader.exec_module(module)
    finally:
        sys.path.pop(0)
    return module


class RetryFailedIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_dir.cleanup)
        self.root = Path(self.temp_dir.name)

    def _run(self, keyword, retry=False, suite_setup=None):
        setup = f"Suite Setup    {suite_setup}" if suite_setup else ""
        suite = self.root / "retry.robot"
        suite.write_text(
            textwrap.dedent(
                f"""
                *** Settings ***
                {setup}

                *** Variables ***
                ${{ATTEMPTS}}    0

                *** Keywords ***
                Fail Once
                    ${{next}} =    Evaluate    ${{ATTEMPTS}} + 1
                    Set Suite Variable    ${{ATTEMPTS}}    ${{next}}
                    Should Be True    ${{next}} > 1    transient failure

                Always Fail
                    Fail    persistent failure

                Fail Then Skip
                    ${{next}} =    Evaluate    ${{ATTEMPTS}} + 1
                    Set Suite Variable    ${{ATTEMPTS}}    ${{next}}
                    Run Keyword If    ${{next}} == 1    Fail    original failure
                    Skip    retry skipped

                *** Test Cases ***
                Retried Test
                    {keyword}

                Following Test
                    No Operation
                """
            ),
            encoding="utf-8",
        )
        output_dir = self.root / "results"
        command = [
            "robot",
            "--outputdir",
            str(output_dir),
        ]
        if retry:
            command += [
                "--pythonpath",
                str(LISTENER_DIR),
                "--listener",
                "retry_failed.RetryFailed:1:True",
            ]
        else:
            command.append("--exitonfailure")
        completed = subprocess.run(
            [*command, str(suite)],
            cwd=ROBOT_DIR,
            check=False,
            capture_output=True,
            text=True,
        )
        return completed, output_dir

    @staticmethod
    def _tests(output_dir):
        return ExecutionResult(output_dir / "output.xml").suite.tests

    def test_without_listener_fail_once_stops_following_test(self):
        completed, output_dir = self._run("Fail Once")

        self.assertNotEqual(completed.returncode, 0)
        tests = self._tests(output_dir)
        self.assertEqual([test.status for test in tests], ["FAIL", "FAIL"])
        self.assertIn("exit-on-failure", tests[1].message)

    def test_listener_recovers_failure_and_keeps_one_final_result(self):
        completed, output_dir = self._run("Fail Once", retry=True)

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        tests = self._tests(output_dir)
        self.assertEqual([test.name for test in tests], ["Retried Test", "Following Test"])
        self.assertEqual([test.status for test in tests], ["PASS", "PASS"])

        report = json.loads(
            (output_dir / "flaky-robot-report.json").read_text(encoding="utf-8")
        )
        flaky_test = report["testResults"][0]
        self.assertEqual(flaky_test["title"], "Retried Test")
        self.assertEqual(
            [(attempt["attempt"], attempt["status"]) for attempt in flaky_test["attempts"]],
            [(1, "fail"), (2, "pass")],
        )

    def test_listener_exhausts_retry_then_skips_remaining_tests(self):
        completed, output_dir = self._run("Always Fail", retry=True)

        self.assertNotEqual(completed.returncode, 0)
        tests = self._tests(output_dir)
        self.assertEqual([test.status for test in tests], ["FAIL", "SKIP"])
        self.assertIn("Failed after 2 attempts", tests[0].message)
        self.assertFalse((output_dir / "flaky-robot-report.json").exists())

    def test_listener_skips_setup_for_suites_after_exhausted_retry(self):
        suites = self.root / "suites"
        suites.mkdir()
        (suites / "01_failure.robot").write_text(
            "*** Test Cases ***\nPersistent Failure\n    Fail    persistent failure\n",
            encoding="utf-8",
        )
        marker = self.root / "suite-setup-ran.txt"
        (suites / "02_following.robot").write_text(
            textwrap.dedent(
                f"""
                *** Settings ***
                Library    OperatingSystem
                Suite Setup    Create File    {marker.as_posix()}    setup ran

                *** Test Cases ***
                Following Test
                    No Operation
                """
            ),
            encoding="utf-8",
        )
        output_dir = self.root / "multi-suite-results"
        completed = subprocess.run(
            [
                "robot",
                "--outputdir",
                str(output_dir),
                "--pythonpath",
                str(LISTENER_DIR),
                "--listener",
                "retry_failed.RetryFailed:1:True",
                str(suites),
            ],
            cwd=ROBOT_DIR,
            check=False,
            capture_output=True,
            text=True,
        )

        self.assertNotEqual(completed.returncode, 0)
        tests = ExecutionResult(output_dir / "output.xml").suite.all_tests
        self.assertEqual([test.status for test in tests], ["FAIL", "SKIP"])
        self.assertFalse(marker.exists())

    def test_listener_fails_when_retry_skips(self):
        completed, output_dir = self._run("Fail Then Skip", retry=True)

        self.assertNotEqual(completed.returncode, 0)
        tests = self._tests(output_dir)
        self.assertEqual([test.status for test in tests], ["FAIL", "SKIP"])
        self.assertIn("original failure", tests[0].message)
        self.assertIn("retry skipped", tests[0].message)
        self.assertFalse((output_dir / "flaky-robot-report.json").exists())

    def test_listener_does_not_report_clean_pass(self):
        completed, output_dir = self._run("No Operation", retry=True)

        self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
        self.assertFalse((output_dir / "flaky-robot-report.json").exists())

    def test_listener_does_not_retry_fatal_or_suite_setup_failures(self):
        cases = (("Fatal Error    stop", None), ("No Operation", "Always Fail"))
        for keyword, suite_setup in cases:
            with self.subTest(keyword=keyword, suite_setup=suite_setup):
                completed, output_dir = self._run(
                    keyword, retry=True, suite_setup=suite_setup
                )
                self.assertNotEqual(completed.returncode, 0)
                self.assertNotIn("[RETRY]", self._tests(output_dir)[0].message)
                self.assertFalse((output_dir / "flaky-robot-report.json").exists())


class RunRobotRetryOptionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.run_robot = load_run_robot()

    @staticmethod
    def _args(retry_count):
        return SimpleNamespace(
            runner_os="Linux",
            run_remote_localhost=False,
            exclude_tags=[],
            results_dir="robot/results",
            include_tags=[],
            workloads=None, launch_workload=None,
            fail_fast=True,
            retry_failed_count=retry_count,
            live_recipe_runs=False,
            test_suite=None, tests_dir="robot/tests",
        )

    def test_command_uses_native_or_listener_fail_fast(self):
        command, _ = self.run_robot.build_robot_command(self._args(0), "target")
        self.assertIn("--exitonfailure", command)
        self.assertNotIn("--listener", command)

        command, _ = self.run_robot.build_robot_command(self._args(1), "target")
        pythonpath_index = command.index("--pythonpath")
        self.assertEqual(command[pythonpath_index + 1], str(LISTENER_DIR))
        listener_index = command.index("--listener")
        self.assertEqual(
            command[listener_index + 1], "retry_failed.RetryFailed:1:True"
        )
        self.assertNotIn("--exitonfailure", command)

    def test_negative_retry_count_is_rejected(self):
        with self.assertRaisesRegex(
            self.run_robot.argparse.ArgumentTypeError, "zero or greater"
        ):
            self.run_robot.non_negative_int("-1")


if __name__ == "__main__":
    unittest.main()
