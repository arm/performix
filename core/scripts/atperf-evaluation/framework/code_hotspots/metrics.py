# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import math
import pathlib
from typing import Dict, List, Tuple

from framework.code_hotspots.io import load_run_percents

RunPercents = Dict[str, float]


def _normalize_run(pcts: RunPercents) -> RunPercents:
    """Scale percents so they sum to 100; if total is zero, return as-is."""
    total = sum(pcts.values())
    if total <= 0:
        return pcts
    scale = 100.0 / total
    return {k: v * scale for k, v in pcts.items()}


def _mean_profile(runs: List[RunPercents]) -> RunPercents:
    """Compute mean percent per symbol across runs (runs already normalized)."""
    if not runs:
        return {}
    symbols = set()
    for r in runs:
        symbols.update(r.keys())
    means: RunPercents = {}
    for s in symbols:
        means[s] = sum(r.get(s, 0.0) for r in runs) / len(runs)
    return means


def _l1_distance(run: RunPercents, mean: RunPercents) -> float:
    """L1 distance between run and mean; 0.5 factor bounds to [0, 100]."""
    symbols = set(run.keys()) | set(mean.keys()) # union of symbols
    dist = sum(abs(run.get(s, 0.0) - mean.get(s, 0.0)) for s in symbols)
    return 0.5 * dist # multiply by 0.5 to bound to [0, 100], otherwise max distance is 200


def profile_variability(
    n_runs: int,
    sanitized_root: pathlib.Path,
    filename: str,
    key_field: str,
    pct_field: str,
) -> Tuple[float, List[float]]:
    """
    Compute mean L1 distance to the mean profile for a given tool.

    Returns (mean_dist, per_run_distances). If no runs present, returns zeros.
    """
    runs: List[RunPercents] = []
    for run_idx in range(1, int(n_runs) + 1):
        run_dir = sanitized_root / f"run_{run_idx}"
        path = run_dir / filename
        if not path.exists():
            continue
        run_pct = load_run_percents(path, key_field=key_field, pct_field=pct_field)
        run_pct = _normalize_run(run_pct)
        runs.append(run_pct)

    if not runs:
        return 0.0, []

    mean_prof = _mean_profile(runs)
    dists = [_l1_distance(r, mean_prof) for r in runs]
    mean_d = sum(dists) / len(dists)
    return mean_d, dists
