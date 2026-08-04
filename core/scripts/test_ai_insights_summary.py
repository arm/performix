# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import sys
import unittest
from pathlib import Path

MODULE_DIR = Path(__file__).resolve().parent
HARNESS_DIR = MODULE_DIR / "ai-insights-evaluation"
sys.path.append(str(HARNESS_DIR))

from evaluation_summary import render_console_summary, render_markdown_summary


SYNTHETIC_ATTEMPTS = [
    {
        "ai_test_id": "test_case_03",
        "ai_mode": "rest",
        "ai_attempt": "1",
        "pytest_outcome": "passed",
        "ai_judge_confidences": "high",
        "ai_agent_duration_seconds": "28.7",
    },
    {
        "ai_test_id": "test_case_03",
        "ai_mode": "hackathon_mcp",
        "ai_attempt": "1",
        "pytest_outcome": "passed",
        "ai_judge_confidences": "medium",
        "ai_agent_duration_seconds": "19.2",
        "ai_input_tokens": "44426",
        "ai_output_tokens": "1002",
        "ai_reasoning_output_tokens": "608",
        "ai_mcp_tool_calls_succeeded": "1",
        "ai_mcp_tool_calls_failed": "0",
        "ai_tool_output_truncation_markers": "1",
        "ai_tool_output_truncated_tokens": "1234",
        "ai_tool_output_truncation_severity": "warning",
    },
    {
        "ai_test_id": "test_case_03",
        "ai_mode": "hackathon_mcp",
        "ai_attempt": "2",
        "pytest_outcome": "failed",
        "ai_judge_confidences": "high",
        "ai_agent_duration_seconds": "20.3",
        "ai_input_tokens": "43000",
        "ai_output_tokens": "998",
        "ai_reasoning_output_tokens": "500",
        "ai_mcp_tool_calls_succeeded": "0",
        "ai_mcp_tool_calls_failed": "1",
    },
    {
        "ai_test_id": "test_case_03",
        "ai_mode": "performix_mcp",
        "ai_attempt": "1",
        "pytest_outcome": "failed",
        "ai_judge_confidences": "high",
        "ai_agent_duration_seconds": "121.4",
        "ai_input_tokens": "35136",
        "ai_output_tokens": "1273",
        "ai_reasoning_output_tokens": "587",
        "ai_mcp_tool_calls_succeeded": "4",
        "ai_mcp_tool_calls_failed": "0",
        "ai_tool_output_truncation_markers": "2",
        "ai_tool_output_truncated_tokens": "4096",
        "ai_tool_output_truncation_severity": "error",
    },
    {
        "ai_test_id": "test_case_16",
        "ai_mode": "rest",
        "ai_attempt": "1",
        "pytest_outcome": "passed",
        "ai_judge_confidences": "high",
        "ai_agent_duration_seconds": "39.2",
    },
]


class AiInsightsSummaryRenderTests(unittest.TestCase):
    def test_render_console_summary_from_attempt_records(self):
        summary = render_console_summary(SYNTHETIC_ATTEMPTS, width=240, color=False)

        self.assertIn("Testcase", summary)
        self.assertIn("REST", summary)
        self.assertIn("Hackathon", summary)
        self.assertIn("Performix", summary)
        self.assertIn("test_case_03", summary)
        self.assertIn("test_case_16", summary)
        self.assertIn("PASS (high)", summary)
        self.assertIn("attempt 1 PASS, attempt 2 FAIL", summary)
        self.assertIn("(medium,high)", summary)
        self.assertIn("FAIL (high)", summary)
        self.assertIn("28.7s", summary)
        self.assertIn("39.5s", summary)
        self.assertIn("121s", summary)
        self.assertIn("tokens in=87,426 out=2,000", summary)
        self.assertIn("reason=1,108 calls=2", summary)
        self.assertIn("⚠️  Warning:", summary)
        self.assertIn("1,234 tokens truncated", summary)
        self.assertIn("tokens in=35,136 out=1,273", summary)
        self.assertIn("reason=587 calls=4", summary)
        self.assertIn("calls=4", summary)
        self.assertIn("❌ Error: 4,096", summary)
        self.assertIn("tokens truncated", summary)
        token_line = next(
            line for line in summary.splitlines() if "reason=1,108 calls=2" in line
        )
        truncation_line = next(
            line for line in summary.splitlines() if "⚠️  Warning:" in line
        )
        self.assertNotIn("⚠️  Warning:", token_line)
        self.assertNotIn("reason=1,108 calls=2", truncation_line)
        self.assertNotIn("❌ Error:", token_line)
        self.assertNotIn("reason=587 calls=4", truncation_line)

    def test_render_markdown_summary_from_attempt_records(self):
        summary = render_markdown_summary(SYNTHETIC_ATTEMPTS)

        self.assertIn("<table>", summary)
        self.assertIn('<th rowspan="2">Testcase</th>', summary)
        self.assertIn('<th colspan="2">REST</th>', summary)
        self.assertIn('<th colspan="3">Hackathon MCP</th>', summary)
        self.assertIn('<th colspan="3">Performix MCP</th>', summary)
        self.assertIn("<td>test_case_03</td>", summary)
        self.assertIn("<td>test_case_16</td>", summary)
        self.assertIn("✅ <strong>PASS</strong> (high)", summary)
        self.assertIn("❌ <strong>attempt 1 PASS, attempt 2 FAIL</strong> (medium,high)", summary)
        self.assertIn("❌ <strong>FAIL</strong> (high)", summary)
        self.assertIn("<td>28.7s</td>", summary)
        self.assertIn("<td>39.5s</td>", summary)
        self.assertIn("<td>121s</td>", summary)
        self.assertIn(
            "<td>tokens in=87,426 out=2,000 reason=1,108 calls=2<br>⚠️  Warning: 1,234 tokens truncated</td>",
            summary,
        )
        self.assertIn(
            "<td>tokens in=35,136 out=1,273 reason=587 calls=4<br>❌ Error: 4,096 tokens truncated</td>",
            summary,
        )
        self.assertEqual(6, summary.count("<td>-</td>"))

    def test_render_summary_lists_mixed_attempt_outcomes(self):
        attempts = [
            {
                "ai_test_id": "test_case_99",
                "ai_mode": "rest",
                "ai_attempt": "1",
                "pytest_outcome": "error",
                "ai_agent_duration_seconds": "1.0",
            },
            {
                "ai_test_id": "test_case_99",
                "ai_mode": "rest",
                "ai_attempt": "2",
                "pytest_outcome": "skipped",
                "ai_agent_duration_seconds": "2.0",
            },
        ]

        console_summary = render_console_summary(attempts, width=120, color=False)
        markdown_summary = render_markdown_summary(attempts)

        self.assertIn("attempt 1 ERROR, attempt 2 SKIPPED", console_summary)
        self.assertIn("❌ <strong>attempt 1 ERROR, attempt 2 SKIPPED</strong>", markdown_summary)
        self.assertIn("<td>3.0s</td>", markdown_summary)

    def test_render_summary_collapses_expected_repeated_xfails(self):
        attempts = [
            {
                "ai_test_id": "test_case_99",
                "ai_mode": "rest",
                "ai_attempt": "1",
                "pytest_outcome": "xfailed",
                "ai_agent_duration_seconds": "1.0",
            },
            {
                "ai_test_id": "test_case_99",
                "ai_mode": "rest",
                "ai_attempt": "2",
                "pytest_outcome": "xfailed",
                "ai_agent_duration_seconds": "2.0",
            },
        ]

        console_summary = render_console_summary(attempts, width=120, color=False)
        color_summary = render_console_summary(attempts, width=120, color=True)
        markdown_summary = render_markdown_summary(attempts)

        self.assertIn("XFAIL", console_summary)
        self.assertIn("\x1b[1;33mXFAIL\x1b[0m", color_summary)
        self.assertIn("🟡 <strong>XFAIL</strong>", markdown_summary)
        self.assertNotIn("attempt 1", markdown_summary)
        self.assertIn("<td>3.0s</td>", markdown_summary)

    def test_render_summary_collapses_repeated_non_pass_outcomes(self):
        attempts = [
            {
                "ai_test_id": "test_case_98",
                "ai_mode": "rest",
                "ai_attempt": "1",
                "pytest_outcome": "skipped",
                "ai_agent_duration_seconds": "1.0",
            },
            {
                "ai_test_id": "test_case_98",
                "ai_mode": "rest",
                "ai_attempt": "2",
                "pytest_outcome": "skipped",
                "ai_agent_duration_seconds": "2.0",
            },
            {
                "ai_test_id": "test_case_99",
                "ai_mode": "rest",
                "ai_attempt": "1",
                "pytest_outcome": "error",
                "ai_agent_duration_seconds": "3.0",
            },
            {
                "ai_test_id": "test_case_99",
                "ai_mode": "rest",
                "ai_attempt": "2",
                "pytest_outcome": "error",
                "ai_agent_duration_seconds": "4.0",
            },
        ]

        console_summary = render_console_summary(attempts, width=120, color=False)
        markdown_summary = render_markdown_summary(attempts)

        self.assertIn("SKIPPED", console_summary)
        self.assertIn("ERROR", console_summary)
        self.assertNotIn("attempt 1 SKIPPED", console_summary)
        self.assertNotIn("attempt 1 ERROR", console_summary)
        self.assertIn("⚠️ <strong>SKIPPED</strong>", markdown_summary)
        self.assertIn("🔴 <strong>ERROR</strong>", markdown_summary)
        self.assertNotIn("❌ <strong>SKIPPED</strong>", markdown_summary)
        self.assertNotIn("❌ <strong>ERROR</strong>", markdown_summary)

    def test_render_summary_uses_display_scores_for_expected_failures(self):
        attempts = [
            {
                "ai_test_id": "test_case_14",
                "ai_mode": "rest",
                "ai_attempt": "1",
                "pytest_outcome": "skipped",
                "ai_display_scores": "xfail",
                "ai_agent_duration_seconds": "1.0",
            },
            {
                "ai_test_id": "test_case_15",
                "ai_mode": "rest",
                "ai_attempt": "1",
                "pytest_outcome": "failed",
                "ai_display_scores": "xpass",
                "ai_agent_duration_seconds": "2.0",
            },
        ]

        markdown_summary = render_markdown_summary(attempts)

        self.assertIn("<td>test_case_14</td>", markdown_summary)
        self.assertIn("🟡 <strong>XFAIL</strong>", markdown_summary)
        self.assertIn("<td>test_case_15</td>", markdown_summary)
        self.assertIn("❌ <strong>XPASS</strong>", markdown_summary)


if __name__ == "__main__":
    unittest.main()
