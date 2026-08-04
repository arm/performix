#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Run the local AI Insights suite and generate its performance reports."""

from __future__ import annotations

import shutil
import subprocess
import sys
from argparse import ArgumentParser
from pathlib import Path


HARNESS_DIR = Path(__file__).resolve().parent
TEST_MODULE = HARNESS_DIR / "test_ai_insights_evaluation.py"
REPORT_GENERATOR = HARNESS_DIR / "ai_insights_performance_report.py"
LOCAL_REPORTING_DIR = HARNESS_DIR / "results" / "reporting"
JUNIT_XML_NAME = "ai-insights-evaluation.xml"
DASHBOARD_REPORT_NAME = "ai-insights-performance-dashboard.json"


def reporting_dir_from_pytest_args(pytest_args: list[str]) -> Path:
    """Return the reporting directory associated with the pytest results directory."""
    parser = ArgumentParser(add_help=False, allow_abbrev=False)
    parser.add_argument("--ai-results-dir")
    options, _ = parser.parse_known_args(pytest_args)
    if options.ai_results_dir is None:
        return LOCAL_REPORTING_DIR

    results_dir = Path(options.ai_results_dir).expanduser()
    if not results_dir.is_absolute():
        results_dir = HARNESS_DIR / results_dir
    return results_dir / "reporting"


def clear_reporting_dir(reporting_dir: Path) -> None:
    """Create the reporting directory if needed and remove its existing contents."""
    reporting_dir.mkdir(parents=True, exist_ok=True)
    for path in reporting_dir.iterdir():
        if path.is_dir() and not path.is_symlink():
            shutil.rmtree(path)
        else:
            path.unlink()


def run_evaluation(
    pytest_args: list[str],
    *,
    reporting_dir: Path = LOCAL_REPORTING_DIR,
    python_executable: str | None = None,
) -> int:
    """Run pytest, generate reports when JUnit XML exists, and preserve failure status."""
    python = python_executable or sys.executable
    reporting_dir = reporting_dir.resolve()
    junit_xml = reporting_dir / JUNIT_XML_NAME
    payload_dir = reporting_dir / "payload"
    dashboard_report = reporting_dir / DASHBOARD_REPORT_NAME

    # Remove previous outputs so an early pytest failure cannot produce a report
    # from stale JUnit XML. Keep the root directory to support reruns on filesystems
    # where removing a recently used directory can fail, such as Windows via WSL.
    clear_reporting_dir(reporting_dir)

    pytest_command = [
        python,
        "-m",
        "pytest",
        TEST_MODULE.name,
        "--verbose",
        *pytest_args,
        f"--junitxml={junit_xml}",
        "-o",
        "junit_family=legacy",
    ]
    pytest_result = subprocess.run(pytest_command, cwd=HARNESS_DIR)

    if not junit_xml.is_file():
        print(
            f"AI Insights performance report not generated: pytest did not create {junit_xml}",
            file=sys.stderr,
        )
        return pytest_result.returncode or 1

    report_result = subprocess.run(
        [
            python,
            str(REPORT_GENERATOR),
            str(junit_xml),
            "--output-dir",
            str(payload_dir),
            "--dashboard-output",
            str(dashboard_report),
        ],
        cwd=HARNESS_DIR,
    )
    if report_result.returncode == 0:
        print("AI Insights performance reports:")
        print(f"  Benchmark-reporting payload: {payload_dir}")
        print(f"  Dashboard data: {dashboard_report}")

    # A report-generation result must not hide the more important pytest result.
    if pytest_result.returncode != 0:
        return pytest_result.returncode
    return report_result.returncode


def main(argv: list[str] | None = None) -> int:
    pytest_args = list(sys.argv[1:] if argv is None else argv)
    return run_evaluation(
        pytest_args,
        reporting_dir=reporting_dir_from_pytest_args(pytest_args),
    )


if __name__ == "__main__":
    raise SystemExit(main())
