# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Render AI Insights evaluation attempt records as summary tables.

The input is a list of dictionaries containing the `ai_*` properties recorded
by the pytest harness, plus a `pytest_outcome` value for each attempt.

This is a common module used both for:
 a) The rich text console summary printed at the end of pytest suite, and
 b) (Optionally if run via GH workflow) A HTML formatted table included in the
    GH workflow results; via render-ai-insights-junit-summary.py

Use `render_console_summary()` for terminal output and
 `render_markdown_summary()` for the GitHub Actions summary.

Example:
    attempts = [{"ai_test_id": "test_case_03", "ai_mode": "rest", "pytest_outcome": "passed"}]
    print(render_console_summary(attempts))
"""

from __future__ import annotations

from dataclasses import dataclass
from html import escape as html_escape
from io import StringIO

from rich import box
from rich.console import Console
from rich.table import Table
from rich.text import Text

from performance_quality import (
    QUALITY_GOOD,
    QUALITY_INDETERMINABLE,
    MetricQuality,
    recorded_performance_failures_for_attempt,
    recorded_performance_metrics_for_attempt,
)


MODE_ORDER = ("rest", "hackathon_mcp", "performix_mcp")
MODE_LABELS = {
    "rest": "REST",
    "hackathon_mcp": "Hackathon MCP",
    "performix_mcp": "Performix MCP",
}
TOKEN_KEYS = ("ai_input_tokens", "ai_output_tokens", "ai_reasoning_output_tokens")


@dataclass(frozen=True)
class Column:
    key: str
    title: str


@dataclass(frozen=True)
class Cell:
    text: str
    status: str | None = None
    confidence: str | None = None
    lines: tuple[str, ...] = ()


@dataclass(frozen=True)
class SummaryTable:
    columns: list[Column]
    rows: list[dict[str, Cell]]


@dataclass(frozen=True)
class SummaryRows:
    modes: list[str]
    rows: list[dict[str, Cell]]


def render_console_summary(
    attempts: list[dict[str, str]],
    *,
    width: int | None = None,
    color: bool = False,
) -> str:
    """Render an AI Insights summary table for the terminal."""
    output = StringIO()
    console = Console(
        file=output,
        force_terminal=color,
        color_system="truecolor" if color else None,
        no_color=not color,
        width=width,
    )
    console.print(
        _rich_flat_table(_build_summary_table(attempts))
    )
    return output.getvalue().strip("\n")


def render_markdown_summary(
    attempts: list[dict[str, str]],
) -> str:
    """Render an AI Insights summary as GitHub-flavoured Markdown."""
    summary = _build_summary_data(attempts)
    return _html_summary_table(summary.modes, summary.rows) + "\n"


def _build_summary_table(
    attempts: list[dict[str, str]],
) -> SummaryTable:
    summary = _build_summary_data(attempts)
    columns = [Column("testcase", "Testcase")]
    for mode in summary.modes:
        columns.extend(_mode_columns(mode))
    return SummaryTable(columns=columns, rows=summary.rows)


def _build_summary_data(
    attempts: list[dict[str, str]],
) -> SummaryRows:
    rows = _summary_rows(attempts)
    modes = _summary_modes(rows)
    return SummaryRows(
        modes=modes,
        rows=[
            {
                "testcase": Cell(test_id),
                **_mode_row_cells(modes, mode_results),
            }
            for test_id, mode_results in rows
        ],
    )


def _rich_flat_table(summary: SummaryTable) -> Table:
    table = Table(box=box.SIMPLE_HEAVY, padding=(0, 1, 0, 0))
    for column in summary.columns:
        table.add_column(column.title)
    for row in summary.rows:
        table.add_row(*[
            _rich_cell(row.get(column.key, Cell("-")))
            for column in summary.columns
        ])
    return table


def _rich_cell(cell: Cell) -> Text:
    text = Text()
    if cell.lines:
        text.append("\n".join(cell.lines))
        return text
    if cell.status:
        text.append(cell.text, style=_rich_status_style(cell.status))
    else:
        text.append(cell.text)
    if cell.confidence:
        text.append(" (")
        text.append(cell.confidence, style=_rich_confidence_style(cell.confidence, cell.status))
        text.append(")")
    return text


def _rich_status_style(status: str) -> str:
    if status == "pass":
        return "bold green"
    if status in {"fail", "error"}:
        return "bold red"
    if status == "xfail":
        return "bold yellow"
    if status == "skipped":
        return "cyan"
    if status == "xpass":
        return "magenta"
    return "yellow"


def _rich_confidence_style(confidence: str, status: str | None) -> str:
    if status in {"fail", "error"}:
        return "red"
    labels = {label.strip().lower() for label in confidence.split(",") if label.strip()}
    if labels == {"high"}:
        return "green"
    if labels.intersection({"low", "unknown", "error"}):
        return "red"
    if labels:
        return "yellow"
    return "default"


def _html_summary_table(modes: list[str], rows: list[dict[str, Cell]]) -> str:
    lines = [
        "<table>",
        "  <thead>",
        "    <tr>",
        "      <th rowspan=\"2\">Testcase</th>",
    ]
    for mode in modes:
        label = MODE_LABELS.get(mode, mode)
        colspan = 3 if _is_mcp_mode_name(mode) else 2
        lines.append(f"      <th colspan=\"{colspan}\">{html_escape(label)}</th>")
    lines.extend([
        "    </tr>",
        "    <tr>",
    ])
    for mode in modes:
        lines.extend([
            "      <th>Result</th>",
            "      <th>Runtime</th>",
        ])
        if _is_mcp_mode_name(mode):
            lines.append("      <th>Details</th>")
    lines.extend([
        "    </tr>",
        "  </thead>",
        "  <tbody>",
    ])
    for row in rows:
        lines.extend(_html_summary_row(modes, row))
    lines.extend([
        "  </tbody>",
        "</table>",
    ])
    return "\n".join(lines)


def _html_summary_row(modes: list[str], row: dict[str, Cell]) -> list[str]:
    lines = [
        "    <tr>",
        f"      <td>{_html_text(row.get('testcase', Cell('-')).text)}</td>",
    ]
    for mode in modes:
        lines.append(f"      <td>{_html_cell(row.get(f'{mode}:result', Cell('-')))}</td>")
        lines.append(f"      <td>{_html_cell(row.get(f'{mode}:runtime', Cell('-')))}</td>")
        if _is_mcp_mode_name(mode):
            lines.append(f"      <td>{_html_cell(row.get(f'{mode}:details', Cell('-')))}</td>")
    lines.append("    </tr>")
    return lines


def _html_cell(cell: Cell) -> str:
    if cell.status:
        return _html_status_cell(cell)
    if cell.lines:
        return "<br>".join(_html_text(line) for line in cell.lines)
    return _html_text(_cell_display_text(cell))


def _html_status_cell(cell: Cell) -> str:
    icon = _status_icon(cell.status)
    result = f"<strong>{_html_text(cell.text)}</strong>"
    if cell.confidence:
        result = f"{result} ({_html_text(cell.confidence)})"
    return f"{icon} {result}"


def _status_icon(status: str) -> str:
    if status == "pass":
        return "✅"
    if status == "xfail":
        return "🟡"
    if status == "fail":
        return "❌"
    if status == "error":
        return "🔴"
    if status == "skipped":
        return "⚠️"
    if status == "xpass":
        return "❓"
    return ""


def _html_text(text: str) -> str:
    return html_escape(text, quote=False)


def _cell_display_text(cell: Cell) -> str:
    if cell.confidence:
        return f"{cell.text} ({cell.confidence})"
    return cell.text


def _mode_columns(mode: str) -> list[Column]:
    label = MODE_LABELS.get(mode, mode)
    console_label = _console_mode_label(mode, label)
    columns = [
        Column(
            f"{mode}:result",
            f"{console_label}\nResult",
        ),
        Column(
            f"{mode}:runtime",
            "Time",
        ),
    ]
    if _is_mcp_mode_name(mode):
        columns.append(
            Column(
                f"{mode}:details",
                f"{console_label}\nDetails",
            )
        )
    return columns


def _console_mode_label(mode: str, label: str) -> str:
    if mode == "hackathon_mcp":
        return "Hackathon"
    if mode == "performix_mcp":
        return "Performix"
    return label


def _mode_row_cells(
    modes: list[str],
    mode_results: dict[str, list[dict[str, str]]],
) -> dict[str, Cell]:
    cells = {}
    for mode in modes:
        cells.update(
            _format_mode_cells(
                mode,
                mode_results.get(mode, []),
            )
        )
    return cells


def _format_mode_cells(
    mode: str,
    attempts: list[dict[str, str]],
) -> dict[str, Cell]:
    if not attempts:
        return _empty_mode_cells(mode)
    attempts = sorted(attempts, key=_attempt_sort_key)
    cells = {
        f"{mode}:result": _format_result(attempts),
        f"{mode}:runtime": Cell(_format_total_duration(attempts)),
    }
    if _is_mcp_mode_name(mode):
        detail_lines = _format_mcp_detail_lines(
            attempts,
        ) or ["-"]
        cells[f"{mode}:details"] = Cell("", lines=tuple(detail_lines))
    return cells


def _empty_mode_cells(mode: str) -> dict[str, Cell]:
    cells = {
        f"{mode}:result": Cell("-"),
        f"{mode}:runtime": Cell("-"),
    }
    if _is_mcp_mode_name(mode):
        cells[f"{mode}:details"] = Cell("-")
    return cells


def _summary_rows(attempts: list[dict[str, str]]) -> list[tuple[str, dict[str, list[dict[str, str]]]]]:
    rows: dict[str, dict[str, list[dict[str, str]]]] = {}
    for attempt in attempts:
        test_id = attempt.get("ai_test_id")
        mode = attempt.get("ai_mode")
        if not test_id or not mode:
            continue
        rows.setdefault(test_id, {}).setdefault(mode, []).append(attempt)
    return [(test_id, rows[test_id]) for test_id in sorted(rows, key=_testcase_sort_key)]


def _testcase_sort_key(test_id: str) -> tuple[int, str]:
    prefix = "test_case_"
    if test_id.startswith(prefix) and test_id[len(prefix) :].isdigit():
        return (int(test_id[len(prefix) :]), test_id)
    return (10**9, test_id)


def _summary_modes(rows: list[tuple[str, dict[str, list[dict[str, str]]]]]) -> list[str]:
    modes = {
        mode
        for _, mode_results in rows
        for mode in mode_results
    }
    return [mode for mode in MODE_ORDER if mode in modes] + sorted(modes.difference(MODE_ORDER))


def _format_result(
    attempts: list[dict[str, str]],
) -> Cell:
    results = [
        _attempt_result(attempt)
        for attempt in attempts
    ]
    if len(results) == 1:
        status, status_text = results[0]
    else:
        status, status_text = _multiple_attempt_result(attempts, results)
    confidence = _format_unique_property(attempts, "ai_judge_confidences")
    if confidence:
        return Cell(status_text, status=status, confidence=confidence)
    return Cell(status_text, status=status)


def _attempt_result(
    attempt: dict[str, str],
) -> tuple[str, str]:
    if recorded_performance_failures_for_attempt(attempt):
        return "fail", "FAIL"
    return _single_attempt_status(attempt), _single_attempt_status_text(attempt)


def _multiple_attempt_result(
    attempts: list[dict[str, str]],
    results: list[tuple[str, str]],
) -> tuple[str, str]:
    statuses = [status for status, _ in results]
    if len(set(statuses)) == 1:
        return statuses[0], statuses[0].upper()

    labelled_results = []
    for index, (attempt, result) in enumerate(zip(attempts, results), start=1):
        _, result_text = result
        labelled_results.append(f"attempt {_attempt_label(attempt, index)} {result_text}")
    return "fail", ", ".join(labelled_results)


def _format_mcp_detail_lines(
    attempts: list[dict[str, str]],
) -> list[str]:
    details_parts = [
        _format_token_usage(attempts),
        _format_mcp_calls(attempts),
    ]
    details = " ".join(part for part in details_parts if part and part != "-")
    truncation = _format_tool_output_truncation(attempts)
    lines = [details] if details else []
    lines.extend(_format_performance_quality_lines(attempts))
    if truncation:
        lines.append(truncation)
    return lines


def _attempt_sort_key(attempt: dict[str, str]) -> int:
    value = attempt.get("ai_attempt")
    if value and value.isdigit():
        return int(value)
    return 0


def _attempt_label(attempt: dict[str, str], fallback: int) -> str:
    value = attempt.get("ai_attempt")
    if value and value.isdigit():
        return value
    return str(fallback)


def _single_attempt_status_text(attempt: dict[str, str]) -> str:
    display_score = _single_display_score(attempt)
    if display_score:
        return display_score.upper()
    return _single_attempt_status(attempt).upper()


def _single_attempt_status(attempt: dict[str, str]) -> str:
    display_score = _single_display_score(attempt)
    if display_score:
        return _status_from_display_score(display_score)
    return _status_from_outcome(attempt.get("pytest_outcome"))


def _single_display_score(attempt: dict[str, str]) -> str:
    display_scores = [
        score.strip().lower()
        for score in str(attempt.get("ai_display_scores") or "").split(",")
        if score.strip()
    ]
    if len(display_scores) == 1:
        return display_scores[0]
    return ""


def _normalised_outcome(outcome: str | None) -> str:
    outcome = str(outcome or "unknown").lower()
    if outcome == "passed":
        return "passed"
    if outcome == "failed":
        return "failed"
    if outcome in {"error", "skipped", "xfailed", "xpassed"}:
        return outcome
    if outcome == "xfail":
        return "xfailed"
    if outcome == "xpass":
        return "xpassed"
    return "unknown"


def _status_from_display_score(display_score: str) -> str:
    return {
        "pass": "pass",
        "fail": "fail",
        "error": "error",
        "xfail": "xfail",
        "xpass": "fail",
        "unknown": "unknown",
    }.get(display_score, "unknown")


def _status_from_outcome(outcome: str | None) -> str:
    return {
        "passed": "pass",
        "failed": "fail",
        "error": "error",
        "skipped": "skipped",
        "xfailed": "xfail",
        "xpassed": "xpass",
        "unknown": "unknown",
    }[_normalised_outcome(outcome)]


def _format_total_duration(attempts: list[dict[str, str]]) -> str:
    durations = []
    for attempt in attempts:
        value = attempt.get("ai_agent_duration_seconds")
        if value is None:
            continue
        try:
            durations.append(float(value))
        except ValueError:
            continue
    if not durations:
        return "-"
    return _format_duration_seconds(sum(durations))


def _format_duration_seconds(seconds: float) -> str:
    if seconds < 100:
        return f"{seconds:.1f}s"
    return f"{seconds:.0f}s"


def _format_unique_property(attempts: list[dict[str, str]], key: str) -> str:
    values = [
        value
        for value in dict.fromkeys(attempt.get(key, "") for attempt in attempts)
        if value
    ]
    return ",".join(values)


def _is_mcp_mode_name(mode: str) -> bool:
    return mode.endswith("_mcp")


def _format_token_usage(attempts: list[dict[str, str]]) -> str:
    totals = [_sum_int_property(attempts, key) for key in TOKEN_KEYS]
    if all(total == 0 for total in totals):
        return ""
    input_tokens, output_tokens, reasoning_tokens = totals
    return (
        f"tokens in={_format_int(input_tokens)} "
        f"out={_format_int(output_tokens)} "
        f"reason={_format_int(reasoning_tokens)}"
    )


def _format_mcp_calls(attempts: list[dict[str, str]]) -> str:
    calls = _sum_int_property(attempts, "ai_mcp_tool_calls_succeeded")
    calls += _sum_int_property(attempts, "ai_mcp_tool_calls_failed")
    if calls == 0:
        return ""
    return f"calls={calls}"


def _format_performance_quality_lines(
    attempts: list[dict[str, str]],
) -> list[str]:
    lines = []
    multiple_attempts = len(attempts) > 1
    for index, attempt in enumerate(attempts, start=1):
        metrics = recorded_performance_metrics_for_attempt(attempt)
        if not metrics:
            continue
        failed_metrics = [metric for metric in metrics if metric.quality != QUALITY_GOOD]
        if not failed_metrics:
            continue
        attempt_label = _attempt_label(attempt, index)
        suffix = f" (attempt {attempt_label})" if multiple_attempts else ""
        lines.append(f"❌ Error{suffix}: {_format_quality_metrics(failed_metrics)}")
    return lines


def _format_quality_metrics(metrics: list[MetricQuality]) -> str:
    return "; ".join(_format_quality_metric(metric) for metric in metrics)


def _format_quality_metric(metric: MetricQuality) -> str:
    if metric.quality == QUALITY_INDETERMINABLE:
        return f"{metric.label} indeterminate"
    comparator = "<=" if metric.quality == QUALITY_GOOD else ">"
    return (
        f"{metric.label} {_format_metric_value(metric.key, metric.value)} "
        f"{comparator} {_format_metric_value(metric.key, metric.threshold)}"
    )


def _format_metric_value(metric_key: str, value: int | float | None) -> str:
    if value is None:
        return "unknown"
    if metric_key == "duration_seconds":
        return _format_duration_seconds(float(value))
    return _format_int(int(value))


def _format_tool_output_truncation(attempts: list[dict[str, str]]) -> str:
    tokens = _sum_int_property(attempts, "ai_tool_output_truncated_tokens")
    markers = _sum_int_property(attempts, "ai_tool_output_truncation_markers")
    if tokens == 0 and markers == 0:
        return ""
    severities = {
        severity.strip().lower()
        for attempt in attempts
        for severity in str(attempt.get("ai_tool_output_truncation_severity") or "").split(",")
        if severity.strip()
    }
    if "error" in severities:
        prefix = "❌ Error"
    else:
        prefix = "⚠️  Warning"
    if tokens:
        return f"{prefix}: {_format_int(tokens)} tokens truncated"
    return f"{prefix}: {_format_int(markers)} truncation marker{'' if markers == 1 else 's'}"


def _sum_int_property(attempts: list[dict[str, str]], key: str) -> int:
    total = 0
    for attempt in attempts:
        value = attempt.get(key)
        if value is None:
            continue
        try:
            total += int(value)
        except ValueError:
            continue
    return total


def _format_int(value: int) -> str:
    return f"{value:,}"
