#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Create the persistent AI Insights evaluation results archive.
CI archive destination on Artifactory: its.performix-ai-insights/test-artifacts/
"""

from __future__ import annotations

import argparse
from pathlib import Path
from typing import NamedTuple
from zipfile import ZIP_DEFLATED, ZipFile


class ArchiveEntry(NamedTuple):
    source: Path
    archive_path: Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-archive", required=True, type=Path)
    parser.add_argument("--junit-xml", required=True, type=Path)
    parser.add_argument("--dashboard-report", required=True, type=Path)
    parser.add_argument("--results-dir", required=True, type=Path)
    parser.add_argument("--run-artifact-base", required=True, type=Path)
    return parser.parse_args()


def report_entries(junit_xml: Path, dashboard_report: Path) -> list[ArchiveEntry]:
    """Collect the top-level JUnit and dashboard reports when present."""
    entries = []
    for report in (junit_xml, dashboard_report):
        if report.is_file():
            entries.append(ArchiveEntry(report, Path(report.name)))
    return entries


def attempt_result_entries(results_dir: Path) -> list[ArchiveEntry]:
    """Collect files written directly into each test attempt directory."""
    entries = []
    if not results_dir.is_dir():
        return entries

    # Selecting only direct children of an attempt excludes generated directories
    # such as codex_home and codex_workspace, as well as imported source inputs.
    for result_file in results_dir.glob("*/*/attempts/*/*"):
        if not result_file.is_file():
            continue
        relative_path = result_file.relative_to(results_dir)
        entries.append(ArchiveEntry(result_file, Path("results") / relative_path))
    return entries


def prerecorded_metadata_entries(run_artifact_base: Path) -> list[ArchiveEntry]:
    """Collect the metadata identifying each pre-recorded input run."""
    entries = []
    if not run_artifact_base.is_dir():
        return entries

    for metadata_file in run_artifact_base.glob("*/metadata.json"):
        if not metadata_file.is_file():
            continue
        relative_path = metadata_file.relative_to(run_artifact_base)
        entries.append(ArchiveEntry(metadata_file, Path("prerecorded_runs") / relative_path))
    return entries


def collect_archive_entries(args: argparse.Namespace) -> list[ArchiveEntry]:
    """Collect and sort every file to store in the persistent archive."""
    entries = report_entries(args.junit_xml, args.dashboard_report)
    entries.extend(attempt_result_entries(args.results_dir))
    entries.extend(prerecorded_metadata_entries(args.run_artifact_base))
    entries.sort(key=lambda entry: entry.archive_path.as_posix())
    return entries


def main() -> int:
    args = parse_args()
    args.output_archive.unlink(missing_ok=True)
    entries = collect_archive_entries(args)
    if not entries:
        print("No AI Insights evaluation result files found; skipping archive creation")
        return 0

    args.output_archive.parent.mkdir(parents=True, exist_ok=True)
    with ZipFile(args.output_archive, "w", compression=ZIP_DEFLATED) as zip_file:
        for entry in entries:
            zip_file.write(entry.source, entry.archive_path.as_posix())

    print(f"Created {args.output_archive} with {len(entries)} file(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
