# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import csv
import pathlib
from typing import Dict, List

from framework.code_hotspots.io import ATPERF_FILENAME, PERF_FILENAME, SampleError

def compute_sample_error_rates(perf_samples: Dict[str, float], atp_samples: Dict[str, float]) -> List[SampleError]:
    """
    Compute per-symbol error rates between perf and ATP sample percentages.

    Error is the absolute percentage-point delta |perf_pct - atp_pct|.

    Entries are ordered by perf percentage descending. Perf-only symbols are
    included (ATP value assumed 0). ATP-only symbols are ignored so perf drives
    the set, matching the current hotspot comparison behavior.
    """
    errors: List[SampleError] = []
    for symbol, perf_pct in sorted(perf_samples.items(), key=lambda kv: kv[1], reverse=True):
        perf_val = float(perf_pct or 0.0)
        atp_val = float(atp_samples.get(symbol, 0.0) or 0.0)
        error_pct = abs(perf_val - atp_val)
        errors.append(SampleError(symbol=symbol, perf_pct=perf_val, atp_pct=atp_val, error_pct=error_pct))
    return errors


def sum_perf_percent_within_threshold(errors: List[SampleError], threshold_pct: float) -> float:
    """
    Sum perf percentages for symbols within the error threshold, normalized to 100% total.

    If the raw perf percentages in `errors` do not sum to 100 (because of rounding or
    due to using averages), we scale the within-threshold sum by the total so coverage stays
    bounded at 100.
    """
    total = sum(e.perf_pct for e in errors)
    if total <= 0:
        return 0.0
    within = sum(e.perf_pct for e in errors if e.error_pct <= threshold_pct)
    return within * (100.0 / total)

def average_runs(workload_slug: str, n_runs: int, sanitized_root: pathlib.Path, results_dir: pathlib.Path):
    """
    Average the self percentages reported by perf and atperf across runs (unweighted).
    Uses the sanitized CSV files in sanitized_root/run_<n>/.

    Writes two CSVs to results_dir:
      - "<workload_slug>_atperf_avg.csv" with columns [function_name, periodic_samples_self_percent]
      - "<workload_slug>_perf_avg.csv" with columns [percent, symbol]
    """
    import logging

    atperf_pct: dict[str, list[float]] = {}
    perf_pct: dict[str, list[float]] = {}

    for run_idx in range(1, n_runs + 1):
        run_dir = sanitized_root / f"run_{run_idx}"
        atperf_path = run_dir / ATPERF_FILENAME
        perf_path = run_dir / PERF_FILENAME
        if not atperf_path.exists() or not perf_path.exists():
            logging.warning(f"[{workload_slug}] Skipping run {run_idx} for averages: missing {atperf_path} or {perf_path}")
            continue

        # atperf percents
        with atperf_path.open(newline="", encoding="utf-8") as f:
            reader = csv.DictReader(f)
            for row in reader:
                sym = row.get("function_name", "")
                if not sym or sym == "EMPTY_SYMBOLS":
                    continue
                try:
                    pct = float(row.get("periodic_samples_self_percent", 0.0) or 0.0)
                except Exception:
                    pct = 0.0
                atperf_pct.setdefault(sym, []).append(pct)

        # perf percents
        with perf_path.open(newline="", encoding="utf-8") as f:
            reader = csv.DictReader(f)
            for row in reader:
                sym = row.get("symbol", "")
                if not sym:
                    continue
                try:
                    pct = float(row.get("percent", 0.0) or 0.0)
                except Exception:
                    pct = 0.0
                perf_pct.setdefault(sym, []).append(pct)

    def mean(lst: list[float]) -> float:
        if not lst:
            return 0.0
        return sum(lst) / len(lst)

    results_dir.mkdir(parents=True, exist_ok=True)

    atperf_out = results_dir / f"{workload_slug}_atperf_avg.csv"
    with atperf_out.open("w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f)
        writer.writerow(["function_name", "periodic_samples_self_percent"])
        for sym, vals in sorted(atperf_pct.items(), key=lambda kv: mean(kv[1]), reverse=True):
            writer.writerow([sym, round(mean(vals), 4)])

    perf_out = results_dir / f"{workload_slug}_perf_avg.csv"
    with perf_out.open("w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f)
        writer.writerow(["percent", "symbol"])
        for sym, vals in sorted(perf_pct.items(), key=lambda kv: mean(kv[1]), reverse=True):
            writer.writerow([round(mean(vals), 4), sym])

    logging.info(f"[{workload_slug}] Averaged run data written to '{atperf_out}' and '{perf_out}'")
