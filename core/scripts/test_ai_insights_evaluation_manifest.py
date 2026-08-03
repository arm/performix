# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

import pytest

MODULE_DIR = Path(__file__).resolve().parent
HARNESS_DIR = MODULE_DIR / "ai-insights-evaluation"
sys.path.append(str(HARNESS_DIR))
EVALUATION_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_evaluation_test_module",
    HARNESS_DIR / "test_ai_insights_evaluation.py",
)
evaluation = importlib.util.module_from_spec(EVALUATION_SPEC)
sys.modules[EVALUATION_SPEC.name] = evaluation
EVALUATION_SPEC.loader.exec_module(evaluation)

CONFTEST_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_evaluation_conftest_test_module",
    HARNESS_DIR / "conftest.py",
)
conftest = importlib.util.module_from_spec(CONFTEST_SPEC)
sys.modules[CONFTEST_SPEC.name] = conftest
CONFTEST_SPEC.loader.exec_module(conftest)

REST_MODE_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_rest_mode_test_module",
    HARNESS_DIR / "rest_mode.py",
)
rest_mode = importlib.util.module_from_spec(REST_MODE_SPEC)
sys.modules[REST_MODE_SPEC.name] = rest_mode
REST_MODE_SPEC.loader.exec_module(rest_mode)


class FakePytestConfig:
    def __init__(self, act: str, modes: str = ""):
        self.act = act
        self.modes = modes

    def getoption(self, option: str):
        if option == "--ai-act":
            return self.act
        if option == "--ai-modes":
            return self.modes
        raise AssertionError(f"unexpected option: {option}")


class FakeXdistConfig:
    def __init__(self, manifest_path: Path):
        self.option = SimpleNamespace(collectonly=False)
        self.manifest_path = manifest_path

    def getoption(self, option: str):
        if option == "--ai-manifest":
            return str(self.manifest_path)
        raise AssertionError(f"unexpected option: {option}")


class FakeXdistNode:
    def __init__(self, config: FakeXdistConfig):
        self.config = config


def selected_testcases_from_nodeids(manifest_tests: list[dict], nodeids: list[str]):
    with tempfile.TemporaryDirectory() as tmpdir:
        manifest_path = Path(tmpdir) / "manifest.json"
        manifest_path.write_text(json.dumps({"tests": manifest_tests}), encoding="utf-8")
        return conftest._selected_testcases_from_nodeids(
            FakeXdistConfig(manifest_path),
            nodeids,
        )


class AiInsightsManifestSelectionTests(unittest.TestCase):
    def test_iter_manifest_parameters_returns_selected_valid_act(self):
        manifest = {
            "defaults": {"modes": ["hackathon_mcp"]},
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act1"],
                },
                {
                    "id": "test_case_02",
                    "acts": ["act2"],
                },
            ],
        }

        params = evaluation.iter_manifest_parameters(FakePytestConfig("act2"), manifest)

        self.assertEqual(1, len(params))
        self.assertEqual("test_case_02", params[0]["test_case"]["id"])
        self.assertEqual("hackathon_mcp", params[0]["mode"])

    def test_iter_manifest_parameters_uses_requested_modes_for_each_testcase(self):
        manifest = {
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act2"],
                },
            ],
        }

        params = evaluation.iter_manifest_parameters(FakePytestConfig("act2", "rest,hackathon_mcp"), manifest)

        self.assertEqual(["rest", "hackathon_mcp"], [param["mode"] for param in params])

    def test_iter_manifest_parameters_rejects_unknown_act(self):
        manifest = {
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act1"],
                },
            ],
        }

        with self.assertRaises(pytest.UsageError) as ctx:
            evaluation.iter_manifest_parameters(FakePytestConfig("act2"), manifest)

        message = str(ctx.exception)
        self.assertIn("Unknown AI Insights act selection: act2", message)
        self.assertIn("Available acts: act1", message)


class AiInsightsPreRecordedInputTests(unittest.TestCase):
    def test_selected_testcases_from_xdist_nodeids_deduplicates_modes(self):
        testcases = selected_testcases_from_nodeids(
            [
                {"id": "case-alpha", "run_artifact": "case-alpha/latest.zip"},
                {"id": "test_case_27", "run_artifact": "test_case_27/latest.zip"},
            ],
            [
                "test_ai_insights_evaluation.py::test_ai_insights[case-alpha-summary-rest-attempt_001]",
                "test_ai_insights_evaluation.py::test_ai_insights[case-alpha-summary-hackathon_mcp-attempt_001]",
                "test_ai_insights_evaluation.py::test_ai_insights[test_case_27-summary-rest-attempt_001]",
            ],
        )

        self.assertEqual(
            ["case-alpha", "test_case_27"],
            [test_case["id"] for test_case in testcases],
        )

    def test_selected_testcases_from_xdist_nodeids_rejects_unmapped_testcase(self):
        with self.assertRaises(pytest.UsageError) as ctx:
            selected_testcases_from_nodeids(
                [{"id": "test_case_26", "run_artifact": "test_case_26/latest.zip"}],
                [
                    "test_ai_insights_evaluation.py::test_ai_insights[unknown-summary-rest-attempt_001]",
                ],
            )

        self.assertIn("Could not map AI Insights pytest node ID", str(ctx.exception))
        self.assertIn("unknown-summary-rest-attempt_001", str(ctx.exception))

    def test_xdist_collection_prepares_once_from_worker_collection(self):
        nodeids = [
            "test_ai_insights_evaluation.py::test_ai_insights[test_case_26-summary-rest-attempt_001]",
            "test_ai_insights_evaluation.py::test_ai_insights[test_case_26-summary-performix_mcp-attempt_001]",
        ]

        with tempfile.TemporaryDirectory() as tmpdir:
            manifest_path = Path(tmpdir) / "manifest.json"
            manifest_path.write_text(
                json.dumps(
                    {
                        "tests": [
                            {
                                "id": "test_case_26",
                                "run_artifact": "test_case_26/latest.zip",
                            },
                        ],
                    }
                ),
                encoding="utf-8",
            )
            config = FakeXdistConfig(manifest_path)
            prepared = []

            with mock.patch.object(
                conftest,
                "_prepare_selected_testcases",
                side_effect=lambda _config, testcases: prepared.append(
                    [test_case["id"] for test_case in testcases]
                ),
            ):
                conftest.pytest_xdist_node_collection_finished(
                    FakeXdistNode(config),
                    nodeids,
                )
                conftest.pytest_xdist_node_collection_finished(
                    FakeXdistNode(config),
                    nodeids,
                )
                conftest.pytest_xdist_node_collection_finished(
                    FakeXdistNode(config),
                    nodeids,
                )

        self.assertEqual([["test_case_26"]], prepared)

    def test_download_missing_run_artifacts_raises_for_artifactory_misses(self):
        lines = []
        calls = []

        def write_line(line: str, **markup):
            lines.append((line, markup))

        def fail_download(
            artifactory_run_base,
            test_id,
            filename,
            destination_dir,
            destination_name,
            required=True,
        ):
            calls.append((filename, required))
            raise pytest.UsageError("download failed")

        testcases = [
            {
                "id": "test_case_26",
                "run_artifact": "test_case_26/latest.zip",
            },
        ]

        with tempfile.TemporaryDirectory() as tmpdir:
            with mock.patch.object(conftest, "_download_artifactory_input", side_effect=fail_download):
                with self.assertRaises(pytest.UsageError):
                    conftest._download_missing_run_artifacts(
                        testcases,
                        Path(tmpdir),
                        "its.apx-prerecorded-runs/ai-insights-evaluation",
                        write_line,
                    )

        self.assertEqual([("latest.zip", True)], calls)
        self.assertTrue(any("test_case_26: latest.zip" in line for line, _ in lines))
        self.assertFalse(any("WARNING" in line for line, _ in lines))

    def test_prepare_run_inputs_raises_for_missing_local_inputs(self):
        testcases = [{"id": "test_case_26", "run_artifact": "test_case_26/latest.zip"}]

        with tempfile.TemporaryDirectory() as tmpdir, self.assertRaisesRegex(
            pytest.UsageError,
            "(?s)test_case_26 run archive.*test_case_26 source archive",
        ):
            conftest._prepare_run_inputs(
                testcases,
                Path(tmpdir),
                "",
                Path(tmpdir) / "results",
                lambda _line: None,
            )


class AiInsightsExpectedFailureTests(unittest.TestCase):
    def test_expected_failure_for_mode_accepts_mode_overrides(self):
        test_case = {
            "id": "test_case_01",
            "expected_failures": {
                "modes": {
                    "rest": {
                        "expected_failure": True,
                        "reason": "REST path does not expose this evidence yet.",
                    },
                },
            },
        }

        rest_expectation = evaluation.expected_failure_for_mode(test_case, "rest")
        mcp_expectation = evaluation.expected_failure_for_mode(test_case, "hackathon_mcp")

        self.assertTrue(rest_expectation["expected_failure"])
        self.assertEqual("REST path does not expose this evidence yet.", rest_expectation["reason"])
        self.assertFalse(mcp_expectation["expected_failure"])

    def test_expected_failure_for_mode_accepts_testcase_default(self):
        test_case = {
            "id": "test_case_14",
            "expected_failures": {
                "expected_failure": True,
                "reason": "Known profiling-context limitation.",
            },
        }

        expectation = evaluation.expected_failure_for_mode(test_case, "hackathon_mcp")

        self.assertTrue(expectation["expected_failure"])
        self.assertEqual("Known profiling-context limitation.", expectation["reason"])

    def test_display_label_marks_expected_failures(self):
        self.assertEqual("xfail", evaluation.display_label("fail", expected_failure=True))
        self.assertEqual("error", evaluation.display_label("error", expected_failure=True))
        self.assertEqual("xpass", evaluation.display_label("pass", expected_failure=True))
        self.assertEqual("fail", evaluation.display_label("fail", expected_failure=False))

    def test_should_xfail_final_label_only_accepts_judged_failures(self):
        self.assertTrue(evaluation.should_xfail_final_label("fail", expected_failure=True))
        self.assertFalse(evaluation.should_xfail_final_label("error", expected_failure=True))
        self.assertFalse(evaluation.should_xfail_final_label("pass", expected_failure=True))
        self.assertFalse(evaluation.should_xfail_final_label("fail", expected_failure=False))

    def test_tool_output_truncation_policy_is_mode_specific(self):
        invoke_meta = {"truncation_markers": 1}

        self.assertFalse(evaluation.should_fail_on_tool_output_truncation("hackathon_mcp", invoke_meta))
        self.assertEqual("warning", evaluation.tool_output_truncation_severity("hackathon_mcp", invoke_meta))
        self.assertTrue(evaluation.should_fail_on_tool_output_truncation("performix_mcp", invoke_meta))
        self.assertEqual("error", evaluation.tool_output_truncation_severity("performix_mcp", invoke_meta))
        self.assertEqual(
            "",
            evaluation.tool_output_truncation_severity("performix_mcp", {"truncation_markers": 0}),
        )
        self.assertFalse(
            evaluation.should_fail_on_tool_output_truncation("performix_mcp", {"truncation_markers": "unknown"}),
        )
        self.assertEqual(
            "",
            evaluation.tool_output_truncation_severity("performix_mcp", {"truncation_markers": "unknown"}),
        )

    def test_record_evaluation_properties_includes_truncation_metadata(self):
        properties = {}

        evaluation.record_evaluation_properties(
            lambda name, value: properties.setdefault(name, str(value)),
            {
                "test_id": "test_case_03",
                "mode": "hackathon_mcp",
                "attempt": 1,
                "attempts_total": 1,
                "attempts": 1,
                "passes": 1,
                "pass_rate": 1.0,
                "scores": ["pass"],
                "display_scores": ["pass"],
                "expected_failure": False,
                "expected_failure_reason": "",
                "artifact_dir": "/tmp/result",
                "artifact_rel_dir": "test_case_03/hackathon_mcp/attempts/001",
                "aggregate_path": "/tmp/result/aggregate.json",
            },
            [
                {
                    "attempt_dir": Path("/tmp/result"),
                    "run_meta": {"run_id": "run-1", "archive_sha256": "abc"},
                    "score": {"judge": {"label": "pass", "confidence": "high"}},
                    "invoke": {
                        "mode": "hackathon_mcp",
                        "duration_seconds": 1.2,
                        "mcp": {"completed_calls": 1},
                        "truncation_markers": 1,
                        "truncation_marker_token_counts": [1234],
                        "token_usage": [],
                    },
                },
            ],
        )

        self.assertEqual("True", properties["ai_tool_output_truncated"])
        self.assertEqual("1", properties["ai_tool_output_truncation_markers"])
        self.assertEqual("1234", properties["ai_tool_output_truncated_tokens"])
        self.assertEqual("1234", properties["ai_tool_output_truncation_token_counts"])
        self.assertEqual("warning", properties["ai_tool_output_truncation_severity"])


class AiInsightsRestReasoningTests(unittest.TestCase):
    def test_build_openai_request_adds_configured_reasoning(self):
        cfg = {"model": "gpt-test", "reasoning_effort": "high"}

        request, request_meta = rest_mode.build_openai_request({"prompt": "Analyze this."}, cfg)

        self.assertEqual({"effort": "high"}, request["reasoning"])
        self.assertEqual("high", request_meta["configured_reasoning_effort"])
        self.assertEqual("high", request_meta["request_reasoning_effort"])
        self.assertTrue(request_meta["request_reasoning_effort_sent"])

    def test_build_openai_request_rejects_empty_reasoning_effort(self):
        cfg = {"model": "gpt-test", "reasoning_effort": ""}

        with self.assertRaises(ValueError):
            rest_mode.build_openai_request({"prompt": "Analyze this."}, cfg)


if __name__ == "__main__":
    unittest.main()
