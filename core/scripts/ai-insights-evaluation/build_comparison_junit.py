#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Build one JUnit report for graphing a baseline and historical variants.

The graph scripts read one report. This tool combines matching testcase/attempt
results from saved runs. Graphs use the normal mode-family order and natural
sorting of variant labels; numeric prefixes give the intended progression.

Run from core/scripts/ai-insights-evaluation, replacing the example paths with
your saved reports:

    .venv/bin/python build_comparison_junit.py \\
      --baseline results/history/initial/reporting/ai-insights-evaluation.xml \\
      --comparison-mode performix_mcp_run_query \\
      --iteration 1-initial=results/history/initial/reporting/ai-insights-evaluation.xml \\
      --iteration 2-schema=results/history/schema/reporting/ai-insights-evaluation.xml \\
      --output results/comparison/ai-insights-evaluation.xml

    .venv/bin/python plot_ai_insights_junit.py \\
      results/comparison/ai-insights-evaluation.xml \\
      --output-dir results/comparison/graphs

The baseline report supplies performix_mcp observations; each iteration supplies
performix_mcp_run_query observations. All selected modes must contain the same
testcase and attempt combinations. Open results/comparison/graphs/index.html
to view the result.
"""

from __future__ import annotations

import argparse
from dataclasses import dataclass
from pathlib import Path
import re
import xml.etree.ElementTree as ET

from junit_attempts import attempts_from_junit


BASELINE_MODE = "performix_mcp"
DEFAULT_COMPARISON_MODE = "performix_mcp_run_query"
ITERATION_LABEL_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]*$")


@dataclass(frozen=True)
class Iteration:
    """One labelled historical report, with an optional source mode."""

    label: str
    junit_xml: Path
    mode: str | None = None


def _attempt_key(attempt: dict[str, str]) -> tuple[str, int]:
    test_id = attempt.get("ai_test_id", "").strip()
    try:
        attempt_number = int(attempt.get("ai_attempt", ""))
    except ValueError as exc:
        raise ValueError(f"invalid attempt number for {test_id or 'unknown testcase'}") from exc
    if not test_id:
        raise ValueError("JUnit attempt is missing ai_test_id")
    return test_id, attempt_number


def _attempts_for_mode(junit_xml: Path, mode: str) -> dict[tuple[str, int], dict[str, str]]:
    selected = {}
    for attempt in attempts_from_junit(junit_xml):
        if attempt.get("ai_mode") != mode:
            continue
        key = _attempt_key(attempt)
        if key in selected:
            raise ValueError(f"duplicate {mode} observation for {key} in {junit_xml}")
        selected[key] = attempt
    if not selected:
        raise ValueError(f"no {mode} observations found in {junit_xml}")
    return selected


def _outcome_element(testcase: ET.Element, outcome: str) -> None:
    element_name = {
        "failed": "failure",
        "error": "error",
        "skipped": "skipped",
    }.get(outcome)
    if element_name:
        ET.SubElement(testcase, element_name, message=f"source JUnit outcome: {outcome}")


def _write_attempt(
    testsuite: ET.Element,
    attempt: dict[str, str],
    *,
    output_mode: str,
) -> None:
    test_id, attempt_number = _attempt_key(attempt)
    duration = attempt.get("ai_agent_duration_seconds", "0")
    testcase = ET.SubElement(
        testsuite,
        "testcase",
        {
            "classname": "ai_insights_comparison",
            "name": f"{test_id}[{output_mode}][attempt_{attempt_number:03d}]",
            "time": duration,
        },
    )
    properties = ET.SubElement(testcase, "properties")
    for name, value in sorted(attempt.items()):
        if name == "pytest_outcome":
            continue
        if name == "ai_mode":
            value = output_mode
        ET.SubElement(properties, "property", name=name, value=value)
    _outcome_element(testcase, attempt.get("pytest_outcome", "passed"))


def build_comparison_junit(
    baseline_junit: Path,
    iterations: list[Iteration],
    output_path: Path,
    *,
    comparison_mode: str = DEFAULT_COMPARISON_MODE,
    included_modes: tuple[str, ...] = (),
) -> int:
    """Write a graph-ready JUnit report and return its observation count.

    Each included mode must have the same testcases and attempts as the
    baseline. Otherwise the graphs would compare different sets of results.
    """
    if not iterations:
        raise ValueError("at least one iteration is required")
    labels = [iteration.label for iteration in iterations]
    if len(labels) != len(set(labels)):
        raise ValueError("iteration labels must be unique")
    for label in labels:
        if not ITERATION_LABEL_PATTERN.fullmatch(label):
            raise ValueError(f"invalid iteration label: {label!r}")
    if len(included_modes) != len(set(included_modes)):
        raise ValueError("included modes must be unique")
    if BASELINE_MODE in included_modes:
        raise ValueError(f"{BASELINE_MODE} is already included as the baseline")

    baseline = _attempts_for_mode(baseline_junit, BASELINE_MODE)
    additional_modes = []
    for mode in included_modes:
        attempts = _attempts_for_mode(baseline_junit, mode)
        if attempts.keys() != baseline.keys():
            missing = sorted(baseline.keys() - attempts.keys())
            extra = sorted(attempts.keys() - baseline.keys())
            raise ValueError(
                f"{mode} observations do not match the baseline; "
                f"missing={missing}, extra={extra}"
            )
        additional_modes.append((mode, attempts))
    variants = []
    for iteration in iterations:
        mode = iteration.mode or comparison_mode
        attempts = _attempts_for_mode(iteration.junit_xml, mode)
        if attempts.keys() != baseline.keys():
            missing = sorted(baseline.keys() - attempts.keys())
            extra = sorted(attempts.keys() - baseline.keys())
            raise ValueError(
                f"{iteration.label} observations do not match the baseline; "
                f"missing={missing}, extra={extra}"
            )
        variants.append((iteration, mode, attempts))

    source_attempts = [
        baseline,
        *(attempts for _mode, attempts in additional_modes),
        *(attempts for _iteration, _mode, attempts in variants),
    ]
    count = len(baseline) * (1 + len(additional_modes) + len(variants))
    failures = sum(
        attempt.get("pytest_outcome") == "failed"
        for attempts in source_attempts
        for attempt in attempts.values()
    )
    errors = sum(
        attempt.get("pytest_outcome") == "error"
        for attempts in source_attempts
        for attempt in attempts.values()
    )
    skipped = sum(
        attempt.get("pytest_outcome") == "skipped"
        for attempts in source_attempts
        for attempt in attempts.values()
    )
    testsuite = ET.Element(
        "testsuite",
        {
            "name": "AI Insights historical comparison",
            "tests": str(count),
            "failures": str(failures),
            "errors": str(errors),
            "skipped": str(skipped),
        },
    )
    suite_properties = ET.SubElement(testsuite, "properties")
    ET.SubElement(
        suite_properties,
        "property",
        name="ai_comparison_baseline",
        value=str(baseline_junit),
    )
    for iteration in iterations:
        ET.SubElement(
            suite_properties,
            "property",
            name=f"ai_comparison_iteration_{iteration.label}",
            value=str(iteration.junit_xml),
        )

    for key in sorted(baseline):
        _write_attempt(testsuite, baseline[key], output_mode=BASELINE_MODE)
        for mode, attempts in additional_modes:
            _write_attempt(testsuite, attempts[key], output_mode=mode)
        for iteration, mode, attempts in variants:
            _write_attempt(
                testsuite,
                attempts[key],
                output_mode=f"{mode}.{iteration.label}",
            )

    output_path.parent.mkdir(parents=True, exist_ok=True)
    tree = ET.ElementTree(testsuite)
    ET.indent(tree, space="  ")
    tree.write(output_path, encoding="utf-8", xml_declaration=True)
    return count


def _parse_iteration(value: str) -> Iteration:
    selector, separator, path = value.partition("=")
    if not separator or not selector or not path:
        raise argparse.ArgumentTypeError("expected [MODE:]LABEL=JUNIT_XML")
    mode, mode_separator, label = selector.partition(":")
    if not mode_separator:
        label = mode
        mode = None
    if not label or mode == "":
        raise argparse.ArgumentTypeError("expected [MODE:]LABEL=JUNIT_XML")
    return Iteration(label, Path(path), mode)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument(
        "--baseline",
        required=True,
        type=Path,
        help="JUnit containing Performix MCP",
    )
    parser.add_argument(
        "--iteration",
        required=True,
        action="append",
        type=_parse_iteration,
        help=(
            "Historical comparison as [MODE:]LABEL=JUNIT_XML; "
            "repeat for each iteration"
        ),
    )
    parser.add_argument(
        "--comparison-mode",
        default=DEFAULT_COMPARISON_MODE,
        help=f"Mode selected from each iteration (default: {DEFAULT_COMPARISON_MODE})",
    )
    parser.add_argument(
        "--include-mode",
        action="append",
        default=[],
        help="Additional mode selected from the baseline report; may be repeated",
    )
    parser.add_argument(
        "--output",
        required=True,
        type=Path,
        help="Composite JUnit output path",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        count = build_comparison_junit(
            args.baseline,
            args.iteration,
            args.output,
            comparison_mode=args.comparison_mode,
            included_modes=tuple(args.include_mode),
        )
    except (OSError, ValueError) as exc:
        raise SystemExit(f"Failed to build comparison JUnit: {exc}") from exc
    print(f"Wrote {count} observations to {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
