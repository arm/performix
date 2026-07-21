# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

from typing import Dict

from sources import CpuSnapshot


def _clamp_percent(pct: float) -> float:
    if pct < 0.0:
        return 0.0
    if pct > 100.0:
        return 100.0
    return pct


def cpu_percent(prev: CpuSnapshot, curr: CpuSnapshot) -> float:
    """Compute CPU utilization percent from two snapshots."""
    total_delta = curr.total - prev.total
    idle_delta = curr.idle - prev.idle
    iowait_delta = curr.iowait - prev.iowait
    if total_delta <= 0:
        return 0.0
    busy = total_delta - idle_delta - iowait_delta
    return _clamp_percent((busy / total_delta) * 100.0)


def iowait_percent(prev: CpuSnapshot, curr: CpuSnapshot) -> float:
    """Compute CPU iowait percent from two snapshots."""
    total_delta = curr.total - prev.total
    iowait_delta = curr.iowait - prev.iowait
    if total_delta <= 0:
        return 0.0
    return _clamp_percent((iowait_delta / total_delta) * 100.0)


def cpu_percents(prev: Dict[str, CpuSnapshot], curr: Dict[str, CpuSnapshot]) -> Dict[str, float]:
    """Compute per-cpu utilization percents keyed by cpu id."""
    out: Dict[str, float] = {}
    for cpu_id, curr_snap in curr.items():
        prev_snap = prev.get(cpu_id)
        if prev_snap is None:
            continue
        out[cpu_id] = cpu_percent(prev_snap, curr_snap)
    return out


def rate_from_delta(prev_val: int, curr_val: int, elapsed: float) -> float:
    """Return per-second rate for counters with basic guardrails. Counters are expected to be non-decreasing except in
    when they wrap around, in which case return 0."""
    if elapsed <= 0:
        return 0.0
    delta = curr_val - prev_val
    if delta < 0:
        return 0.0
    return delta / elapsed
