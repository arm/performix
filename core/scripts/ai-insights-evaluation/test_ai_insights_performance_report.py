# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import sys
import tempfile
import unittest
from pathlib import Path


MODULE_DIR = Path(__file__).resolve().parent
REPORTING_DIR = MODULE_DIR.parent / "atperf-benchmark-reporting"
sys.path.append(str(MODULE_DIR))
sys.path.append(str(REPORTING_DIR))

from ai_insights_performance_report import (  # noqa: E402
    REPORT_NAME,
    build_dashboard_benchmarks,
    build_payloads,
    write_payloads,
)
from junit_attempts import attempts_from_junit  # noqa: E402
from parse_results import parse_payloads  # noqa: E402
from performance_quality import (  # noqa: E402
    is_performance_evaluated_mode,
    performance_failures_for_attempt,
    performance_metrics_for_attempt,
    performance_thresholds_from_manifest,
    recorded_performance_properties,
)


def with_performance_assessment(
    attempt: dict[str, str],
    thresholds: dict[str, int | float],
) -> dict[str, str]:
    metrics = performance_metrics_for_attempt(
        attempt,
        performance_thresholds={attempt["ai_test_id"]: thresholds},
    )
    return {**attempt, **dict(recorded_performance_properties(metrics))}


class AiInsightsPerformanceReportTests(unittest.TestCase):
    def test_only_performix_mcp_has_performance_evaluation(self):
        self.assertTrue(is_performance_evaluated_mode("performix_mcp"))
        self.assertFalse(is_performance_evaluated_mode("rest"))
        self.assertFalse(is_performance_evaluated_mode("hackathon_mcp"))

    def test_poor_and_indeterminable_metrics_are_performance_failures(self):
        thresholds = {
            "test_case_01": {
                "duration_seconds": 30,
                "input_tokens": 1000,
                "output_tokens": 1000,
            }
        }
        failures = performance_failures_for_attempt(
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "ai_agent_duration_seconds": "31",
                "ai_input_tokens": "",
                "ai_output_tokens": "900",
            },
            performance_thresholds=thresholds,
        )

        self.assertEqual(
            {"duration_seconds": "POOR", "input_tokens": "INDETERMINABLE"},
            {metric.key: metric.quality for metric in failures},
        )

    def test_missing_thresholds_do_not_create_performance_failures(self):
        failures = performance_failures_for_attempt(
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "ai_agent_duration_seconds": "31",
            },
        )

        self.assertEqual([], failures)

    def test_recorded_performix_mcp_assessment_drives_quality_distribution(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "rest",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "999",
                "ai_input_tokens": "999999",
                "ai_output_tokens": "999999",
            },
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "hackathon_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "999",
                "ai_input_tokens": "999999",
                "ai_output_tokens": "999999",
            },
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "60",
                "ai_input_tokens": "1000",
                "ai_output_tokens": "1000",
                "ai_reasoning_output_tokens": "123",
                "ai_mcp_completed_calls": "2",
            },
        ]

        attempts[2] = with_performance_assessment(
            attempts[2],
            {"duration_seconds": 60, "input_tokens": 1000, "output_tokens": 1000},
        )
        metadata, report = build_payloads(attempts)

        self.assertEqual(
            {
                "GOOD": 100,
                "POOR": 0,
                "INDETERMINABLE": 0,
            },
            metadata["data_quality_distribution"],
        )
        self.assertEqual(1, metadata["n_runs"])
        self.assertEqual(3, len(report["rows"]))
        ignored_col = report["headers"].index("ignored_for_quality")
        self.assertEqual([True, True, False], [row[ignored_col] for row in report["rows"]])
        self.assertIn("attempt", report["headers"])
        self.assertIn("attempts_total", report["headers"])

    def test_missing_and_over_threshold_performix_metrics_are_alertable(self):
        attempts = [
            {
                "ai_test_id": "test_case_02",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "60.01",
                "ai_input_tokens": "not-a-number",
                "ai_output_tokens": "1001",
            },
        ]

        attempts[0] = with_performance_assessment(
            attempts[0],
            {"duration_seconds": 60, "input_tokens": 1000, "output_tokens": 1000},
        )
        metadata, report = build_payloads(attempts)

        self.assertEqual(
            {
                "GOOD": 0,
                "POOR": 67,
                "INDETERMINABLE": 33,
            },
            metadata["data_quality_distribution"],
        )
        row = dict(zip(report["headers"], report["rows"][0]))
        self.assertEqual("POOR", row["duration_quality"])
        self.assertEqual("INDETERMINABLE", row["input_tokens_quality"])
        self.assertEqual("POOR", row["output_tokens_quality"])

    def test_report_preserves_recorded_per_test_thresholds_and_quality(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "61",
                "ai_input_tokens": "1001",
                "ai_output_tokens": "1001",
            },
            {
                "ai_test_id": "test_case_02",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "61",
                "ai_input_tokens": "1001",
                "ai_output_tokens": "1001",
            },
        ]

        attempts[0] = with_performance_assessment(
            attempts[0],
            {"duration_seconds": 60, "input_tokens": 1000, "output_tokens": 1000},
        )
        attempts[1] = with_performance_assessment(
            attempts[1],
            {"duration_seconds": 120, "input_tokens": 2000, "output_tokens": 2000},
        )
        metadata, report = build_payloads(attempts)

        rows = [dict(zip(report["headers"], row)) for row in report["rows"]]
        self.assertEqual("POOR", rows[0]["duration_quality"])
        self.assertEqual("GOOD", rows[1]["duration_quality"])
        self.assertEqual(120, rows[1]["duration_threshold_seconds"])
        self.assertEqual(
            {"duration_seconds": 120, "input_tokens": 2000, "output_tokens": 2000},
            metadata["benchmark_args"]["performance_thresholds"]["test_case_02"],
        )

    def test_performix_mcp_without_recorded_assessment_is_ignored_for_quality(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "61",
                "ai_input_tokens": "1001",
                "ai_output_tokens": "1001",
            },
            {
                "ai_test_id": "test_case_02",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "62",
                "ai_input_tokens": "1002",
                "ai_output_tokens": "1002",
            },
        ]

        metadata, report = build_payloads(attempts)

        rows = [dict(zip(report["headers"], row)) for row in report["rows"]]
        self.assertEqual(2, metadata["n_runs"])
        self.assertEqual(
            {
                "GOOD": 0,
                "POOR": 0,
                "INDETERMINABLE": 100,
            },
            metadata["data_quality_distribution"],
        )
        self.assertTrue(rows[0]["ignored_for_quality"])
        self.assertTrue(rows[1]["ignored_for_quality"])
        self.assertEqual("", rows[0]["duration_quality"])
        self.assertIsNone(rows[0]["duration_threshold_seconds"])
        self.assertEqual("", rows[1]["duration_quality"])
        self.assertIsNone(rows[1]["input_token_threshold"])

    def test_incomplete_recorded_performance_assessment_is_rejected(self):
        attempt = {
            "ai_test_id": "test_case_01",
            "ai_mode": "performix_mcp",
            "ai_performance_evaluated": "true",
        }

        with self.assertRaisesRegex(ValueError, "incomplete recorded performance assessment"):
            build_payloads([attempt])

    def test_conflicting_recorded_thresholds_are_rejected(self):
        base_attempt = {
            "ai_test_id": "test_case_01",
            "ai_mode": "performix_mcp",
            "ai_agent_duration_seconds": "1",
            "ai_input_tokens": "2",
            "ai_output_tokens": "3",
        }
        attempts = [
            with_performance_assessment(
                base_attempt,
                {"duration_seconds": 10, "input_tokens": 20, "output_tokens": 30},
            ),
            with_performance_assessment(
                base_attempt,
                {"duration_seconds": 11, "input_tokens": 20, "output_tokens": 30},
            ),
        ]

        with self.assertRaisesRegex(ValueError, "conflicting recorded performance thresholds"):
            build_payloads(attempts)

    def test_performance_thresholds_are_loaded_from_manifest(self):
        manifest = """{
  "tests": [
    {
      "id": "test_case_01",
      "performance_thresholds": {
        "duration_seconds": 90.5,
        "input_tokens": 1500
      }
    },
    {
      "id": "test_case_02"
    }
  ]
}
"""
        with tempfile.TemporaryDirectory() as tmp:
            manifest_path = Path(tmp) / "ai_insights_evaluation.json"
            manifest_path.write_text(manifest, encoding="utf-8")

            performance_thresholds = performance_thresholds_from_manifest(manifest_path)

        self.assertEqual(
            {"test_case_01": {"duration_seconds": 90.5, "input_tokens": 1500}},
            performance_thresholds,
        )

    def test_generated_output_is_read_by_benchmark_reporting_parser(self):
        attempts = [
            {
                "ai_test_id": "test_case_03",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_agent_duration_seconds": "1",
                "ai_input_tokens": "2",
                "ai_output_tokens": "3",
            },
        ]
        metadata, report = build_payloads(attempts)

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_payloads(metadata, report, root / "output")
            payloads = parse_payloads(root)

            self.assertEqual(1, len(payloads))
            self.assertEqual("ai_insights_evaluation", payloads[0].metadata["benchmark_name"])
            self.assertIn(REPORT_NAME, payloads[0].reports)

    def test_dashboard_benchmarks_include_only_performix_mcp_metrics(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "rest",
                "ai_attempt": "1",
                "ai_attempts_total": "1",
                "ai_agent_duration_seconds": "999",
                "ai_input_tokens": "999999",
                "ai_output_tokens": "999999",
            },
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "ai_attempt": "1",
                "ai_attempts_total": "1",
                "ai_agent_duration_seconds": "12.345",
                "ai_input_tokens": "456",
                "ai_output_tokens": "78",
            },
        ]
        _, report = build_payloads(attempts)

        self.assertEqual(
            [
                {"name": "test_case_01 wall-clock time", "unit": "s", "value": 12.345},
                {"name": "test_case_01 input tokens", "unit": "tokens", "value": 456},
                {"name": "test_case_01 output tokens", "unit": "tokens", "value": 78},
            ],
            build_dashboard_benchmarks(report),
        )

    def test_dashboard_benchmarks_disambiguate_repeated_attempts(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "ai_attempt": "1",
                "ai_attempts_total": "2",
                "ai_agent_duration_seconds": "11",
                "ai_input_tokens": "22",
                "ai_output_tokens": "33",
            },
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "ai_attempt": "2",
                "ai_attempts_total": "2",
                "ai_agent_duration_seconds": "44",
                "ai_input_tokens": "55",
                "ai_output_tokens": "66",
            },
        ]
        _, report = build_payloads(attempts)

        names = [benchmark["name"] for benchmark in build_dashboard_benchmarks(report)]

        self.assertIn("test_case_01 attempt 1 wall-clock time", names)
        self.assertIn("test_case_01 attempt 2 output tokens", names)

    def test_attempts_from_junit_reads_ai_properties_and_outcomes(self):
        junit = """<?xml version="1.0" encoding="utf-8"?>
<testsuite>
  <testcase classname="suite" name="passing">
    <properties>
      <property name="ai_test_id" value="test_case_04" />
      <property name="ai_mode" value="performix_mcp" />
      <property name="ai_agent_duration_seconds" value="1.5" />
      <property name="ai_input_tokens" value="20" />
      <property name="ai_output_tokens" value="30" />
      <property name="ai_performance_evaluated" value="true" />
      <property name="ai_performance_duration_threshold_seconds" value="2" />
      <property name="ai_performance_input_token_threshold" value="25" />
      <property name="ai_performance_output_token_threshold" value="35" />
      <property name="ai_performance_duration_quality" value="GOOD" />
      <property name="ai_performance_input_tokens_quality" value="GOOD" />
      <property name="ai_performance_output_tokens_quality" value="GOOD" />
    </properties>
  </testcase>
  <testcase classname="suite" name="failing">
    <properties>
      <property name="ai_test_id" value="test_case_05" />
      <property name="ai_mode" value="rest" />
    </properties>
    <failure message="failed" />
  </testcase>
</testsuite>
"""
        with tempfile.TemporaryDirectory() as tmp:
            junit_path = Path(tmp) / "ai.xml"
            junit_path.write_text(junit, encoding="utf-8")

            attempts = attempts_from_junit(junit_path)

        self.assertEqual(["passed", "failed"], [attempt["pytest_outcome"] for attempt in attempts])
        self.assertEqual("test_case_04", attempts[0]["ai_test_id"])
        metadata, report = build_payloads(attempts)
        self.assertEqual(100, metadata["data_quality_distribution"]["GOOD"])
        row = dict(zip(report["headers"], report["rows"][0]))
        self.assertEqual(2, row["duration_threshold_seconds"])
        self.assertEqual("GOOD", row["duration_quality"])


if __name__ == "__main__":
    unittest.main()
