# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Merge Istanbul coverage directories in a deterministic order."""

from __future__ import annotations

import argparse
import copy
import json
from pathlib import Path
from typing import Any


COUNTER_MAPS = ("s", "f")
LOCATION_MAPS = ("statementMap", "fnMap", "branchMap")


def coverage_files(directory: Path) -> list[Path]:
    files = sorted(path for path in directory.glob("*.json") if path.is_file())
    if not files:
        raise ValueError(f"No coverage JSON files found in {directory}")
    return files


def merge_scalar_counters(
    current: dict[str, int], incoming: dict[str, int]
) -> dict[str, int]:
    if current.keys() != incoming.keys():
        raise ValueError("Istanbul coverage counter keys do not match")
    return {key: current[key] + incoming[key] for key in current}


def merge_branch_counters(
    current: dict[str, list[int]], incoming: dict[str, list[int]]
) -> dict[str, list[int]]:
    if current.keys() != incoming.keys():
        raise ValueError("Istanbul branch counter keys do not match")

    merged: dict[str, list[int]] = {}
    for key, current_hits in current.items():
        incoming_hits = incoming[key]
        if len(current_hits) != len(incoming_hits):
            raise ValueError("Istanbul branch counter lengths do not match")
        merged[key] = [
            current_hit + incoming_hit
            for current_hit, incoming_hit in zip(current_hits, incoming_hits)
        ]
    return merged


def merge_file_coverage(
    current: dict[str, Any], incoming: dict[str, Any]
) -> dict[str, Any]:
    if incoming.get("all") is True:
        return current
    if current.get("all") is True:
        return copy.deepcopy(incoming)

    for map_name in LOCATION_MAPS:
        if current[map_name] != incoming[map_name]:
            raise ValueError(f"Istanbul {map_name} values do not match")

    merged = copy.deepcopy(current)
    for counter_name in COUNTER_MAPS:
        merged[counter_name] = merge_scalar_counters(
            current[counter_name], incoming[counter_name]
        )
    merged["b"] = merge_branch_counters(current["b"], incoming["b"])
    return merged


def merge_coverage_directories(directories: list[Path]) -> dict[str, Any]:
    merged: dict[str, Any] = {}
    for directory in directories:
        for coverage_file in coverage_files(directory):
            report = json.loads(coverage_file.read_text(encoding="utf-8"))
            for source_path, file_coverage in report.items():
                if source_path not in merged:
                    merged[source_path] = copy.deepcopy(file_coverage)
                    continue
                merged[source_path] = merge_file_coverage(
                    merged[source_path], file_coverage
                )
    return merged


def write_merged_coverage(directories: list[Path], output_file: Path) -> None:
    merged = merge_coverage_directories(directories)
    output_file.parent.mkdir(parents=True, exist_ok=True)
    output_file.write_text(json.dumps(merged), encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input_directories", nargs="+", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    write_merged_coverage(args.input_directories, args.output)


if __name__ == "__main__":
    main()
