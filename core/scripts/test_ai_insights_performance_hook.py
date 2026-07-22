# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import sys
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


SCRIPTS_DIR = Path(__file__).resolve().parent
HARNESS_DIR = SCRIPTS_DIR / "ai-insights-evaluation"
sys.path.append(str(HARNESS_DIR))

import conftest as ai_conftest  # noqa: E402


class _HookOutcome:
    def __init__(self, report):
        self.report = report

    def get_result(self):
        return self.report


class PerformanceQualityHookTests(unittest.TestCase):
    PERFORMANCE_THRESHOLDS = {
        "test_case_01": {
            "duration_seconds": 30,
            "input_tokens": 1000,
            "output_tokens": 1000,
        }
    }

    def _run_hook(self, report) -> None:
        item = SimpleNamespace(config=object())
        hook = ai_conftest.pytest_runtest_makereport(item, call=None)
        next(hook)
        with patch.object(
            ai_conftest,
            "_performance_thresholds",
            return_value=self.PERFORMANCE_THRESHOLDS,
        ):
            with self.assertRaises(StopIteration):
                hook.send(_HookOutcome(report))

    def test_hook_fails_passing_call_report_for_poor_or_indeterminable_performance(self):
        report = SimpleNamespace(
            when="call",
            passed=True,
            outcome="passed",
            longrepr=None,
            user_properties=[
                ("ai_test_id", "test_case_01"),
                ("ai_mode", "performix_mcp"),
                ("ai_agent_duration_seconds", "31"),
                ("ai_input_tokens", ""),
                ("ai_output_tokens", "900"),
            ],
        )

        self._run_hook(report)

        self.assertEqual("failed", report.outcome)
        self.assertIn("runtime=31 (POOR, threshold=30)", report.longrepr)
        self.assertIn("input tokens=None (INDETERMINABLE, threshold=1000)", report.longrepr)
        properties = dict(report.user_properties)
        self.assertEqual("true", properties["ai_performance_evaluated"])
        self.assertEqual("30", properties["ai_performance_duration_threshold_seconds"])
        self.assertEqual("POOR", properties["ai_performance_duration_quality"])
        self.assertEqual("INDETERMINABLE", properties["ai_performance_input_tokens_quality"])

    def test_hook_leaves_passing_call_report_with_good_performance_unchanged(self):
        report = SimpleNamespace(
            when="call",
            passed=True,
            outcome="passed",
            longrepr=None,
            user_properties=[
                ("ai_test_id", "test_case_01"),
                ("ai_mode", "performix_mcp"),
                ("ai_agent_duration_seconds", "30"),
                ("ai_input_tokens", "1000"),
                ("ai_output_tokens", "1000"),
            ],
        )

        self._run_hook(report)

        self.assertEqual("passed", report.outcome)
        self.assertIsNone(report.longrepr)
        properties = dict(report.user_properties)
        self.assertEqual("true", properties["ai_performance_evaluated"])
        self.assertEqual("GOOD", properties["ai_performance_duration_quality"])

    def test_hook_records_performance_for_an_already_failed_call_report(self):
        report = SimpleNamespace(
            when="call",
            passed=False,
            outcome="failed",
            longrepr="functional failure",
            user_properties=[
                ("ai_test_id", "test_case_01"),
                ("ai_mode", "performix_mcp"),
                ("ai_agent_duration_seconds", "31"),
                ("ai_input_tokens", "1000"),
                ("ai_output_tokens", "1000"),
            ],
        )

        self._run_hook(report)

        self.assertEqual("failed", report.outcome)
        self.assertEqual("functional failure", report.longrepr)
        self.assertEqual("POOR", dict(report.user_properties)["ai_performance_duration_quality"])

    def test_hook_records_skipped_assessment_for_a_non_evaluated_mode(self):
        report = SimpleNamespace(
            when="call",
            passed=True,
            outcome="passed",
            longrepr=None,
            user_properties=[
                ("ai_test_id", "test_case_01"),
                ("ai_mode", "hackathon_mcp"),
                ("ai_agent_duration_seconds", "31"),
                ("ai_input_tokens", "1001"),
                ("ai_output_tokens", "1001"),
            ],
        )

        self._run_hook(report)

        properties = dict(report.user_properties)
        self.assertEqual("false", properties["ai_performance_evaluated"])
        self.assertNotIn("ai_performance_duration_quality", properties)

    def test_hook_ignores_non_call_report(self):
        report = SimpleNamespace(
            when="setup",
            passed=True,
            outcome="passed",
            longrepr=None,
            user_properties=[
                ("ai_test_id", "test_case_01"),
                ("ai_mode", "performix_mcp"),
                ("ai_agent_duration_seconds", "31"),
                ("ai_input_tokens", "1001"),
                ("ai_output_tokens", "1001"),
            ],
        )

        self._run_hook(report)

        self.assertEqual("passed", report.outcome)
        self.assertIsNone(report.longrepr)


if __name__ == "__main__":
    unittest.main()
