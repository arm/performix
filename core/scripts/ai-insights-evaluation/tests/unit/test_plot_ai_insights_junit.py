# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import csv
import re
from dataclasses import replace
from pathlib import Path
import xml.etree.ElementTree as ET

from matplotlib import pyplot as plt
from matplotlib.ticker import LogFormatterSciNotation, LogLocator

from evaluation_summary import MODE_LABELS, MODE_ORDER
from plot_ai_insights_junit import (
    MODE_PALETTE,
    _configure_ratio_axis,
    _hierarchical_offsets,
    _mode_colors,
    _mode_label,
    _ordered_modes,
    _write_html_report,
    generate_graphs,
    load_observations,
)


def write_junit(
    path: Path,
    *,
    omit: tuple[str, str] | None = None,
    attempts: int = 1,
    include_mcp_rounds: bool = True,
) -> None:
    testsuite = ET.Element("testsuite")
    for test_number in (1, 2):
        test_id = f"test_case_{test_number:02d}"
        for attempt in range(1, attempts + 1):
            for mode in MODE_ORDER:
                mode_number = {
                    "performix_mcp": 2,
                    "performix_mcp_run_query": 3,
                }[mode]
                if omit == (test_id, mode):
                    continue
                testcase = ET.SubElement(
                    testsuite,
                    "testcase",
                    name=f"{test_id}[{mode}][attempt_{attempt:03d}]",
                )
                properties = ET.SubElement(testcase, "properties")
                failed = test_number == 2 and mode == "performix_mcp"
                mcp_calls_failed = (test_number + mode_number + 1) % 2
                mcp_calls_succeeded = test_number * mode_number - mcp_calls_failed
                mcp_tool_rounds = mcp_calls_succeeded + mcp_calls_failed
                mcp_duration = float(test_number * mode_number ** 2)
                mcp_failed_duration = mcp_duration / 4 if mcp_calls_failed else 0.0
                attempt_scale = 1.0 + 0.1 * (attempt - 1)
                values = {
                    "ai_test_id": test_id,
                    "ai_mode": mode,
                    "ai_attempt": str(attempt),
                    "ai_display_scores": "fail" if failed else "pass",
                    "ai_judge_confidences": "high" if test_number == 1 else "medium",
                    "ai_agent_duration_seconds": str(
                        10 * test_number * mode_number * attempt_scale
                    ),
                    "ai_input_tokens": str(
                        int(1000 * test_number * mode_number * attempt_scale)
                    ),
                    "ai_output_tokens": str(
                        int(100 * test_number * mode_number * attempt_scale)
                    ),
                    "ai_reasoning_output_tokens": str(
                        int(50 * test_number * mode_number * attempt_scale)
                    ),
                    "ai_mcp_tool_calls_succeeded": str(mcp_calls_succeeded),
                    "ai_mcp_tool_calls_failed": str(mcp_calls_failed),
                    "ai_mcp_tool_duration_seconds_succeeded": str(
                        (mcp_duration - mcp_failed_duration) * attempt_scale
                    ),
                    "ai_mcp_tool_duration_seconds_failed": str(
                        mcp_failed_duration * attempt_scale
                    ),
                }
                if include_mcp_rounds:
                    values["ai_mcp_tool_rounds"] = str(mcp_tool_rounds)
                for name, value in values.items():
                    ET.SubElement(properties, "property", name=name, value=value)
                if failed:
                    ET.SubElement(testcase, "failure", message="judged failure")
    ET.ElementTree(testsuite).write(path, encoding="utf-8", xml_declaration=True)


def test_assigns_palette_in_discovered_mode_order() -> None:
    modes = ["third_party", "another_mode", "future_mode"]

    colors = _mode_colors(modes)

    assert list(colors) == modes
    assert list(colors.values()) == list(MODE_PALETTE[: len(modes)])


def test_orders_and_colours_historical_mode_variants() -> None:
    modes = _ordered_modes(
        {
            "performix_mcp.10-later",
            "performix_mcp",
            "performix_mcp.2-current",
            "performix_mcp.1-guidance",
        }
    )

    assert modes == [
        "performix_mcp",
        "performix_mcp.1-guidance",
        "performix_mcp.2-current",
        "performix_mcp.10-later",
    ]
    colors = _mode_colors(modes)
    variants = modes[1:]
    assert len({colors[mode] for mode in variants}) == len(variants)
    assert colors["performix_mcp"] == MODE_PALETTE[0]
    assert all(
        colors[variants[0]][channel] >= colors[variants[1]][channel]
        for channel in range(3)
    )
    assert _mode_label(variants[0]) == "Performix MCP\n1 guidance"


def test_wraps_parenthesised_and_long_mode_labels() -> None:
    assert _mode_label("performix_mcp_run_query.6-duration-plus-batching") == (
        "Performix MCP\n(run_query)\n6 duration plus\nbatching"
    )


def test_configures_base_10_log_ratio_axes() -> None:
    with plt.rc_context({"axes.autolimit_mode": "round_numbers"}):
        figure, axis = plt.subplots()
        axis.plot([0, 1], [0.45, 5.4])

        _configure_ratio_axis(axis)

        assert axis.get_yscale() == "log"
        assert axis.yaxis.get_transform().base == 10
        assert isinstance(axis.yaxis.get_major_locator(), LogLocator)
        assert isinstance(axis.yaxis.get_minor_locator(), LogLocator)
        assert isinstance(axis.yaxis.get_major_formatter(), LogFormatterSciNotation)
        assert axis.get_ylim() == (0.1, 10.0)
        plt.close(figure)


def test_aligns_attempts_and_separates_testcases() -> None:
    keys = [
        ("test_case_01", 1),
        ("test_case_01", 2),
        ("test_case_02", 1),
        ("test_case_02", 2),
    ]

    test_offsets, attempt_offsets = _hierarchical_offsets(keys)

    assert attempt_offsets[("test_case_01", 1)] == attempt_offsets[("test_case_01", 2)]
    assert attempt_offsets[("test_case_02", 1)] == attempt_offsets[("test_case_02", 2)]
    assert test_offsets["test_case_01"] != test_offsets["test_case_02"]


def test_loads_paired_observations_and_generates_graphs(tmp_path: Path) -> None:
    junit = tmp_path / "results.xml"
    output_dir = tmp_path / "graphs"
    write_junit(junit)

    observations = load_observations(junit)
    outputs = generate_graphs(observations, output_dir)

    assert len(observations) == 2 * len(MODE_ORDER)
    assert sum(observation.passed for observation in observations) == len(observations) - 1
    performix = next(
        observation
        for observation in observations
        if observation.test_id == "test_case_01" and observation.mode == "performix_mcp"
    )
    assert performix.mcp_tool_duration_seconds == 4.0
    assert performix.mcp_tool_share_percent == 100.0 * 4.0 / 20.0
    assert performix.mcp_calls == 2
    assert performix.mcp_tool_rounds == 2
    failed_performix = next(
        observation
        for observation in observations
        if observation.test_id == "test_case_02"
        and observation.mode == "performix_mcp"
    )
    assert failed_performix.mcp_tool_duration_seconds_succeeded == 6.0
    assert failed_performix.mcp_tool_duration_seconds_failed == 2.0
    assert failed_performix.mcp_tool_duration_seconds == 8.0
    expected_bases = {
        "quality-matrix",
        "runtime",
        "input-tokens",
        "output-tokens",
        "reasoning-tokens",
        "mcp-calls",
        "mcp-rounds",
        "mcp-call-outcomes",
        "mcp-tool-duration",
        "mcp-tool-duration-outcomes",
        "mcp-tool-share",
        "cost-quality",
    }
    assert {path.stem for path in outputs if path.suffix == ".svg"} == expected_bases
    assert {path.stem for path in outputs if path.suffix == ".png"} == expected_bases
    assert output_dir / "index.html" in outputs
    assert all(path.stat().st_size > 0 for path in outputs)
    with (output_dir / "observations.csv").open(encoding="utf-8", newline="") as source:
        rows = list(csv.DictReader(source))
    assert len(rows) == len(observations)
    performix_row = next(
        row for row in rows
        if row["test_id"] == "test_case_01" and row["mode"] == "performix_mcp"
    )
    assert performix_row["mcp_tool_duration_seconds"] == "4.0"
    assert performix_row["mcp_tool_duration_seconds_succeeded"] == "4.0"
    assert performix_row["mcp_tool_duration_seconds_failed"] == "0.0"
    assert performix_row["mcp_tool_share_percent"] == "20.0"
    assert performix_row["mcp_calls_succeeded"] == "2"
    assert performix_row["mcp_calls_failed"] == "0"
    assert performix_row["mcp_tool_rounds"] == "2"
    html = (output_dir / "index.html").read_text(encoding="utf-8")
    assert "<title>AI Insights mode comparison</title>" in html
    assert 'src="quality-matrix.svg"' in html
    assert 'src="cost-quality.svg"' in html
    assert 'src="runtime-repeatability.svg"' not in html
    assert '<section id="runtime-repeatability">' in html
    assert "Omitted because the report contains only one attempt" in html
    assert 'href="observations.csv"' in html
    assert "End-to-end runtime" in html
    assert "MCP tool calls" in html
    assert "Sequential MCP tool rounds" in html
    assert "MCP call outcomes" in html
    assert "MCP tool time" in html
    assert "MCP tool time by outcome" in html
    assert "Observation runtime" in html
    assert "Median test runtime" in html
    for figure_name in ("quality-matrix", "runtime", "cost-quality"):
        svg = (output_dir / f"{figure_name}.svg").read_text(encoding="utf-8")
        assert "Open circle: non-high-confidence pass" in svg
    runtime_svg = (output_dir / "runtime.svg").read_text(encoding="utf-8")
    assert "small labels mark the first and last testcase" in runtime_svg
    for figure_name, panel_labels in (
        ("mcp-call-outcomes", ("Successful calls", "Unsuccessful calls")),
        (
            "mcp-tool-duration-outcomes",
            ("Successful call time", "Unsuccessful call time"),
        ),
    ):
        svg = (output_dir / f"{figure_name}.svg").read_text(encoding="utf-8")
        assert all(label in svg for label in panel_labels)
        assert "Each circle is one testcase and attempt" in svg
        assert "Cross: failure" not in svg
    for mode in MODE_ORDER:
        assert MODE_LABELS[mode] in html


def test_older_reports_without_mcp_rounds_remain_graphable(tmp_path: Path) -> None:
    junit = tmp_path / "results.xml"
    output_dir = tmp_path / "graphs"
    write_junit(junit, include_mcp_rounds=False)

    observations = load_observations(junit)
    outputs = generate_graphs(observations, output_dir)

    assert all(observation.mcp_tool_rounds is None for observation in observations)
    assert "mcp-rounds" not in {
        path.stem for path in outputs if path.suffix == ".svg"
    }


def test_html_summary_reports_total_and_median_runtime(tmp_path: Path) -> None:
    junit = tmp_path / "results.xml"
    write_junit(junit)
    template = load_observations(junit)[0]
    observations = [
        replace(template, test_id="test_case_01", attempt=1, duration_seconds=1.0),
        replace(template, test_id="test_case_01", attempt=2, duration_seconds=2.0),
        replace(template, test_id="test_case_01", attempt=3, duration_seconds=3.0),
        replace(template, test_id="test_case_02", attempt=1, duration_seconds=100.0),
    ]
    report = tmp_path / "index.html"

    _write_html_report(observations, [template.mode], set(), report)

    html = report.read_text(encoding="utf-8")
    assert "<td>106.0 s</td>" in html
    assert "<td>51.0 s</td>" in html
    assert "<td>1–3</td>" in html
    assert "<td>51.0 s</td><td>100.0%</td><td>100.0%</td>" in html


def test_groups_repeated_attempts_and_reports_runtime_variation(tmp_path: Path) -> None:
    junit = tmp_path / "results.xml"
    output_dir = tmp_path / "graphs"
    write_junit(junit, attempts=3)

    observations = load_observations(junit)
    outputs = generate_graphs(observations, output_dir)

    assert len(observations) == 6 * len(MODE_ORDER)
    assert output_dir / "runtime-repeatability.svg" in outputs
    repeatability_svg = (output_dir / "runtime-repeatability.svg").read_text(
        encoding="utf-8"
    )
    assert "Runtime repeatability" in repeatability_svg
    assert "Vertical lines show the minimum-to-maximum range" in repeatability_svg
    quality_svg = (output_dir / "quality-matrix.svg").read_text(encoding="utf-8")
    assert "test_case_01" in quality_svg
    assert "test_case_01 / 1" not in quality_svg
    assert "Attempts are ordered from left to right" in quality_svg
    html = (output_dir / "index.html").read_text(encoding="utf-8")
    assert "<td>3</td>" in html
    assert "<td>18.2%</td>" in html


def test_separates_repeatability_figure_and_panel_titles(tmp_path: Path) -> None:
    junit = tmp_path / "results.xml"
    output_dir = tmp_path / "graphs"
    write_junit(junit, attempts=3)

    observations = [
        observation
        for observation in load_observations(junit)
        if observation.mode == "performix_mcp" and observation.test_id == "test_case_01"
    ]
    generate_graphs(observations, output_dir)

    svg = (output_dir / "runtime-repeatability.svg").read_text(encoding="utf-8")
    panel_title = re.search(r'y="([0-9.]+)"[^>]*>Performix MCP</text>', svg)
    figure_title = re.search(
        r'y="([0-9.]+)"[^>]*>Runtime repeatability</text>', svg
    )
    testcase_label = re.search(
        r'translate\([0-9.]+ ([0-9.]+)\) rotate\(-45\)">test_case_01</text>',
        svg,
    )
    caption = re.search(
        r'translate\([0-9.]+ ([0-9.]+)\)">Each circle is one attempt', svg
    )

    assert panel_title is not None
    assert figure_title is not None
    assert testcase_label is not None
    assert caption is not None
    assert abs(float(panel_title.group(1)) - float(figure_title.group(1))) > 14
    assert float(caption.group(1)) - float(testcase_label.group(1)) > 14


def test_allows_a_missing_mode_observation(tmp_path: Path) -> None:
    junit = tmp_path / "results.xml"
    output_dir = tmp_path / "graphs"
    write_junit(junit, omit=("test_case_02", MODE_ORDER[-1]))

    observations = load_observations(junit)
    outputs = generate_graphs(observations, output_dir)

    assert len(observations) == 2 * len(MODE_ORDER) - 1
    assert output_dir / "quality-matrix.svg" in outputs
    assert output_dir / "index.html" in outputs


def test_html_omits_comparison_without_performix_baseline(tmp_path: Path) -> None:
    junit = tmp_path / "results.xml"
    output_dir = tmp_path / "graphs"
    write_junit(junit)
    observations = [
        observation
        for observation in load_observations(junit)
        if observation.mode != "performix_mcp"
    ]

    outputs = generate_graphs(observations, output_dir)

    assert output_dir / "cost-quality.svg" not in outputs
    html = (output_dir / "index.html").read_text(encoding="utf-8")
    assert 'src="cost-quality.svg"' not in html
