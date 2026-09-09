# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Create deterministic directional core-to-core latency asymmetry in ASCT runs.

The modification selects a pair of cores whose measured A-to-B and B-to-A
latencies are initially comparable, then increases one direction by a supplied
ratio while leaving the reverse direction unchanged. For example, a ratio of
six changes two directions measured at about 25 ns to 150 ns and 25 ns. This
introduces a clear, repeatable asymmetry for evaluating whether AI Insights
identifies and explains the abnormal core-to-core communication cost.

Only the renderer-facing core-to-core latency CSV is changed. The remaining
measurements and archive members retain the context of the original captured
run.
"""

from __future__ import annotations

import json
import math
import zipfile
from dataclasses import dataclass
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
DEFAULT_TARGET_DIRECTIONAL_RATIO = 6.0
MAX_ORIGINAL_DIRECTIONAL_RATIO = 1.5
C2C_LATENCY_COLUMNS = ("CPUA", "CPUB", "LATENCY", "CPUA_NODE", "MEMBIND_NODE")


@dataclass(frozen=True)
class _CoreDirection:
    row: dict[str, str]
    latency: float


@dataclass(frozen=True)
class _ComparableCorePair:
    source_cpu: str
    target_cpu: str
    source_node: str
    memory_node: str
    forward_row: dict[str, str]
    forward_latency: float
    reverse_row: dict[str, str]
    reverse_latency: float


def _cpu_sort_key(cpu: str) -> tuple[int, int | str]:
    try:
        return 0, int(cpu)
    except ValueError:
        return 1, cpu


def _pair_record(row: dict[str, str], latency: float) -> dict[str, Any]:
    return {
        "source_cpu": row["CPUA"],
        "target_cpu": row["CPUB"],
        "source_node": row["CPUA_NODE"],
        "memory_node": row["MEMBIND_NODE"],
        "latency_ns": latency,
    }


def _directional_ratio(pair: _ComparableCorePair) -> float:
    return max(pair.forward_latency, pair.reverse_latency) / min(
        pair.forward_latency, pair.reverse_latency
    )


def _create_latency_asymmetry(
    data: bytes, member: str, target_directional_ratio: float
) -> tuple[bytes, list[dict[str, Any]], list[dict[str, Any]]]:
    fieldnames, rows = _read_csv(data, member)
    expected = ["", *C2C_LATENCY_COLUMNS]
    if fieldnames != expected or not rows:
        raise ValueError(f"{member} has an unsupported core-to-core latency schema")

    indexed: dict[tuple[str, str, str, str], _CoreDirection] = {}
    for row in rows:
        source_cpu = row["CPUA"].strip()
        target_cpu = row["CPUB"].strip()
        source_node = row["CPUA_NODE"].strip()
        memory_node = row["MEMBIND_NODE"].strip()
        if not source_cpu or not target_cpu or source_cpu == target_cpu:
            raise ValueError(f"{member} contains an invalid core pair")
        if not source_node or not memory_node:
            raise ValueError(f"{member} contains incomplete topology data")
        key = (source_cpu, target_cpu, source_node, memory_node)
        if key in indexed:
            raise ValueError(f"{member} contains duplicate comparable directions")
        indexed[key] = _CoreDirection(
            row=row,
            latency=_finite_positive(row["LATENCY"], "core-to-core latency"),
        )

    comparable_candidates: list[_ComparableCorePair] = []
    for key, forward in indexed.items():
        source_cpu, target_cpu, source_node, memory_node = key
        if _cpu_sort_key(source_cpu) >= _cpu_sort_key(target_cpu):
            continue
        reverse = indexed.get((target_cpu, source_cpu, source_node, memory_node))
        if reverse is not None:
            comparable_candidates.append(
                _ComparableCorePair(
                    source_cpu=source_cpu,
                    target_cpu=target_cpu,
                    source_node=source_node,
                    memory_node=memory_node,
                    forward_row=forward.row,
                    forward_latency=forward.latency,
                    reverse_row=reverse.row,
                    reverse_latency=reverse.latency,
                )
            )
    if not comparable_candidates:
        raise ValueError(f"{member} has no reverse core pair with comparable memory binding")

    candidates = [
        candidate
        for candidate in comparable_candidates
        if _directional_ratio(candidate) <= MAX_ORIGINAL_DIRECTIONAL_RATIO
    ]
    if not candidates:
        raise ValueError(
            f"original directional ratio must not exceed "
            f"{MAX_ORIGINAL_DIRECTIONAL_RATIO}"
        )

    selected = min(
        candidates,
        key=lambda candidate: (
            candidate.source_node,
            candidate.memory_node,
            _cpu_sort_key(candidate.source_cpu),
            _cpu_sort_key(candidate.target_cpu),
        ),
    )
    generated_latency = round(
        selected.reverse_latency * target_directional_ratio, 6
    )
    original = [
        _pair_record(selected.forward_row, selected.forward_latency),
        _pair_record(selected.reverse_row, selected.reverse_latency),
    ]
    selected.forward_row["LATENCY"] = repr(generated_latency)
    generated = [
        _pair_record(selected.forward_row, generated_latency),
        _pair_record(selected.reverse_row, selected.reverse_latency),
    ]
    return _write_csv(fieldnames, rows), original, generated


def create_core_to_core_latency_asymmetry_run(
    source: Path,
    destination: Path,
    target_directional_ratio: float = DEFAULT_TARGET_DIRECTIONAL_RATIO,
) -> None:
    """Increase one direction of a comparable ASCT core pair by the supplied ratio."""

    if (
        not math.isfinite(target_directional_ratio)
        or target_directional_ratio <= 1
    ):
        raise ValueError("target directional ratio must be finite and greater than one")
    if source.resolve() == destination.resolve():
        raise ValueError("input and output archives must be different paths")
    with zipfile.ZipFile(source, "r") as archive:
        root, infos, contents, comment = _validated_members(archive)

    c2c_member = f"{root}/tool/asct/0/output/c2c-latency.csv"
    metadata_member = f"{root}/metadata.json"
    required = {c2c_member, metadata_member}
    missing = sorted(required - contents.keys())
    if missing:
        raise ValueError(f"ASCT archive is missing required members: {missing}")
    metadata = _json_object(contents[metadata_member], metadata_member)
    if metadata.get("run.recipe_name") != "asct":
        raise ValueError("archive is not an ASCT run")
    if metadata.get("run.result") != "success":
        raise ValueError("ASCT run did not complete successfully")

    c2c_data, original_pair, generated_pair = _create_latency_asymmetry(
        contents[c2c_member], c2c_member, target_directional_ratio
    )
    contents[c2c_member] = c2c_data
    _atomic_write_archive(destination, infos, contents, comment)

    report = {
        "source_archive_sha256": _sha256_file(source),
        "generated_archive_sha256": _sha256_file(destination),
        "transformation": {
            "name": "core_to_core_latency_asymmetry",
            "version": TRANSFORM_VERSION,
            "target_directional_ratio": target_directional_ratio,
        },
        "changed_members": [c2c_member],
        "original_core_pair": original_pair,
        "generated_core_pair": generated_pair,
    }
    destination.with_name("run-modification-report.json").write_text(
        json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
