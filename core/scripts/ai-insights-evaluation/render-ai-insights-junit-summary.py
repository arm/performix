#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Render a GitHub Markdown summary from an AI Insights JUnit XML file.

The input is the JUnit XML written by pytest with the AI Insights `ai_*`
properties. The output is a Markdown heading and HTML table suitable for
`GITHUB_STEP_SUMMARY`.

Example:
    render-ai-insights-junit-summary.py ai-insights-evaluation.xml
"""

from __future__ import annotations

import argparse
from pathlib import Path

from evaluation_summary import render_markdown_summary
from junit_attempts import attempts_from_junit


def main() -> int:
    args = parse_args()
    attempts = attempts_from_junit(args.junit_xml)
    if not attempts:
        return 0
    if args.title:
        print(f"## {args.title}")
        print()
    print(render_markdown_summary(attempts), end="")
    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Render AI Insights recorded pytest properties as a Markdown table.",
    )
    parser.add_argument(
        "junit_xml",
        type=Path,
        help="pytest JUnit XML file from the AI Insights evaluation suite.",
    )
    parser.add_argument(
        "--title",
        default="AI Insights evaluation",
        help="Markdown heading to print before the table. Use an empty string to omit it.",
    )
    return parser.parse_args()


if __name__ == "__main__":
    raise SystemExit(main())
