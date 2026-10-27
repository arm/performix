# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import sys
import unittest
from pathlib import Path


MODULE_DIR = Path(__file__).resolve().parent
sys.path.append(str(MODULE_DIR))

from evaluation_summary import render_console_summary, render_markdown_summary  # noqa: E402


class EvaluationSummaryTests(unittest.TestCase):
    def test_timeout_is_explicit_in_console_and_ci_summaries(self):
        attempt = {"ai_test_id": "pathways_01", "ai_mode": "performix_mcp", "ai_attempt": "1",
                   "ai_display_scores": "error", "pytest_outcome": "failed",
                   "ai_agent_timeout_seconds": "300", "ai_agent_duration_seconds": "301"}
        for render in (render_console_summary, render_markdown_summary):
            for attempts in ([attempt], [attempt, {**attempt, "ai_attempt": "2"}]):
                with self.subTest(renderer=render.__name__, attempts=len(attempts)):
                    summary = render(attempts)
                    self.assertIn("TIMEOUT", summary)
                    self.assertIn("300s limit; judge not run", summary)
                    self.assertIn(f"{301 * len(attempts)}s", summary)

    def test_console_summary_separates_failed_and_successful_performance_metrics(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_judge_confidences": "high",
                "ai_agent_duration_seconds": "90",
                "ai_input_tokens": "110000",
                "ai_output_tokens": "2000",
                "ai_performance_evaluated": "true",
                "ai_performance_duration_threshold_seconds": "80",
                "ai_performance_input_token_threshold": "100000",
                "ai_performance_output_token_threshold": "3000",
                "ai_performance_duration_quality": "POOR",
                "ai_performance_input_tokens_quality": "POOR",
                "ai_performance_output_tokens_quality": "GOOD",
            }
        ]

        summary = render_console_summary(
            attempts,
            width=240,
        )

        self.assertIn("FAIL (high)", summary)
        self.assertNotIn("PASS (high)", summary)
        self.assertIn(
            "❌ Error: runtime 90.0s > 80.0s; input tokens 110,000 > 100,000",
            summary,
        )
        self.assertNotIn("Info", summary)

    def test_console_summary_marks_indeterminate_performance_as_failed(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "60",
                "ai_output_tokens": "4000",
                "ai_performance_evaluated": "true",
                "ai_performance_duration_threshold_seconds": "80",
                "ai_performance_input_token_threshold": "100000",
                "ai_performance_output_token_threshold": "5000",
                "ai_performance_duration_quality": "GOOD",
                "ai_performance_input_tokens_quality": "INDETERMINABLE",
                "ai_performance_output_tokens_quality": "GOOD",
            }
        ]

        summary = render_console_summary(
            attempts,
            width=240,
        )

        self.assertIn("FAIL", summary)
        self.assertIn("❌ Error: input tokens indeterminate", summary)
        self.assertNotIn("Info", summary)

    def test_console_summary_labels_mixed_results_across_multiple_attempts(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "ai_attempt": "1",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "60",
                "ai_input_tokens": "1000",
                "ai_output_tokens": "2000",
                "ai_performance_evaluated": "true",
                "ai_performance_duration_threshold_seconds": "80",
                "ai_performance_input_token_threshold": "100000",
                "ai_performance_output_token_threshold": "3000",
                "ai_performance_duration_quality": "GOOD",
                "ai_performance_input_tokens_quality": "GOOD",
                "ai_performance_output_tokens_quality": "GOOD",
            },
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "ai_attempt": "2",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "60",
                "ai_input_tokens": "1000",
                "ai_output_tokens": "4000",
                "ai_performance_evaluated": "true",
                "ai_performance_duration_threshold_seconds": "80",
                "ai_performance_input_token_threshold": "100000",
                "ai_performance_output_token_threshold": "3000",
                "ai_performance_duration_quality": "GOOD",
                "ai_performance_input_tokens_quality": "GOOD",
                "ai_performance_output_tokens_quality": "POOR",
            },
        ]

        summary = render_console_summary(attempts, width=300)

        self.assertIn("attempt 1 PASS, attempt 2 FAIL", summary)
        self.assertIn("❌ Error (attempt 2): output tokens 4,000 > 3,000", summary)
        self.assertNotIn("quality PASS", summary)
        self.assertNotIn("Info", summary)

    def test_summaries_omit_passing_performance_assessment(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "90",
                "ai_input_tokens": "1000",
                "ai_output_tokens": "1000",
                "ai_performance_evaluated": "true",
                "ai_performance_duration_threshold_seconds": "120",
                "ai_performance_input_token_threshold": "100000",
                "ai_performance_output_token_threshold": "3000",
                "ai_performance_duration_quality": "GOOD",
                "ai_performance_input_tokens_quality": "GOOD",
                "ai_performance_output_tokens_quality": "GOOD",
            }
        ]

        console_summary = render_console_summary(attempts, width=200)
        markdown_summary = render_markdown_summary(attempts)

        for summary in (console_summary, markdown_summary):
            self.assertNotIn("quality PASS", summary)
            self.assertNotIn("Info", summary)
            self.assertNotIn("❌ Error", summary)
            self.assertIn("tokens in=1,000 out=1,000", summary)

    def test_markdown_summary_separates_failed_and_successful_performance_metrics(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "90",
                "ai_input_tokens": "110000",
                "ai_output_tokens": "2000",
                "ai_performance_evaluated": "true",
                "ai_performance_duration_threshold_seconds": "80",
                "ai_performance_input_token_threshold": "100000",
                "ai_performance_output_token_threshold": "3000",
                "ai_performance_duration_quality": "POOR",
                "ai_performance_input_tokens_quality": "POOR",
                "ai_performance_output_tokens_quality": "GOOD",
            }
        ]

        summary = render_markdown_summary(attempts)

        self.assertIn(
            "❌ Error: runtime 90.0s &gt; 80.0s; input tokens 110,000 &gt; 100,000",
            summary,
        )
        self.assertNotIn("Info", summary)

    def test_performix_mcp_summary_omits_quality_without_recorded_assessment(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "60",
                "ai_input_tokens": "1000",
                "ai_output_tokens": "4000",
                "ai_performance_evaluated": "false",
            }
        ]

        summary = render_console_summary(attempts)

        self.assertNotIn("quality", summary)
        self.assertIn("PASS", summary)

    def test_non_performix_mcp_summary_omits_quality(self):
        attempts = [
            {
                "ai_test_id": "test_case_01",
                "ai_mode": "other_mode",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "90",
                "ai_input_tokens": "1000",
                "ai_output_tokens": "4000",
                "ai_performance_evaluated": "false",
            }
        ]

        summary = render_console_summary(attempts)

        self.assertNotIn("quality", summary)
        self.assertIn("PASS", summary)

    def test_run_query_mcp_summary_includes_metrics(self):
        attempts = [
            {
                "ai_test_id": "test_case_03",
                "ai_mode": "performix_mcp_run_query",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_agent_duration_seconds": "60",
                "ai_input_tokens": "1000",
                "ai_output_tokens": "2000",
                "ai_reasoning_output_tokens": "300",
                "ai_mcp_tool_rounds": "3",
                "ai_mcp_tool_calls_succeeded": "4",
                "ai_mcp_tool_calls_failed": "1",
            }
        ]

        summary = render_console_summary(attempts, width=240)

        self.assertIn("tokens in=1,000 out=2,000 reason=300 rounds=3 calls=5", summary)

    def test_mcp_tool_failure_is_reported_as_a_warning(self):
        attempts = [
            {
                "ai_test_id": "test_case_34",
                "ai_mode": "performix_mcp",
                "pytest_outcome": "passed",
                "ai_display_scores": "pass",
                "ai_judge_confidences": "high",
                "ai_mcp_tool_calls_succeeded": "3",
                "ai_mcp_tool_calls_failed": "1",
            }
        ]

        console_summary = render_console_summary(attempts, width=200)
        markdown_summary = render_markdown_summary(attempts)

        for summary in (console_summary, markdown_summary):
            self.assertIn("PASS", summary)
            self.assertIn("Warning: 1 MCP tool call failed", summary)


if __name__ == "__main__":
    unittest.main()
