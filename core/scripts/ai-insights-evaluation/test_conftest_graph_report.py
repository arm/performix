# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import pytest

import conftest


class AiInsightsGraphReportTests(unittest.TestCase):
    def test_identifies_ai_insights_collection(self):
        self.assertTrue(
            conftest._is_ai_insights_collection(
                ["test_ai_insights_evaluation.py::test_ai_insights[test_case_01-performix_mcp]"]
            )
        )
        self.assertFalse(
            conftest._is_ai_insights_collection(
                ["test_conftest_graph_report.py::test_unrelated"]
            )
        )

    def test_console_summary_keeps_ci_width_and_complete_metrics(self):
        reports = []
        for mode, tokens, reasoning in (("performix_mcp", "92293", "749"),
                                         ("performix_mcp_run_query", "186181", "1879")):
            reports.append(types.SimpleNamespace(
                nodeid=f"test_ai_insights[test_case_03-{mode}]", when="call", outcome="passed",
                user_properties=list({
                    "ai_test_id": "test_case_03", "ai_mode": mode,
                    "ai_judge_confidences": "high", "ai_agent_duration_seconds": "49.7",
                    "ai_input_tokens": tokens, "ai_output_tokens": "2242",
                    "ai_reasoning_output_tokens": reasoning, "ai_mcp_tool_calls_succeeded": "11",
                    "ai_mcp_tool_calls_failed": "1",
                }.items()),
            ))
        reporter = types.SimpleNamespace(
            stats={"passed": reports}, _screen_width=140,
            _tw=types.SimpleNamespace(fullwidth=80, hasmarkup=False),
            section=Mock(), write_line=Mock(),
        )
        conftest.pytest_terminal_summary(reporter, 0, types.SimpleNamespace())
        summary = "\n".join(call.args[0] for call in reporter.write_line.call_args_list)
        for text in ("test_case_03", "PASS (high)", "49.7s", "in=92,293", "in=186,181",
                     "out=2,242", "reason=749", "reason=1,879", "calls=12"):
            self.assertIn(text, summary)
        self.assertNotIn("…", summary)

    def test_generates_graphs_from_junit_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            junit_xml = root / "ai-insights-evaluation.xml"
            junit_xml.write_text(
                "<testsuites><testsuite><testcase><properties>"
                '<property name="ai_mode" value="performix_mcp" />'
                "</properties></testcase></testsuite></testsuites>",
                encoding="utf-8",
            )
            results_dir = root / "results"
            config = types.SimpleNamespace(
                option=types.SimpleNamespace(xmlpath=str(junit_xml)),
                getoption=Mock(return_value=str(results_dir)),
            )
            plot_module = types.ModuleType("plot_ai_insights_junit")
            plot_module.load_observations = Mock(return_value=["observation"])
            plot_module.generate_graphs = Mock(
                return_value=[root / "graphs" / "index.html"]
            )

            with patch.dict(sys.modules, {"plot_ai_insights_junit": plot_module}):
                report = conftest._generate_graph_report(config)

        resolved_junit_xml = junit_xml.resolve()
        output_dir = (root / "graphs").resolve()
        self.assertEqual(
            {"output_dir": str(output_dir), "file_count": 1},
            report,
        )
        plot_module.load_observations.assert_called_once_with(resolved_junit_xml)
        plot_module.generate_graphs.assert_called_once_with(["observation"], output_dir)

    def test_skips_graphs_without_junit_reporting(self):
        config = types.SimpleNamespace(option=types.SimpleNamespace(xmlpath=None))

        self.assertIsNone(conftest._generate_graph_report(config))

    def test_ci_selected_attempt_still_generates_graphs(self):
        config = types.SimpleNamespace(
            option=types.SimpleNamespace(collectonly=False, ai_selected_nodeids="selected.json"),
        )
        session = types.SimpleNamespace(config=config, exitstatus=pytest.ExitCode.OK)
        with patch.object(conftest, "_generate_graph_report", return_value={"file_count": 1}) as generate:
            conftest.pytest_sessionfinish(session, pytest.ExitCode.OK)
        generate.assert_called_once_with(config)

    def test_skips_graphs_for_junit_without_ai_insights_results(self):
        with tempfile.TemporaryDirectory() as tmp:
            junit_xml = Path(tmp) / "results.xml"
            junit_xml.write_text(
                "<testsuites><testsuite><testcase /></testsuite></testsuites>",
                encoding="utf-8",
            )
            config = types.SimpleNamespace(
                option=types.SimpleNamespace(xmlpath=str(junit_xml)),
            )

            self.assertIsNone(conftest._generate_graph_report(config))

    def test_graph_failure_fails_an_otherwise_successful_session(self):
        config = types.SimpleNamespace(
            option=types.SimpleNamespace(collectonly=False),
        )
        session = types.SimpleNamespace(config=config, exitstatus=pytest.ExitCode.OK)

        with patch.object(
            conftest,
            "_generate_graph_report",
            side_effect=ValueError("no graphable observations"),
        ):
            conftest.pytest_sessionfinish(session, pytest.ExitCode.OK)

        self.assertEqual(pytest.ExitCode.TESTS_FAILED, session.exitstatus)
        self.assertEqual(
            {"error": "no graphable observations"},
            getattr(config, conftest.AI_GRAPH_REPORT_ATTR),
        )

    def test_unexpected_graph_failure_preserves_existing_failure(self):
        config = types.SimpleNamespace(
            option=types.SimpleNamespace(collectonly=False),
        )
        session = types.SimpleNamespace(
            config=config,
            exitstatus=pytest.ExitCode.TESTS_FAILED,
        )

        with patch.object(
            conftest,
            "_generate_graph_report",
            side_effect=RuntimeError("matplotlib failed"),
        ):
            conftest.pytest_sessionfinish(session, pytest.ExitCode.TESTS_FAILED)

        self.assertEqual(pytest.ExitCode.TESTS_FAILED, session.exitstatus)
        self.assertEqual(
            {"error": "matplotlib failed"},
            getattr(config, conftest.AI_GRAPH_REPORT_ATTR),
        )


if __name__ == "__main__":
    unittest.main()
