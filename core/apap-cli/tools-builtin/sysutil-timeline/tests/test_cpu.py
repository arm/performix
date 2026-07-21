# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os

import pytest

from collector import _build_header, _cpu_ids_in_stable_order
from compute import cpu_percents, iowait_percent
from sources import read_all_cpu_snapshots


def test_cpu_snapshot_parsing_preserves_idle_and_iowait(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")

    snaps = read_all_cpu_snapshots(proc_root=proc1)

    assert snaps["cpu"].idle == 200
    assert snaps["cpu"].iowait == 20
    assert snaps["cpu0"].idle == 100
    assert snaps["cpu0"].iowait == 10


def test_cpu_percent_from_procstat(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    proc2 = os.path.join(fixtures_dir, "proc2")

    prev = read_all_cpu_snapshots(proc_root=proc1)
    curr = read_all_cpu_snapshots(proc_root=proc2)

    pcts = cpu_percents(prev, curr)

    assert pcts["cpu"] == pytest.approx(63.636, rel=1e-3)
    assert pcts["cpu0"] == pytest.approx(61.538, rel=1e-3)
    assert pcts["cpu1"] == pytest.approx(66.666, rel=1e-3)


def test_iowait_percent_from_procstat(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    proc2 = os.path.join(fixtures_dir, "proc2")

    prev = read_all_cpu_snapshots(proc_root=proc1)
    curr = read_all_cpu_snapshots(proc_root=proc2)

    pct = iowait_percent(prev["cpu"], curr["cpu"])

    assert pct == pytest.approx(9.090, rel=1e-3)


def test_cpu_ids_sort_by_numeric_suffix() -> None:
    cpu_ids = _cpu_ids_in_stable_order({
        "cpu": object(),
        "cpu10": object(),
        "cpu2": object(),
        "cpu0": object(),
        "cpu1": object(),
    })

    assert cpu_ids == ["cpu", "cpu0", "cpu1", "cpu2", "cpu10"]


def test_large_cpu_count_header_has_stable_shape() -> None:
    cpu_count = 200
    cpu_ids = ["cpu", *[f"cpu{i}" for i in range(cpu_count)]]

    header = _build_header(
        cpu_ids=cpu_ids,
        disk_devs=[],
        net_ifaces=[],
        numa_node_ids=[],
    )

    assert len(header) == len(set(header))
    cpu_columns = [f"cpu{cpu_index}_percent" for cpu_index in range(cpu_count)]
    irq_columns = [f"irq_cpu{cpu_index}_per_s" for cpu_index in range(cpu_count)]
    assert (
        header[header.index("cpu0_percent") : header.index("iowait_percent")]
        == cpu_columns
    )
    assert header[
        header.index("irq_cpu0_per_s") : header.index("page_faults_per_s")
    ] == irq_columns
