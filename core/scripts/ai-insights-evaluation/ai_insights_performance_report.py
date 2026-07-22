#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Generate AI Insights performance outputs from JUnit XML.

The report named by `REPORT_NAME` is written into the payload consumed by
benchmark-reporting. See benchmark-reporting.yaml, which passes the payload
to atperf-benchmark-reporting/benchmark-reporting.py.

Separately, dashboard benchmark data is written to the path supplied
through `--dashboard-output` for github-action-benchmark. See workflow
`ai-insights-evaluation.yaml`.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any

from junit_attempts import attempts_from_junit
from performance_quality import (
    PERFORMIX_MCP_MODE,
    QUALITY_INDETERMINABLE,
    has_recorded_performance_evaluation,
    number,
    quality_distribution,
    recorded_performance_metrics_for_attempt,
)

# Name of the AI Insights performance report written to output/reports
# and consumed by benchmark-reporting:
REPORT_NAME = "ai_insights_performance.json"


def build_payloads(
    attempts: list[dict[str, str]],
) -> tuple[dict[str, Any], dict[str, Any]]:
    """Build benchmark-reporting metadata and tabular metric rows.

    Every AI Insights attempt is preserved in report named by REPORT_NAME so
    downstream tooling can inspect raw runtimes and token counts. Quality
    assessment is narrower: only production Performix MCP attempts with a
    recorded performance assessment contribute GOOD/POOR/INDETERMINABLE values.
    Attempts without one are marked as `ignored_for_quality` instead of being
    treated as failures.
    """
    headers = [
        "test_id",
        "mode",
        "attempt",
        "attempts_total",
        "pytest_outcome",
        "duration_seconds",
        "input_tokens",
        "output_tokens",
        "reasoning_output_tokens",
        "mcp_completed_calls",
        "duration_threshold_seconds",
        "input_token_threshold",
        "output_token_threshold",
        "duration_quality",
        "input_tokens_quality",
        "output_tokens_quality",
        "ignored_for_quality",
    ]
    rows = []
    quality_values = []
    performance_thresholds = {}

    for attempt in attempts:
        test_id = attempt.get("ai_test_id", "")
        mode = attempt.get("ai_mode", "")
        duration = number(attempt.get("ai_agent_duration_seconds"))
        input_tokens = number(attempt.get("ai_input_tokens"))
        output_tokens = number(attempt.get("ai_output_tokens"))
        reasoning_output_tokens = number(attempt.get("ai_reasoning_output_tokens"))
        mcp_completed_calls = number(attempt.get("ai_mcp_completed_calls"))
        evaluated = has_recorded_performance_evaluation(attempt)

        metrics = {}
        duration_quality = ""
        input_quality = ""
        output_quality = ""
        if evaluated:
            metrics = {
                metric.key: metric
                for metric in recorded_performance_metrics_for_attempt(attempt)
            }
            attempt_thresholds = {
                key: metric.threshold
                for key, metric in metrics.items()
            }
            existing_thresholds = performance_thresholds.setdefault(test_id, attempt_thresholds)
            if existing_thresholds != attempt_thresholds:
                raise ValueError(f"conflicting recorded performance thresholds for {test_id}")
            duration_quality = metrics["duration_seconds"].quality
            input_quality = metrics["input_tokens"].quality
            output_quality = metrics["output_tokens"].quality
            quality_values.extend([duration_quality, input_quality, output_quality])

        rows.append([
            test_id,
            mode,
            number(attempt.get("ai_attempt")),
            number(attempt.get("ai_attempts_total")),
            attempt.get("pytest_outcome", ""),
            duration,
            input_tokens,
            output_tokens,
            reasoning_output_tokens,
            mcp_completed_calls,
            metrics["duration_seconds"].threshold if evaluated else None,
            metrics["input_tokens"].threshold if evaluated else None,
            metrics["output_tokens"].threshold if evaluated else None,
            duration_quality,
            input_quality,
            output_quality,
            not evaluated,
        ])

    warnings = []
    if not quality_values:
        quality_values.append(QUALITY_INDETERMINABLE)
        warnings.append(
            f"no {PERFORMIX_MCP_MODE} attempts with a recorded performance assessment "
            "found in AI Insights JUnit XML"
        )

    report = {"headers": headers, "rows": rows}
    metadata = {
        "benchmark_name": "ai_insights_evaluation",
        "benchmark_args": {
            "ai_mode": PERFORMIX_MCP_MODE,
            "performance_thresholds": performance_thresholds,
        },
        "system": {},
        "workloads": [
            {
                "index": 0,
                "command": "ai-insights-evaluation",
                "executable": "ai-insights-evaluation",
                "name": "ai-insights-evaluation",
            }
        ],
        "n_runs": sum(1 for attempt in attempts if attempt.get("ai_mode") == PERFORMIX_MCP_MODE),
        "data_quality_distribution": quality_distribution(quality_values),
        "errors": [],
        "warnings": warnings,
        "data": {
            "ai_mode": PERFORMIX_MCP_MODE,
            "performance_thresholds": performance_thresholds,
            "attempts": len(attempts),
            "report": f"reports/{REPORT_NAME}",
        },
    }
    return metadata, report


def build_dashboard_benchmarks(report: dict[str, Any]) -> list[dict[str, Any]]:
    """Return Performix MCP measurements for the GitHub dashboard.

    Include every recorded runtime and token count, even when pytest skipped
    quality assessment for that attempt. This keeps the measurements visible
    on the dashboard without classifying them as GOOD, POOR, or INDETERMINABLE
    in the benchmark report or Slack alerts.
    """
    headers = report.get("headers", [])
    rows = report.get("rows", [])
    if not isinstance(headers, list) or not isinstance(rows, list):
        return []

    def cell(row: list[Any], name: str) -> Any:
        try:
            index = headers.index(name)
        except ValueError:
            return None
        return row[index] if index < len(row) else None

    benchmarks = []
    for row in rows:
        if not isinstance(row, list) or cell(row, "mode") != PERFORMIX_MCP_MODE:
            continue
        test_id = str(cell(row, "test_id") or "unknown")
        attempt = cell(row, "attempt")
        attempts_total = cell(row, "attempts_total")
        name_prefix = test_id
        if isinstance(attempts_total, int) and attempts_total > 1 and isinstance(attempt, int):
            name_prefix = f"{test_id} attempt {attempt}"
        for metric, label, unit in (
            ("duration_seconds", "wall-clock time", "s"),
            ("input_tokens", "input tokens", "tokens"),
            ("output_tokens", "output tokens", "tokens"),
        ):
            value = cell(row, metric)
            if isinstance(value, (int, float)):
                benchmarks.append({"name": f"{name_prefix} {label}", "unit": unit, "value": value})
    return benchmarks


def write_payloads(metadata: dict[str, Any], report: dict[str, Any], output_dir: Path) -> None:
    """Write benchmark-reporting-compatible output/metadata.json and reports."""
    reports_dir = output_dir / "reports"
    reports_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    (reports_dir / REPORT_NAME).write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Generate benchmark-reporting payloads from AI Insights JUnit XML.",
    )
    parser.add_argument("junit_xml", type=Path)
    parser.add_argument("--output-dir", type=Path, default=Path("output"))
    parser.add_argument(
        "--dashboard-output",
        type=Path,
        required=True,
        help="github-action-benchmark output path.",
    )
    args = parser.parse_args()

    attempts = attempts_from_junit(args.junit_xml)
    metadata, report = build_payloads(attempts)
    write_payloads(metadata, report, args.output_dir)
    args.dashboard_output.write_text(
        json.dumps(build_dashboard_benchmarks(report), indent=2) + "\n",
        encoding="utf-8",
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
