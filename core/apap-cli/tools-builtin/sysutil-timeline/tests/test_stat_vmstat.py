# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os

from sources import (
    parse_proc_interrupts,
    read_proc_interrupts,
    read_proc_stat_counters,
    read_proc_vmstat,
)


def test_proc_stat_counters(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    counters = read_proc_stat_counters(proc_root=proc1)
    assert counters["ctxt"] == 1000
    assert counters["intr"] == 2000
    assert counters["procs_running"] == 2
    assert counters["procs_blocked"] == 1


def test_vmstat_parsing(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    stats = read_proc_vmstat(proc_root=proc1)
    assert stats["pgfault"] == 1000
    assert stats["pgmajfault"] == 10


def test_proc_interrupts_parsing_sums_sources_per_cpu() -> None:
    stats = parse_proc_interrupts(
        """
           CPU0       CPU1
  0:        100        200   IO-APIC   2-edge      timer
  1:          5         15   IO-APIC   1-edge      i8042
NMI:         10         20   Non-maskable interrupts
ERR:          7
MIS:          2
BAD: not-a-number       7   malformed
"""
    )

    assert stats == {"cpu0": 115, "cpu1": 235}


def test_proc_interrupts_single_cpu_skips_global_summary_rows() -> None:
    stats = parse_proc_interrupts(
        """
           CPU0
  0:        100   IO-APIC   2-edge      timer
NMI:         10   Non-maskable interrupts
ERR:          7
MIS:          2
"""
    )

    assert stats == {"cpu0": 110}


def test_proc_interrupts_missing_returns_none(tmp_path) -> None:
    assert read_proc_interrupts(proc_root=str(tmp_path)) is None


def test_proc_interrupts_without_cpu_header_returns_none() -> None:
    assert parse_proc_interrupts("no cpu columns here\n 0: 1 2 timer\n") is None


def test_proc_interrupts_parses_arm_gic_style_rows() -> None:
    stats = parse_proc_interrupts(
        """
              CPU0       CPU1       CPU2       CPU3
  11:          1          2          3          4     GICv3  27 Level     arch_timer
  12:          5          6          7          8     GICv3  30 Level     kvm guest timer
IPI0:          9         10         11         12     Rescheduling interrupts
IPI1:         13         14         15         16     Function call interrupts
BAD:          x         99         99         99     malformed
"""
    )

    assert stats == {
        "cpu0": 28,
        "cpu1": 32,
        "cpu2": 36,
        "cpu3": 40,
    }


def test_proc_interrupts_parses_many_cpu_columns() -> None:
    stats = parse_proc_interrupts(
        """
              CPU0       CPU1       CPU2       CPU3       CPU4       CPU5       CPU6       CPU7
  42:          1          2          3          4          5          6          7          8     GICv3  42 Level device
IPI0:          8          7          6          5          4          3          2          1     Rescheduling interrupts
"""
    )

    assert stats == {
        "cpu0": 9,
        "cpu1": 9,
        "cpu2": 9,
        "cpu3": 9,
        "cpu4": 9,
        "cpu5": 9,
        "cpu6": 9,
        "cpu7": 9,
    }
