# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import csv
import pathlib
from dataclasses import dataclass
from framework.utils import *

ATPERF_FILENAME = "atperf_code_hotspots_results.csv"
PERF_FILENAME   = "perf_code_hotspots_results.csv"

@dataclass
class SampleError:
    """Per-symbol comparison of sample percentages between perf and ATP."""
    symbol: str
    perf_pct: float
    atp_pct: float
    error_pct: float


def load_perf_self_percent(perf_path: pathlib.Path) -> dict[str, float]:
    """Load perf self percentages from sanitized CSV."""
    return load_run_percents(perf_path, key_field="symbol", pct_field="percent")

def load_atperf_self_percent(atperf_path: pathlib.Path) -> dict[str, float]:
    """Load atperf self percentages from sanitized CSV."""
    return load_run_percents(atperf_path, key_field="function_name", pct_field="periodic_samples_self_percent")


def load_run_percents(path: pathlib.Path, key_field: str, pct_field: str) -> dict[str, float]:
    """Generic loader for percent fields from a sanitized CSV."""
    out: dict[str, float] = {}
    with path.open(newline="", encoding="utf-8") as f:
        reader = csv.DictReader(f)
        for row in reader:
            sym = row.get(key_field, "")
            if not sym:
                continue
            try:
                pct = float(row.get(pct_field, 0.0) or 0.0)
            except Exception:
                pct = 0.0
            out[sym] = pct
    return out
