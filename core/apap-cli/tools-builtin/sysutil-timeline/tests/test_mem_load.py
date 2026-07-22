# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os

from sources import read_loadavg, read_proc_meminfo


def test_meminfo_parsing(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    mem = read_proc_meminfo(proc_root=proc1)
    assert mem.mem_total_kb == 8000000
    assert mem.mem_available_kb == 2000000
    assert mem.mem_used_kb == 6000000
    assert mem.mem_used_pct == 75.0
    assert mem.swap_total_kb == 1000000
    assert mem.swap_used_kb == 750000


def test_loadavg_parsing(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    load1, load5, load15 = read_loadavg(proc_root=proc1)
    assert load1 == 1.0
    assert load5 == 0.5
    assert load15 == 0.25
