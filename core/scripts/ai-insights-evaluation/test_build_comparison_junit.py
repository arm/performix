# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for combining historical AI Insights JUnit reports."""

from __future__ import annotations

from pathlib import Path
import xml.etree.ElementTree as ET

import pytest

from build_comparison_junit import Iteration, build_comparison_junit
from junit_attempts import attempts_from_junit
from plot_ai_insights_junit import load_observations


def write_junit(
    path: Path,
    *,
    modes: tuple[str, ...],
    attempts: int = 2,
    failed: tuple[str, int, str] | None = None,
) -> None:
    testsuite = ET.Element("testsuite")
    for test_id in ("test_case_01", "test_case_02"):
        for attempt_number in range(1, attempts + 1):
            for mode in modes:
                testcase = ET.SubElement(testsuite, "testcase")
                properties = ET.SubElement(testcase, "properties")
                values = {
                    "ai_test_id": test_id,
                    "ai_mode": mode,
                    "ai_attempt": str(attempt_number),
                    "ai_display_scores": "pass",
                    "ai_judge_confidences": "high",
                    "ai_agent_duration_seconds": str(10 + attempt_number),
                    "ai_input_tokens": "100",
                    "ai_output_tokens": "20",
                    "ai_reasoning_output_tokens": "10",
                    "ai_mcp_tool_calls_succeeded": "1",
                    "ai_mcp_tool_calls_failed": "0",
                    "ai_mcp_tool_rounds": "1",
                    "ai_mcp_tool_duration_seconds_succeeded": "0.5",
                    "ai_mcp_tool_duration_seconds_failed": "0",
                }
                for name, value in values.items():
                    ET.SubElement(properties, "property", name=name, value=value)
                if failed == (test_id, attempt_number, mode):
                    ET.SubElement(testcase, "failure", message="source failure")
    ET.ElementTree(testsuite).write(path, encoding="utf-8", xml_declaration=True)


def test_combines_baseline_and_historical_iterations(tmp_path: Path) -> None:
    baseline = tmp_path / "baseline.xml"
    first = tmp_path / "first.xml"
    second = tmp_path / "second.xml"
    output = tmp_path / "comparison.xml"
    write_junit(
        baseline,
        modes=("performix_mcp", "performix_run_query_schema_mcp"),
        failed=("test_case_01", 2, "performix_mcp"),
    )
    write_junit(first, modes=("performix_mcp_run_query",))
    write_junit(second, modes=("performix_mcp_run_query",))

    count = build_comparison_junit(
        baseline,
        [Iteration("1-guidance", first), Iteration("2-current", second)],
        output,
        included_modes=("performix_run_query_schema_mcp",),
    )

    assert count == 16
    attempts = attempts_from_junit(output)
    assert {attempt["ai_mode"] for attempt in attempts} == {
        "performix_mcp",
        "performix_run_query_schema_mcp",
        "performix_mcp_run_query.1-guidance",
        "performix_mcp_run_query.2-current",
    }
    assert len(load_observations(output)) == count
    baseline_attempts = [
        attempt for attempt in attempts if attempt["ai_mode"] == "performix_mcp"
    ]
    assert [attempt["pytest_outcome"] for attempt in baseline_attempts] == [
        "passed",
        "failed",
        "passed",
        "passed",
    ]
    testsuite = ET.parse(output).getroot()
    assert testsuite.attrib["tests"] == "16"
    assert testsuite.attrib["failures"] == "1"


def test_combines_iterations_from_different_modes(tmp_path: Path) -> None:
    baseline = tmp_path / "baseline.xml"
    iteration = tmp_path / "iteration.xml"
    output = tmp_path / "comparison.xml"
    write_junit(baseline, modes=("performix_mcp",))
    write_junit(
        iteration,
        modes=(
            "performix_run_query_schema_mcp",
            "performix_mcp_run_query",
        ),
    )

    count = build_comparison_junit(
        baseline,
        [
            Iteration("guided", iteration),
            Iteration(
                "unguided",
                iteration,
                "performix_run_query_schema_mcp",
            ),
        ],
        output,
    )

    assert count == 12
    assert {attempt["ai_mode"] for attempt in attempts_from_junit(output)} == {
        "performix_mcp",
        "performix_mcp_run_query.guided",
        "performix_run_query_schema_mcp.unguided",
    }


def test_rejects_iteration_with_different_attempts(tmp_path: Path) -> None:
    baseline = tmp_path / "baseline.xml"
    incomplete = tmp_path / "incomplete.xml"
    write_junit(baseline, modes=("performix_mcp",), attempts=2)
    write_junit(
        incomplete,
        modes=("performix_mcp_run_query",),
        attempts=1,
    )

    with pytest.raises(ValueError, match="do not match the baseline"):
        build_comparison_junit(
            baseline,
            [Iteration("1-incomplete", incomplete)],
            tmp_path / "comparison.xml",
        )


def test_rejects_duplicate_iteration_labels(tmp_path: Path) -> None:
    baseline = tmp_path / "baseline.xml"
    iteration = tmp_path / "iteration.xml"
    write_junit(baseline, modes=("performix_mcp",))
    write_junit(iteration, modes=("performix_mcp_run_query",))

    with pytest.raises(ValueError, match="labels must be unique"):
        build_comparison_junit(
            baseline,
            [Iteration("same", iteration), Iteration("same", iteration)],
            tmp_path / "comparison.xml",
        )


def test_rejects_duplicate_mode_observations(tmp_path: Path) -> None:
    baseline = tmp_path / "baseline.xml"
    iteration = tmp_path / "iteration.xml"
    write_junit(
        baseline,
        modes=("performix_mcp", "performix_mcp"),
    )
    write_junit(iteration, modes=("performix_mcp_run_query",))

    with pytest.raises(ValueError, match="duplicate performix_mcp observation"):
        build_comparison_junit(
            baseline,
            [Iteration("iteration", iteration)],
            tmp_path / "comparison.xml",
        )
