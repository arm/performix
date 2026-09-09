# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Create deterministic low-peak-bandwidth derivatives of exported ASCT runs.

The modification scales every peak and cross-NUMA bandwidth measurement by the
same factor until the highest result is 50% of theoretical peak. For example,
a run whose maximum is 160 GB/s at 80% of theoretical peak is scaled to
100 GB/s at 50%. Preserving the relative differences between measurements
creates a coherent, repeatable low-bandwidth signal for evaluating whether AI
Insights identifies and explains the shortfall.

Only the renderer-facing bandwidth CSVs are changed. The remaining measurements
and archive members are preserved unchanged.
"""

from __future__ import annotations

import json
import zipfile
from pathlib import Path
from typing import Any

from .archive_helper import (
    _atomic_write_archive,
    _finite_positive,
    _json_object,
    _read_csv,
    _sha256_file,
    _validated_members,
    _write_csv,
)


TRANSFORM_VERSION = 1
LOW_PEAK_BANDWIDTH_PERCENT = 50
PEAK_BANDWIDTH_COLUMNS = (
    "Traffic type",
    "Peak BW [GB/s]",
    "% of Peak Theoretical",
)


def _scale_peak_csv(
    data: bytes, member: str
) -> tuple[bytes, list[dict[str, Any]], list[dict[str, Any]], float]:
    fieldnames, rows = _read_csv(data, member)
    expected = ["", *PEAK_BANDWIDTH_COLUMNS]
    if fieldnames != expected or not rows:
        raise ValueError(f"{member} has an unsupported peak-bandwidth schema")

    parsed_rows = [
        (
            row,
            _finite_positive(row[PEAK_BANDWIDTH_COLUMNS[1]], "peak bandwidth"),
            _finite_positive(row[PEAK_BANDWIDTH_COLUMNS[2]], "peak percentage"),
        )
        for row in rows
    ]
    original_max = max(percent for _, _, percent in parsed_rows)
    if LOW_PEAK_BANDWIDTH_PERCENT >= original_max:
        raise ValueError(
            f"original maximum percentage must exceed {LOW_PEAK_BANDWIDTH_PERCENT}"
        )
    scale = LOW_PEAK_BANDWIDTH_PERCENT / original_max
    original: list[dict[str, Any]] = []
    generated: list[dict[str, Any]] = []
    for row, bandwidth, percent in parsed_rows:
        original.append(
            {
                "traffic_type": row[PEAK_BANDWIDTH_COLUMNS[0]],
                "bandwidth_gb_s": bandwidth,
                "percent_of_peak_theoretical": percent,
            }
        )
        row[PEAK_BANDWIDTH_COLUMNS[1]] = repr(bandwidth * scale)
        row[PEAK_BANDWIDTH_COLUMNS[2]] = repr(percent * scale)
        generated.append(
            {
                "traffic_type": row[PEAK_BANDWIDTH_COLUMNS[0]],
                "bandwidth_gb_s": bandwidth * scale,
                "percent_of_peak_theoretical": percent * scale,
            }
        )
    return _write_csv(fieldnames, rows), original, generated, scale


def _scale_matrix_csv(data: bytes, member: str, scale: float) -> bytes:
    fieldnames, rows = _read_csv(data, member)
    if len(fieldnames) < 2 or fieldnames[0] != "" or not rows:
        raise ValueError(f"{member} has an unsupported cross-NUMA schema")
    for row in rows:
        for column in fieldnames[1:]:
            value = _finite_positive(row[column], f"cross-NUMA value {column!r}")
            row[column] = repr(value * scale)
    return _write_csv(fieldnames, rows)


def create_low_peak_bandwidth_run(
    source: Path,
    destination: Path,
) -> None:
    """Scale the renderer-facing CSVs in a successful ASCT run."""

    if source.resolve() == destination.resolve():
        raise ValueError("input and output archives must be different paths")
    with zipfile.ZipFile(source, "r") as archive:
        root, infos, contents, comment = _validated_members(archive)

    output_root = f"{root}/tool/asct/0/output"
    peak_member = f"{output_root}/peak-bandwidth.csv"
    cross_member = f"{output_root}/cross-numa-bandwidth.csv"
    metadata_member = f"{root}/metadata.json"
    required = {
        peak_member,
        cross_member,
        metadata_member,
    }
    missing = sorted(required - contents.keys())
    if missing:
        raise ValueError(f"ASCT archive is missing required members: {missing}")
    metadata = _json_object(contents[metadata_member], metadata_member)
    if metadata.get("run.recipe_name") != "asct":
        raise ValueError("archive is not an ASCT run")
    if metadata.get("run.result") != "success":
        raise ValueError("ASCT run did not complete successfully")

    peak_data, original_rows, generated_rows, scale = _scale_peak_csv(
        contents[peak_member], peak_member
    )
    contents[peak_member] = peak_data
    contents[cross_member] = _scale_matrix_csv(
        contents[cross_member], cross_member, scale
    )

    _atomic_write_archive(destination, infos, contents, comment)
    report = {
        "source_archive_sha256": _sha256_file(source),
        "generated_archive_sha256": _sha256_file(destination),
        "transformation": {
            "name": "low_peak_bandwidth",
            "version": TRANSFORM_VERSION,
            "max_percent": LOW_PEAK_BANDWIDTH_PERCENT,
            "scale_factor": scale,
        },
        "changed_members": [cross_member, peak_member],
        "original_peak_bandwidth_rows": original_rows,
        "generated_peak_bandwidth_rows": generated_rows,
    }
    report_path = destination.with_name("run-modification-report.json")
    report_path.write_text(
        json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
