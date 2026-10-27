#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Plot paired quality and cost observations from AI Insights JUnit XML."""

from __future__ import annotations

import argparse
import csv
from dataclasses import dataclass
from html import escape as html_escape
from pathlib import Path
import re
from statistics import median
from textwrap import wrap
from typing import Callable

import matplotlib

matplotlib.use("Agg")

from matplotlib import pyplot as plt  # noqa: E402
from matplotlib.axes import Axes  # noqa: E402
from matplotlib.ticker import FuncFormatter  # noqa: E402

from evaluation_summary import MODE_LABELS as SUMMARY_MODE_LABELS  # noqa: E402
from evaluation_summary import MODE_ORDER as SUMMARY_MODE_ORDER  # noqa: E402
from junit_attempts import attempts_from_junit  # noqa: E402


PERFORMIX_MODE = "performix_mcp"

PASS_COLOR = "#252525"
FAIL_COLOR = "#111111"
CONNECTION_COLOR = "#D0D0D0"
GRID_COLOR = "#E5E5E5"
LOG_MAJOR_GRID_COLOR = "#D0D0D0"
LOG_MINOR_GRID_COLOR = "#ECECEC"
REFERENCE_LINE_COLOR = "#6F6F6F"
MODE_PALETTE = tuple(matplotlib.colormaps["tab10"].colors)
MARKER_LEGEND = (
    "Filled circle: high-confidence pass   "
    "Open circle: non-high-confidence pass   "
    "Cross: failure"
)
MODE_LABEL_WRAP_WIDTH = 16


@dataclass(frozen=True)
class Observation:
    test_id: str
    mode: str
    attempt: int
    score: str
    confidence: str
    duration_seconds: float
    input_tokens: int
    output_tokens: int
    reasoning_tokens: int
    mcp_tool_rounds: int | None
    mcp_calls_succeeded: int
    mcp_calls_failed: int
    mcp_tool_duration_seconds_succeeded: float
    mcp_tool_duration_seconds_failed: float

    @property
    def pair_key(self) -> tuple[str, int]:
        return (self.test_id, self.attempt)

    @property
    def passed(self) -> bool:
        return self.score == "pass"

    @property
    def mcp_calls(self) -> int:
        return self.mcp_calls_succeeded + self.mcp_calls_failed

    @property
    def mcp_tool_duration_seconds(self) -> float:
        return (
            self.mcp_tool_duration_seconds_succeeded
            + self.mcp_tool_duration_seconds_failed
        )

    @property
    def mcp_tool_share_percent(self) -> float:
        if self.duration_seconds <= 0:
            return 0.0
        return 100.0 * self.mcp_tool_duration_seconds / self.duration_seconds


@dataclass(frozen=True)
class Metric:
    filename: str
    title: str
    ylabel: str
    field: str
    formatter: Callable[[float, int], str]
    show_relative: bool = True
    omit_zero: bool = False


def comma_formatter(value: float, _position: int) -> str:
    return f"{value:,.0f}"


def decimal_formatter(value: float, _position: int) -> str:
    return f"{value:g}"


def percent_formatter(value: float, _position: int) -> str:
    return f"{value:.0f}%"


METRICS = (
    Metric("runtime", "End-to-end runtime", "Seconds", "duration_seconds", decimal_formatter),
    Metric("input-tokens", "Input token volume", "Input tokens", "input_tokens", comma_formatter),
    Metric(
        "output-tokens",
        "Non-reasoning output token volume",
        "Non-reasoning output tokens",
        "output_tokens",
        comma_formatter,
    ),
    Metric(
        "reasoning-tokens",
        "Reasoning token volume",
        "Reasoning tokens",
        "reasoning_tokens",
        comma_formatter,
    ),
    Metric(
        "mcp-calls",
        "MCP tool calls",
        "Calls",
        "mcp_calls",
        comma_formatter,
        omit_zero=True,
    ),
    Metric(
        "mcp-rounds",
        "Sequential MCP tool rounds",
        "Rounds",
        "mcp_tool_rounds",
        comma_formatter,
        omit_zero=True,
    ),
    Metric(
        "mcp-tool-duration",
        "MCP tool time",
        "Seconds",
        "mcp_tool_duration_seconds",
        decimal_formatter,
        omit_zero=True,
    ),
    Metric(
        "mcp-tool-share",
        "MCP tool time as a share of invocation",
        "Percent of invocation",
        "mcp_tool_share_percent",
        percent_formatter,
        show_relative=False,
        omit_zero=True,
    ),
)

FIGURE_DESCRIPTIONS = {
    "quality-matrix": (
        "Quality by testcase",
        "Each row is a testcase and attempt. The matrix shows whether each available mode passed, "
        "and the confidence assigned by the judge. A dash means that the JUnit report contains no "
        "observation for that mode and testcase.",
    ),
    "runtime": (
        "End-to-end runtime",
        "Elapsed agent runtime for each testcase and mode. This measures the complete evaluation "
        "attempt rather than the execution time of an individual MCP request.",
    ),
    "runtime-repeatability": (
        "Runtime repeatability",
        "Each attempt is divided by the median runtime for its testcase and mode. Values near "
        "1× indicate stable runtime. Vertical lines show the minimum-to-maximum range within a "
        "testcase. A single attempt is plotted at 1× but cannot measure repeatability.",
    ),
    "input-tokens": (
        "Input token volume",
        "Total model input tokens reported for each attempt. The values include repeated context "
        "sent across model calls; cached tokens are not deducted.",
    ),
    "output-tokens": (
        "Non-reasoning output token volume",
        "Model output tokens reported for each attempt after subtracting reasoning tokens. Reasoning "
        "tokens are plotted separately.",
    ),
    "reasoning-tokens": (
        "Reasoning token volume",
        "Reasoning tokens reported for each attempt. These are shown separately because their cost "
        "and availability depend on the model and evaluation configuration.",
    ),
    "mcp-calls": (
        "MCP tool calls",
        "The number of MCP tool calls in each attempt, including failed calls. Modes that do not use MCP can "
        "legitimately report zero calls; those zero observations are omitted from this MCP-only figure.",
    ),
    "mcp-rounds": (
        "Sequential MCP tool rounds",
        "The number of sequential MCP tool-call rounds in each attempt. Calls whose execution "
        "overlaps count as one round. Modes that made no MCP calls are omitted.",
    ),
    "mcp-call-outcomes": (
        "MCP call outcomes",
        "Successful and unsuccessful MCP calls in each attempt. The aligned panels use the same "
        "linear scale, so their magnitudes can be compared directly. Modes that made no MCP calls "
        "are omitted; zero unsuccessful calls remain visible because they are meaningful. Marker "
        "shape does not encode the overall judge result, which is shown in the quality matrix.",
    ),
    "mcp-tool-duration": (
        "MCP tool time",
        "The sum of elapsed time for every MCP tool call in an attempt, including failed calls. "
        "This measures the time observed by Codex around each call, rather than only the time spent "
        "inside Performix.",
    ),
    "mcp-tool-duration-outcomes": (
        "MCP tool time by outcome",
        "Codex-observed MCP tool time split between successful and unsuccessful calls. The aligned "
        "panels use the same linear scale and should be read with the total MCP tool-time figure. "
        "Marker shape does not encode the overall judge result.",
    ),
    "mcp-tool-share": (
        "MCP tool time as a share of invocation",
        "Aggregate MCP tool time divided by the end-to-end Codex invocation time. Read this with "
        "the absolute runtime and MCP tool-time figures: the same percentage can represent different "
        "absolute costs. Concurrent tool calls can make the aggregate exceed 100%.",
    ),
    "cost-quality": (
        "Cost and quality relative to Performix MCP",
        "Runtime and input-token costs for each mode relative to the matching Performix MCP "
        "testcase and attempt. The reference lines mark equal cost; points above or to the right "
        "used more tokens or time respectively.",
    ),
}

FIGURE_ORDER = tuple(FIGURE_DESCRIPTIONS)


def load_observations(junit_xml: Path) -> list[Observation]:
    """Load graphable observations from one JUnit report."""
    observations = []
    for attempt in attempts_from_junit(junit_xml):
        mode = attempt.get("ai_mode", "")
        if not mode:
            continue
        observations.append(
            Observation(
                test_id=_required(attempt, "ai_test_id"),
                mode=mode,
                attempt=_integer(attempt, "ai_attempt"),
                score=_single_value(attempt, "ai_display_scores").lower(),
                confidence=_single_value(attempt, "ai_judge_confidences", required=False).lower(),
                duration_seconds=_number(attempt, "ai_agent_duration_seconds"),
                input_tokens=_integer(attempt, "ai_input_tokens"),
                output_tokens=_integer(attempt, "ai_output_tokens"),
                reasoning_tokens=_integer(attempt, "ai_reasoning_output_tokens"),
                mcp_tool_rounds=_optional_integer(attempt, "ai_mcp_tool_rounds"),
                mcp_calls_succeeded=_integer(
                    attempt, "ai_mcp_tool_calls_succeeded"
                ),
                mcp_calls_failed=_integer(attempt, "ai_mcp_tool_calls_failed"),
                mcp_tool_duration_seconds_succeeded=_number(
                    attempt, "ai_mcp_tool_duration_seconds_succeeded"
                ),
                mcp_tool_duration_seconds_failed=_number(
                    attempt, "ai_mcp_tool_duration_seconds_failed"
                ),
            )
        )
    if not observations:
        raise ValueError("JUnit report contains no AI Insights observations")
    _group_observations(observations)
    return sorted(observations, key=_observation_sort_key)


def _required(attempt: dict[str, str], key: str) -> str:
    value = attempt.get(key, "").strip()
    if not value:
        raise ValueError(f"missing JUnit property: {key}")
    return value


def _single_value(attempt: dict[str, str], key: str, *, required: bool = True) -> str:
    values = [value.strip() for value in attempt.get(key, "").split(",") if value.strip()]
    if not values and not required:
        return ""
    if len(values) != 1:
        raise ValueError(f"expected one {key} value, found {values!r}")
    return values[0]


def _number(attempt: dict[str, str], key: str) -> float:
    value = _required(attempt, key)
    try:
        return float(value)
    except ValueError as exc:
        raise ValueError(f"invalid numeric JUnit property {key}: {value!r}") from exc


def _integer(attempt: dict[str, str], key: str) -> int:
    value = _number(attempt, key)
    if not value.is_integer():
        raise ValueError(f"expected integer JUnit property {key}, found {value}")
    return int(value)


def _optional_integer(attempt: dict[str, str], key: str) -> int | None:
    if not attempt.get(key, "").strip():
        return None
    return _integer(attempt, key)


def _test_sort_key(test_id: str) -> tuple[int, str]:
    prefix = "test_case_"
    suffix = test_id[len(prefix) :] if test_id.startswith(prefix) else ""
    return (int(suffix), test_id) if suffix.isdigit() else (10**9, test_id)


def _observation_sort_key(observation: Observation) -> tuple[int, str, int, int, str]:
    test_number, test_id = _test_sort_key(observation.test_id)
    try:
        mode_index = SUMMARY_MODE_ORDER.index(observation.mode)
    except ValueError:
        mode_index = len(SUMMARY_MODE_ORDER)
    return (test_number, test_id, observation.attempt, mode_index, observation.mode)


def _group_observations(observations: list[Observation]) -> dict[tuple[str, int], dict[str, Observation]]:
    groups: dict[tuple[str, int], dict[str, Observation]] = {}
    for observation in observations:
        modes = groups.setdefault(observation.pair_key, {})
        if observation.mode in modes:
            raise ValueError(f"duplicate observation for {observation.pair_key} and {observation.mode}")
        modes[observation.mode] = observation
    return groups


def _ordered_modes(modes: set[str]) -> list[str]:
    families: dict[str, list[str]] = {}
    for mode in modes:
        family, _variant = _mode_parts(mode)
        families.setdefault(family, []).append(mode)
    ordered_families = [mode for mode in SUMMARY_MODE_ORDER if mode in families]
    ordered_families.extend(sorted(families.keys() - set(ordered_families)))
    return [
        mode
        for family in ordered_families
        for mode in sorted(families[family], key=_mode_variant_sort_key)
    ]


def _mode_parts(mode: str) -> tuple[str, str]:
    family, separator, variant = mode.partition(".")
    return (family, variant) if separator else (mode, "")


def _mode_variant_sort_key(mode: str) -> tuple[int, tuple[tuple[int, object], ...]]:
    _family, variant = _mode_parts(mode)
    if not variant:
        return 0, ()
    parts = tuple(
        (0, int(part)) if part.isdigit() else (1, part.lower())
        for part in re.split(r"(\d+)", variant)
        if part
    )
    return 1, parts


def _mode_label(mode: str) -> str:
    family, variant = _mode_parts(mode)
    label = SUMMARY_MODE_LABELS.get(family, family)
    label = label.replace(" (", "\n(", 1)
    if variant:
        variant_label = variant.replace("-", " ").replace("_", " ")
        variant_lines = wrap(
            variant_label,
            width=MODE_LABEL_WRAP_WIDTH,
            break_long_words=False,
            break_on_hyphens=False,
        )
        label = "\n".join((label, *variant_lines))
    return label


def _mode_label_line_count(modes: list[str]) -> int:
    return max(_mode_label(mode).count("\n") + 1 for mode in modes)


def generate_graphs(observations: list[Observation], output_dir: Path) -> list[Path]:
    """Generate quality and cost figures, source CSV and an HTML report."""
    groups = _group_observations(observations)
    modes = _ordered_modes({observation.mode for observation in observations})
    mode_colors = _mode_colors(modes)
    output_dir.mkdir(parents=True, exist_ok=True)
    outputs = []
    with plt.rc_context(
        {
            "axes.facecolor": "white",
            "figure.facecolor": "white",
            "font.size": 9,
            "axes.titlesize": 13,
            "axes.labelsize": 9,
            "axes.autolimit_mode": "round_numbers",
            "xtick.labelsize": 8,
            "ytick.labelsize": 8,
            "svg.fonttype": "none",
        }
    ):
        outputs.extend(_plot_quality_matrix(groups, modes, mode_colors, output_dir))
        for metric in METRICS:
            outputs.extend(_plot_metric(groups, modes, mode_colors, metric, output_dir))
        outputs.extend(_plot_runtime_repeatability(groups, modes, mode_colors, output_dir))
        outputs.extend(
            _plot_mcp_outcomes(
                groups,
                modes,
                mode_colors,
                output_dir,
                filename="mcp-call-outcomes",
                title="MCP call outcomes",
                panels=(
                    ("mcp_calls_succeeded", "Successful calls"),
                    ("mcp_calls_failed", "Unsuccessful calls"),
                ),
                ylabel="Calls",
                formatter=comma_formatter,
            )
        )
        outputs.extend(
            _plot_mcp_outcomes(
                groups,
                modes,
                mode_colors,
                output_dir,
                filename="mcp-tool-duration-outcomes",
                title="MCP tool time by outcome",
                panels=(
                    (
                        "mcp_tool_duration_seconds_succeeded",
                        "Successful call time",
                    ),
                    (
                        "mcp_tool_duration_seconds_failed",
                        "Unsuccessful call time",
                    ),
                ),
                ylabel="Seconds",
                formatter=decimal_formatter,
            )
        )
        outputs.extend(_plot_cost_quality(groups, modes, mode_colors, output_dir))
    csv_path = output_dir / "observations.csv"
    _write_observations_csv(observations, csv_path)
    outputs.append(csv_path)
    html_path = output_dir / "index.html"
    figure_names = {path.stem for path in outputs if path.suffix == ".svg"}
    _write_html_report(observations, modes, figure_names, html_path)
    outputs.append(html_path)
    return outputs


def _plot_quality_matrix(
    groups: dict[tuple[str, int], dict[str, Observation]],
    modes: list[str],
    mode_colors: dict[str, tuple[float, float, float]],
    output_dir: Path,
) -> list[Path]:
    keys = sorted(groups, key=lambda key: (*_test_sort_key(key[0]), key[1]))
    test_ids = sorted({test_id for test_id, _attempt in keys}, key=_test_sort_key)
    attempts_by_test = {
        test_id: sorted(attempt for key_test_id, attempt in keys if key_test_id == test_id)
        for test_id in test_ids
    }
    y_positions = {
        test_id: len(test_ids) - index - 1
        for index, test_id in enumerate(test_ids)
    }
    label_lines = _mode_label_line_count(modes) + 1
    figure, axis = plt.subplots(
        figsize=(
            max(7.2, 1.45 * len(modes)),
            max(4.0, 1.5 + len(test_ids) * 0.36)
            + 0.18 * max(0, label_lines - 3),
        )
    )
    for test_id in test_ids:
        y = y_positions[test_id]
        attempts = attempts_by_test[test_id]
        attempt_offsets = _point_offsets(len(attempts), width=0.24)
        for column, mode in enumerate(modes):
            for attempt, offset in zip(attempts, attempt_offsets):
                observation = groups[(test_id, attempt)].get(mode)
                x = column + offset
                if observation is None:
                    axis.text(
                        x,
                        y,
                        "–",
                        ha="center",
                        va="center",
                        color="#AAAAAA",
                        fontsize=11,
                    )
                else:
                    _plot_quality_marker(
                        axis,
                        x,
                        y,
                        observation,
                        color=mode_colors[mode],
                        size=42,
                    )
    axis.set_yticks(
        [y_positions[test_id] for test_id in test_ids],
        labels=test_ids,
    )
    mode_labels = []
    for mode in modes:
        mode_observations = [group[mode] for group in groups.values() if mode in group]
        passed = sum(observation.passed for observation in mode_observations)
        mode_labels.append(f"{_mode_label(mode)}\n{passed}/{len(mode_observations)} pass")
    axis.set_xticks(range(len(modes)), labels=mode_labels)
    axis.xaxis.tick_top()
    axis.tick_params(axis="both", length=0, pad=8)
    axis.set_xlim(-0.6, len(modes) - 0.4)
    axis.set_ylim(-0.8, len(test_ids) - 0.2)
    for x in range(len(modes)):
        axis.axvline(x, color=GRID_COLOR, linewidth=0.6, zorder=0)
    _remove_spines(axis)
    axis.set_title("AI Insights quality by testcase", loc="left", pad=16)
    figure.text(
        0.5,
        0.015,
        f"{MARKER_LEGEND}\nAttempts are ordered from left to right.",
        ha="center",
        color="#555555",
        fontsize=7,
        fontstyle="italic",
        linespacing=1.5,
    )
    figure.tight_layout(rect=(0, 0.06, 1, 0.96))
    return _save_figure(figure, output_dir / "quality-matrix")


def _plot_metric(
    groups: dict[tuple[str, int], dict[str, Observation]],
    modes: list[str],
    mode_colors: dict[str, tuple[float, float, float]],
    metric: Metric,
    output_dir: Path,
) -> list[Path]:
    if not any(
        _metric_value(observation, metric) is not None
        for group in groups.values()
        for observation in group.values()
    ):
        return []
    keys = sorted(groups, key=lambda key: (*_test_sort_key(key[0]), key[1]))
    comparison_modes = [mode for mode in modes if mode != PERFORMIX_MODE]
    show_ratios = metric.show_relative and PERFORMIX_MODE in modes and bool(comparison_modes)
    extra_label_lines = max(0, _mode_label_line_count(modes) - 2)
    if show_ratios:
        panel_spacing = 0.24 + 0.04 * extra_label_lines
        figure, axes = plt.subplots(
            2,
            1,
            figsize=(
                max(8.2, 1.45 * len(modes)),
                8.75 + 0.18 * extra_label_lines,
            ),
            gridspec_kw={"height_ratios": (2.0, 1.875), "hspace": panel_spacing},
        )
        absolute_axis, ratio_axis = axes
    else:
        figure, absolute_axis = plt.subplots(
            figsize=(
                max(8.2, 1.45 * len(modes)),
                4.8 + 0.18 * extra_label_lines,
            )
        )
        ratio_axis = None
    test_offsets, attempt_offsets = _hierarchical_offsets(keys)
    for key in keys:
        available = []
        for position, mode in enumerate(modes):
            observation = groups[key].get(mode)
            if observation is None:
                continue
            value = _metric_value(observation, metric)
            if value is None:
                continue
            if metric.omit_zero and value == 0:
                continue
            available.append((position, observation, value))
        x_values = [
            position + attempt_offsets[key]
            for position, _observation, _value in available
        ]
        values = [value for _position, _observation, value in available]
        if len(available) > 1:
            absolute_axis.plot(x_values, values, color=CONNECTION_COLOR, linewidth=0.65, zorder=1)
        for x, (_position, observation, value) in zip(x_values, available):
            _plot_quality_marker(
                absolute_axis,
                x,
                value,
                observation,
                color=mode_colors[observation.mode],
                size=30,
            )
    _plot_test_ranges(
        absolute_axis,
        groups,
        modes,
        test_offsets,
        value=lambda observation: _required_metric_value(observation, metric),
        include=lambda value: not metric.omit_zero or value != 0,
        include_observation=lambda observation: _metric_value(observation, metric) is not None,
    )
    for position, mode in enumerate(modes):
        centre = _median_of_test_medians(
            groups,
            mode,
            value=lambda observation: _required_metric_value(observation, metric),
            include=lambda value: not metric.omit_zero or value != 0,
            include_observation=lambda observation: _metric_value(observation, metric) is not None,
        )
        if centre is None:
            continue
        absolute_axis.plot(
            [position - 0.12, position + 0.12],
            [centre, centre],
            color=PASS_COLOR,
            linewidth=2.0,
            solid_capstyle="butt",
            zorder=4,
        )
    absolute_axis.set_xticks(range(len(modes)), labels=[_mode_label(mode) for mode in modes])
    absolute_axis.set_ylabel(metric.ylabel)
    absolute_axis.yaxis.set_major_formatter(FuncFormatter(metric.formatter))
    absolute_axis.grid(axis="y", color=GRID_COLOR, linewidth=0.55)
    absolute_axis.axhline(
        0.0,
        color=REFERENCE_LINE_COLOR,
        linewidth=0.9,
        zorder=2,
    )
    _label_testcase_extents(absolute_axis, modes, test_offsets)
    absolute_axis.tick_params(axis="both", length=0, pad=6)
    _remove_spines(absolute_axis)
    absolute_axis.set_title(metric.title, loc="left", pad=12)

    if ratio_axis is not None:
        ratio_values: dict[tuple[str, int], dict[str, float]] = {}
        for key in keys:
            baseline_observation = groups[key].get(PERFORMIX_MODE)
            if baseline_observation is None:
                continue
            baseline = _metric_value(baseline_observation, metric)
            if baseline is None or baseline <= 0:
                continue
            available = []
            for position, mode in enumerate(comparison_modes):
                observation = groups[key].get(mode)
                if observation is None:
                    continue
                observation_value = _metric_value(observation, metric)
                if observation_value is None:
                    continue
                value = observation_value / baseline
                if value > 0:
                    available.append((position, observation, value))
                    ratio_values.setdefault(key, {})[mode] = value
            x_values = [
                position + attempt_offsets[key]
                for position, _observation, _value in available
            ]
            values = [value for _position, _observation, value in available]
            if len(available) > 1:
                ratio_axis.plot(x_values, values, color=CONNECTION_COLOR, linewidth=0.65, zorder=1)
            for x, (_position, observation, value) in zip(x_values, available):
                _plot_quality_marker(
                    ratio_axis,
                    x,
                    value,
                    observation,
                    color=mode_colors[observation.mode],
                    size=28,
                )
        _plot_value_ranges(ratio_axis, ratio_values, comparison_modes, test_offsets)
        ratio_axis.axhline(
            1.0,
            color=REFERENCE_LINE_COLOR,
            linewidth=0.9,
            zorder=2,
        )
        _label_testcase_extents(ratio_axis, comparison_modes, test_offsets)
        ratio_axis.set_xticks(
            range(len(comparison_modes)), labels=[_mode_label(mode) for mode in comparison_modes]
        )
        ratio_axis.set_ylabel("Relative to\nPerformix MCP")
        ratio_axis.tick_params(axis="both", which="both", length=0, pad=6)
        _configure_ratio_axis(ratio_axis)
        ratio_axis.grid(
            axis="y",
            which="major",
            color=LOG_MAJOR_GRID_COLOR,
            linewidth=0.65,
        )
        ratio_axis.grid(
            axis="y",
            which="minor",
            color=LOG_MINOR_GRID_COLOR,
            linewidth=0.4,
        )
        _remove_spines(ratio_axis)
    figure.text(
        0.5,
        0.01,
        f"{MARKER_LEGEND}\n"
        "Grey lines join matching testcases and attempts; vertical lines show each testcase's "
        "attempt range.\n"
        "Short bars in the absolute panel show the median of testcase medians; small labels "
        "mark the first and last testcase in each cluster.",
        ha="center",
        color="#555555",
        fontsize=7,
        fontstyle="italic",
        linespacing=1.5,
    )
    if ratio_axis is None:
        figure.subplots_adjust(
            bottom=0.18 + 0.02 * extra_label_lines,
            left=0.11,
            right=0.98,
            top=0.93,
        )
    else:
        figure.subplots_adjust(
            bottom=0.12 + 0.02 * extra_label_lines,
            left=0.11,
            right=0.98,
            top=0.93,
            hspace=panel_spacing,
        )
    return _save_figure(figure, output_dir / metric.filename)


def _plot_cost_quality(
    groups: dict[tuple[str, int], dict[str, Observation]],
    modes: list[str],
    mode_colors: dict[str, tuple[float, float, float]],
    output_dir: Path,
) -> list[Path]:
    if PERFORMIX_MODE not in modes:
        return []
    comparison_modes = [
        mode
        for mode in modes
        if mode != PERFORMIX_MODE
        and any(PERFORMIX_MODE in group and mode in group for group in groups.values())
    ]
    if not comparison_modes:
        return []
    keys = sorted(groups, key=lambda key: (*_test_sort_key(key[0]), key[1]))
    figure, axes_grid = plt.subplots(
        1,
        len(comparison_modes),
        figsize=(max(5.0, 4.25 * len(comparison_modes)), 7.25),
        sharex=True,
        sharey=True,
        squeeze=False,
    )
    axes = axes_grid[0]
    for axis, mode in zip(axes, comparison_modes):
        for key in keys:
            baseline = groups[key].get(PERFORMIX_MODE)
            observation = groups[key].get(mode)
            if baseline is None or observation is None:
                continue
            if baseline.duration_seconds <= 0 or baseline.input_tokens <= 0:
                continue
            runtime_ratio = observation.duration_seconds / baseline.duration_seconds
            token_ratio = observation.input_tokens / baseline.input_tokens
            if runtime_ratio <= 0 or token_ratio <= 0:
                continue
            _plot_quality_marker(
                axis,
                runtime_ratio,
                token_ratio,
                observation,
                color=mode_colors[observation.mode],
                size=35,
            )
        axis.axvline(
            1.0,
            color=REFERENCE_LINE_COLOR,
            linewidth=0.9,
            zorder=2,
        )
        axis.axhline(
            1.0,
            color=REFERENCE_LINE_COLOR,
            linewidth=0.9,
            zorder=2,
        )
        axis.set_xscale("log", base=10)
        axis.set_yscale("log", base=10)
        axis.grid(which="major", color=LOG_MAJOR_GRID_COLOR, linewidth=0.65)
        axis.grid(which="minor", color=LOG_MINOR_GRID_COLOR, linewidth=0.4)
        axis.tick_params(axis="both", which="both", length=0, pad=6)
        _remove_spines(axis)
        axis.set_title(_mode_label(mode).replace("\n", " "), loc="left")
        axis.set_xlabel("Runtime relative to Performix MCP")
    axes[0].set_ylabel("Input tokens relative to Performix MCP")
    figure.suptitle("Cost and quality relative to the curated implementation", x=0.06, ha="left", fontsize=13)
    figure.text(
        0.5,
        0.015,
        f"{MARKER_LEGEND}\n"
        "Each point is one testcase and attempt; reference lines mark equal cost.",
        ha="center",
        color="#555555",
        fontsize=7,
        fontstyle="italic",
        linespacing=1.5,
    )
    figure.tight_layout(rect=(0, 0.1, 1, 0.92))
    return _save_figure(figure, output_dir / "cost-quality")


def _plot_runtime_repeatability(
    groups: dict[tuple[str, int], dict[str, Observation]],
    modes: list[str],
    mode_colors: dict[str, tuple[float, float, float]],
    output_dir: Path,
) -> list[Path]:
    if not _has_repeated_mode_test(groups):
        return []

    test_ids = sorted({test_id for test_id, _attempt in groups}, key=_test_sort_key)
    figure, axes = plt.subplots(
        len(modes),
        1,
        figsize=(max(9.0, 0.65 * len(test_ids)), max(4.0, 1.9 + 2.15 * len(modes))),
        sharex=True,
        sharey=True,
        squeeze=False,
        gridspec_kw={"hspace": 0.28},
    )
    for axis, mode in zip(axes[:, 0], modes):
        for position, test_id in enumerate(test_ids):
            observations = sorted(
                (
                    group[mode]
                    for (group_test_id, _attempt), group in groups.items()
                    if group_test_id == test_id and mode in group
                ),
                key=lambda observation: observation.attempt,
            )
            if not observations:
                continue
            runtime_median = median(
                observation.duration_seconds for observation in observations
            )
            if runtime_median <= 0:
                continue
            values = [
                observation.duration_seconds / runtime_median
                for observation in observations
            ]
            if min(values) != max(values):
                axis.vlines(
                    position,
                    min(values),
                    max(values),
                    color=REFERENCE_LINE_COLOR,
                    linewidth=0.8,
                    alpha=0.7,
                    zorder=2,
                )
            for value in values:
                _plot_mode_marker(
                    axis,
                    position,
                    value,
                    color=mode_colors[mode],
                    size=28,
                )
        axis.axhline(1.0, color=REFERENCE_LINE_COLOR, linewidth=0.9, zorder=1)
        axis.grid(axis="y", color=GRID_COLOR, linewidth=0.55)
        axis.yaxis.set_major_formatter(FuncFormatter(lambda value, _position: f"{value:g}×"))
        axis.tick_params(axis="both", length=0, pad=6)
        axis.set_ylabel("Runtime /\ntest median")
        axis.set_title(_mode_label(mode).replace("\n", " "), loc="left", pad=7)
        _remove_spines(axis)
    bottom_axis = axes[-1, 0]
    bottom_axis.set_xticks(range(len(test_ids)), labels=test_ids, rotation=45, ha="right")
    figure.suptitle("Runtime repeatability", x=0.11, ha="left", fontsize=13)
    figure.text(
        0.5,
        0.01,
        "Each circle is one attempt, divided by the median for its testcase and mode.\n"
        "Vertical lines show the minimum-to-maximum range. One attempt is always 1× and "
        "does not measure repeatability.",
        ha="center",
        color="#555555",
        fontsize=7,
        fontstyle="italic",
        linespacing=1.5,
    )
    figure.subplots_adjust(bottom=0.26, left=0.11, right=0.98, top=0.85)
    return _save_figure(figure, output_dir / "runtime-repeatability")


def _plot_mcp_outcomes(
    groups: dict[tuple[str, int], dict[str, Observation]],
    modes: list[str],
    mode_colors: dict[str, tuple[float, float, float]],
    output_dir: Path,
    *,
    filename: str,
    title: str,
    panels: tuple[tuple[str, str], tuple[str, str]],
    ylabel: str,
    formatter: Callable[[float, int], str],
) -> list[Path]:
    mcp_modes = [
        mode
        for mode in modes
        if any(group.get(mode) and group[mode].mcp_calls > 0 for group in groups.values())
    ]
    if not mcp_modes:
        return []

    keys = sorted(groups, key=lambda key: (*_test_sort_key(key[0]), key[1]))
    test_offsets, attempt_offsets = _hierarchical_offsets(keys)
    extra_label_lines = max(0, _mode_label_line_count(mcp_modes) - 2)
    figure, axes = plt.subplots(
        2,
        1,
        figsize=(
            max(8.2, 1.45 * len(mcp_modes)),
            7.4 + 0.18 * extra_label_lines,
        ),
        sharex=True,
        sharey=True,
        gridspec_kw={"hspace": 0.2},
    )
    for axis, (field, panel_title) in zip(axes, panels):
        for key in keys:
            available = [
                (position, groups[key][mode])
                for position, mode in enumerate(mcp_modes)
                if mode in groups[key] and groups[key][mode].mcp_calls > 0
            ]
            x_values = [position + attempt_offsets[key] for position, _observation in available]
            values = [float(getattr(observation, field)) for _position, observation in available]
            if len(available) > 1:
                axis.plot(x_values, values, color=CONNECTION_COLOR, linewidth=0.65, zorder=1)
            for x, (_position, observation), value in zip(x_values, available, values):
                _plot_mode_marker(
                    axis,
                    x,
                    value,
                    color=mode_colors[observation.mode],
                    size=30,
                )
        _plot_test_ranges(
            axis,
            groups,
            mcp_modes,
            test_offsets,
            value=lambda observation: float(getattr(observation, field)),
            include=lambda _value: True,
            include_observation=lambda observation: observation.mcp_calls > 0,
        )
        for position, mode in enumerate(mcp_modes):
            centre = _median_of_test_medians(
                groups,
                mode,
                value=lambda observation: float(getattr(observation, field)),
                include=lambda _value: True,
                include_observation=lambda observation: observation.mcp_calls > 0,
            )
            if centre is not None:
                axis.plot(
                    [position - 0.12, position + 0.12],
                    [centre, centre],
                    color=PASS_COLOR,
                    linewidth=2.0,
                    solid_capstyle="butt",
                    zorder=4,
                )
        axis.set_ylabel(ylabel)
        axis.yaxis.set_major_formatter(FuncFormatter(formatter))
        axis.grid(axis="y", color=GRID_COLOR, linewidth=0.55)
        axis.axhline(0.0, color=REFERENCE_LINE_COLOR, linewidth=0.9, zorder=2)
        _label_testcase_extents(axis, mcp_modes, test_offsets)
        axis.tick_params(axis="both", length=0, pad=6)
        axis.set_title(panel_title, loc="left", pad=10)
        _remove_spines(axis)

    axes[1].set_xticks(
        range(len(mcp_modes)), labels=[_mode_label(mode) for mode in mcp_modes]
    )
    figure.suptitle(title, x=0.11, ha="left", fontsize=13)
    figure.text(
        0.5,
        0.01,
        "Each circle is one testcase and attempt. Grey lines join matching "
        "testcases and attempts;\nvertical lines show each testcase's attempt "
        "range. Short bars show the median of testcase medians. Both panels "
        "use the same linear scale.",
        ha="center",
        color="#555555",
        fontsize=7,
        fontstyle="italic",
        linespacing=1.5,
    )
    figure.subplots_adjust(
        bottom=0.14 + 0.02 * extra_label_lines,
        left=0.11,
        right=0.98,
        top=0.91,
    )
    return _save_figure(figure, output_dir / filename)


def _plot_quality_marker(
    axis: Axes,
    x: float,
    y: float,
    observation: Observation,
    *,
    color: tuple[float, float, float],
    size: float,
) -> None:
    if not observation.passed:
        axis.scatter(x, y, marker="x", s=size, color=FAIL_COLOR, linewidths=1.3, zorder=5)
        return
    filled = observation.confidence == "high"
    axis.scatter(
        x,
        y,
        marker="o",
        s=size,
        facecolors=color if filled else "white",
        edgecolors=color,
        linewidths=0.9,
        zorder=4,
    )


def _plot_mode_marker(
    axis: Axes,
    x: float,
    y: float,
    *,
    color: tuple[float, float, float],
    size: float,
) -> None:
    axis.scatter(
        x,
        y,
        marker="o",
        s=size,
        facecolors=color,
        edgecolors=color,
        linewidths=0.9,
        zorder=4,
    )


def _mode_colors(modes: list[str]) -> dict[str, tuple[float, float, float]]:
    families = list(dict.fromkeys(_mode_parts(mode)[0] for mode in modes))
    family_colors = {
        family: MODE_PALETTE[index % len(MODE_PALETTE)]
        for index, family in enumerate(families)
    }
    colors = {}
    for family in families:
        family_modes = [mode for mode in modes if _mode_parts(mode)[0] == family]
        variants = [mode for mode in family_modes if _mode_parts(mode)[1]]
        unversioned = [mode for mode in family_modes if not _mode_parts(mode)[1]]
        for mode in unversioned:
            colors[mode] = family_colors[family]
        for index, mode in enumerate(variants):
            denominator = max(1, len(variants) - 1)
            white_fraction = 0.55 * (len(variants) - 1 - index) / denominator
            if unversioned:
                white_fraction = 0.2 + 0.45 * (len(variants) - 1 - index) / denominator
            colors[mode] = _blend_with_white(family_colors[family], white_fraction)
    return {mode: colors[mode] for mode in modes}


def _blend_with_white(
    color: tuple[float, float, float],
    fraction: float,
) -> tuple[float, float, float]:
    return tuple(channel + (1.0 - channel) * fraction for channel in color)


def _metric_value(observation: Observation, metric: Metric) -> float | None:
    value = getattr(observation, metric.field)
    return None if value is None else float(value)


def _required_metric_value(observation: Observation, metric: Metric) -> float:
    value = _metric_value(observation, metric)
    if value is None:
        raise ValueError(f"missing metric {metric.field}")
    return value


def _point_offsets(count: int, *, width: float = 0.10) -> list[float]:
    if count <= 1:
        return [0.0]
    return [(-width / 2) + (width * index / (count - 1)) for index in range(count)]


def _hierarchical_offsets(
    keys: list[tuple[str, int]],
) -> tuple[dict[str, float], dict[tuple[str, int], float]]:
    """Align attempts while separating testcases within each mode column."""
    test_ids = sorted({test_id for test_id, _attempt in keys}, key=_test_sort_key)
    test_offset_values = _point_offsets(len(test_ids), width=0.30)
    test_offsets = dict(zip(test_ids, test_offset_values))
    attempt_offsets = {
        key: test_offsets[key[0]]
        for key in keys
    }
    return test_offsets, attempt_offsets


def _label_testcase_extents(
    axis: Axes,
    modes: list[str],
    test_offsets: dict[str, float],
) -> None:
    """Label the testcase order at the edges of each mode's point cluster."""
    test_ids = sorted(test_offsets, key=_test_sort_key)
    extent_ids = test_ids if len(test_ids) == 1 else (test_ids[0], test_ids[-1])
    for position, _mode in enumerate(modes):
        for test_id in extent_ids:
            axis.text(
                position + test_offsets[test_id],
                0.015,
                test_id.removeprefix("test_case_"),
                transform=axis.get_xaxis_transform(),
                ha="center",
                va="bottom",
                color="#888888",
                fontsize=7,
                zorder=3,
            )


def _plot_test_ranges(
    axis: Axes,
    groups: dict[tuple[str, int], dict[str, Observation]],
    modes: list[str],
    test_offsets: dict[str, float],
    *,
    value: Callable[[Observation], float],
    include: Callable[[float], bool],
    include_observation: Callable[[Observation], bool] = lambda _observation: True,
) -> None:
    values = {}
    for key, group in groups.items():
        for mode in modes:
            observation = group.get(mode)
            if observation is None or not include_observation(observation):
                continue
            observation_value = value(observation)
            if include(observation_value):
                values.setdefault(key, {})[mode] = observation_value
    _plot_value_ranges(axis, values, modes, test_offsets)


def _plot_value_ranges(
    axis: Axes,
    values: dict[tuple[str, int], dict[str, float]],
    modes: list[str],
    test_offsets: dict[str, float],
) -> None:
    for position, mode in enumerate(modes):
        for test_id, test_offset in test_offsets.items():
            test_values = [
                mode_values[mode]
                for (value_test_id, _attempt), mode_values in values.items()
                if value_test_id == test_id and mode in mode_values
            ]
            if len(test_values) <= 1 or min(test_values) == max(test_values):
                continue
            axis.vlines(
                position + test_offset,
                min(test_values),
                max(test_values),
                color=REFERENCE_LINE_COLOR,
                linewidth=0.75,
                alpha=0.65,
                zorder=2,
            )


def _median_of_test_medians(
    groups: dict[tuple[str, int], dict[str, Observation]],
    mode: str,
    *,
    value: Callable[[Observation], float],
    include: Callable[[float], bool],
    include_observation: Callable[[Observation], bool] = lambda _observation: True,
) -> float | None:
    values_by_test: dict[str, list[float]] = {}
    for (test_id, _attempt), group in groups.items():
        observation = group.get(mode)
        if observation is None or not include_observation(observation):
            continue
        observation_value = value(observation)
        if include(observation_value):
            values_by_test.setdefault(test_id, []).append(observation_value)
    if not values_by_test:
        return None
    return median(median(values) for values in values_by_test.values())


def _configure_ratio_axis(axis: Axes) -> None:
    axis.set_yscale("log", base=10)


def _remove_spines(axis: Axes) -> None:
    for spine in axis.spines.values():
        spine.set_visible(False)


def _has_repeated_mode_test(
    groups: dict[tuple[str, int], dict[str, Observation]],
) -> bool:
    counts: dict[tuple[str, str], int] = {}
    for (test_id, _attempt), mode_observations in groups.items():
        for mode in mode_observations:
            key = (test_id, mode)
            counts[key] = counts.get(key, 0) + 1
    return any(count > 1 for count in counts.values())


def _save_figure(figure, base_path: Path) -> list[Path]:
    paths = []
    for suffix in (".svg", ".png"):
        path = base_path.with_suffix(suffix)
        figure.savefig(path, dpi=180, bbox_inches="tight")
        paths.append(path)
    plt.close(figure)
    return paths


def _write_observations_csv(observations: list[Observation], path: Path) -> None:
    with path.open("w", encoding="utf-8", newline="") as output:
        writer = csv.writer(output)
        writer.writerow(
            (
                "test_id",
                "mode",
                "attempt",
                "score",
                "confidence",
                "duration_seconds",
                "input_tokens",
                "output_tokens",
                "reasoning_tokens",
                "mcp_tool_rounds",
                "mcp_calls",
                "mcp_calls_succeeded",
                "mcp_calls_failed",
                "mcp_tool_duration_seconds",
                "mcp_tool_duration_seconds_succeeded",
                "mcp_tool_duration_seconds_failed",
                "mcp_tool_share_percent",
            )
        )
        for observation in observations:
            writer.writerow(
                (
                    observation.test_id,
                    observation.mode,
                    observation.attempt,
                    observation.score,
                    observation.confidence,
                    observation.duration_seconds,
                    observation.input_tokens,
                    observation.output_tokens,
                    observation.reasoning_tokens,
                    observation.mcp_tool_rounds,
                    observation.mcp_calls,
                    observation.mcp_calls_succeeded,
                    observation.mcp_calls_failed,
                    observation.mcp_tool_duration_seconds,
                    observation.mcp_tool_duration_seconds_succeeded,
                    observation.mcp_tool_duration_seconds_failed,
                    observation.mcp_tool_share_percent,
                )
            )


def _write_html_report(
    observations: list[Observation],
    modes: list[str],
    figure_names: set[str],
    path: Path,
) -> None:
    mode_rows = []
    for mode in modes:
        mode_observations = [
            observation for observation in observations if observation.mode == mode
        ]
        passed = sum(observation.passed for observation in mode_observations)
        pass_rate = passed / len(mode_observations)
        observations_by_test: dict[str, list[Observation]] = {}
        for observation in mode_observations:
            observations_by_test.setdefault(observation.test_id, []).append(observation)
        attempt_counts = [
            len(test_observations)
            for test_observations in observations_by_test.values()
        ]
        attempts_per_test = (
            str(attempt_counts[0])
            if min(attempt_counts) == max(attempt_counts)
            else f"{min(attempt_counts)}–{max(attempt_counts)}"
        )
        total_runtime = sum(
            observation.duration_seconds for observation in mode_observations
        )
        median_test_runtime = median(
            median(observation.duration_seconds for observation in test_observations)
            for test_observations in observations_by_test.values()
        )
        runtime_ranges = []
        for test_observations in observations_by_test.values():
            runtimes = [observation.duration_seconds for observation in test_observations]
            runtime_median = median(runtimes)
            if len(runtimes) > 1 and runtime_median > 0:
                runtime_ranges.append(
                    100.0 * (max(runtimes) - min(runtimes)) / runtime_median
                )
        median_runtime_range = (
            f"{median(runtime_ranges):.1f}%" if runtime_ranges else "–"
        )
        worst_runtime_range = f"{max(runtime_ranges):.1f}%" if runtime_ranges else "–"
        label = _mode_label(mode).replace("\n", " ")
        mode_rows.append(
            "<tr>"
            f"<th scope=\"row\">{html_escape(label)}</th>"
            f"<td>{len(observations_by_test)}</td>"
            f"<td>{attempts_per_test}</td>"
            f"<td>{len(mode_observations)}</td>"
            f"<td>{passed}</td>"
            f"<td>{pass_rate:.1%}</td>"
            f"<td>{total_runtime:.1f} s</td>"
            f"<td>{median_test_runtime:.1f} s</td>"
            f"<td>{median_runtime_range}</td>"
            f"<td>{worst_runtime_range}</td>"
            "</tr>"
        )

    figures = []
    for name in FIGURE_ORDER:
        if name not in figure_names:
            if name == "runtime-repeatability":
                title, _description = FIGURE_DESCRIPTIONS[name]
                figures.append(
                    f"<section id=\"{name}\">\n"
                    f"<h2>{html_escape(title)}</h2>\n"
                    "<p>Omitted because the report contains only one attempt per "
                    "testcase and mode.</p>\n"
                    "</section>"
                )
            continue
        title, description = FIGURE_DESCRIPTIONS[name]
        figures.append(
            f"<section id=\"{name}\">\n"
            f"<h2>{html_escape(title)}</h2>\n"
            f"<p>{html_escape(description)}</p>\n"
            "<figure>\n"
            f"<img src=\"{name}.svg\" alt=\"{html_escape(title)} graph\" loading=\"lazy\">\n"
            f"<figcaption><a href=\"{name}.svg\">SVG</a> · "
            f"<a href=\"{name}.png\">PNG</a></figcaption>\n"
            "</figure>\n"
            "</section>"
        )

    content = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AI Insights mode comparison</title>
<style>
:root {{ color-scheme: light; font-family: system-ui, sans-serif; color: #252525; }}
body {{ margin: 0; background: #fff; }}
main {{ max-width: 1120px; margin: 0 auto; padding: 2rem 1.25rem 4rem; }}
h1 {{ font-size: 1.8rem; margin: 0 0 .5rem; }}
h2 {{ font-size: 1.25rem; margin: 2.75rem 0 .4rem; }}
p {{ max-width: 78ch; line-height: 1.55; }}
table {{ border-collapse: collapse; margin: 1.25rem 0 2rem; min-width: min(100%, 34rem); }}
th, td {{ border-bottom: 1px solid #ddd; padding: .55rem .8rem; text-align: right; }}
th:first-child {{ text-align: left; }}
thead th {{ color: #555; font-size: .85rem; }}
figure {{ margin: 1rem 0; }}
img {{ display: block; width: 100%; height: auto; }}
figcaption {{ margin-top: .35rem; color: #666; font-size: .85rem; }}
a {{ color: #286486; }}
.key {{ border-left: 3px solid #ddd; padding-left: 1rem; }}
.table-scroll {{ overflow-x: auto; }}
</style>
</head>
<body>
<main>
<h1>AI Insights mode comparison</h1>
<p>This report contains {len(observations)} observations from the supplied JUnit report. Each
observation represents one mode, testcase and attempt. Pass rates use only the observations
present in the report. Observation runtime is the total evaluation cost and therefore increases
with the number of attempts. Median test runtime is the median of each testcase's median runtime.
Runtime ranges are the minimum-to-maximum span for each testcase, expressed as a percentage of
its median.</p>
<div class="table-scroll">
<table>
<thead><tr>
<th scope="col">Mode</th>
<th scope="col">Testcases</th>
<th scope="col">Attempts / testcase</th>
<th scope="col">Observations</th>
<th scope="col">Passes</th>
<th scope="col">Pass rate</th>
<th scope="col">Observation runtime</th>
<th scope="col">Median test runtime</th>
<th scope="col">Median runtime range</th>
<th scope="col">Worst runtime range</th>
</tr></thead>
<tbody>{''.join(mode_rows)}</tbody>
</table>
</div>
<div class="key">
<h2>Reading the figures</h2>
<p>A filled circle is a high-confidence pass, an open circle is a non-high-confidence pass, and
a cross is a failure. Grey lines join matching testcases and attempts. Short horizontal bars in
absolute metric panels show the median of testcase medians. Thin vertical lines show the
minimum-to-maximum attempt range for a testcase.
Relative panels use the matching Performix MCP observation as the 1× baseline.</p>
<p>Relative axes use a base-10 logarithmic scale. Zero ratios are omitted because logarithmic
axes cannot represent zero. Modes with no MCP calls are omitted from MCP-only figures.</p>
</div>
{''.join(figures)}
<section id="data">
<h2>Source data</h2>
<p>The values plotted above are available as <a href="observations.csv">observations.csv</a>.
Missing mode/testcase combinations are shown as dashes in the quality matrix and omitted from
comparisons that require both observations.</p>
</section>
</main>
</body>
</html>
"""
    path.write_text(content, encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("junit_xml", type=Path, help="AI Insights JUnit XML report")
    parser.add_argument(
        "--output-dir",
        required=True,
        type=Path,
        help="Directory for HTML, SVG, PNG and CSV output",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        observations = load_observations(args.junit_xml)
        outputs = generate_graphs(observations, args.output_dir)
    except (OSError, ValueError) as exc:
        raise SystemExit(f"Failed to plot AI Insights JUnit results: {exc}") from exc
    print(f"Generated {len(outputs)} files in {args.output_dir}")
    for output in outputs:
        print(f"  {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
