# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import csv
import os
import signal
import shutil
import subprocess
import sys
import threading
import time
from pathlib import Path
from types import SimpleNamespace

import pytest

from collector import (
    INSUFFICIENT_SAMPLES_EXIT_CODE,
    InsufficientSamplesError,
    _build_header,
    collect_sample,
    init_collector_state,
    run_collect,
)


def _copy_fixture(src: str, dst: str) -> None:
    for root, dirs, files in os.walk(src):
        rel = os.path.relpath(root, src)
        target = dst if rel == "." else os.path.join(dst, rel)
        os.makedirs(target, exist_ok=True)
        for name in files:
            shutil.copyfile(os.path.join(root, name), os.path.join(target, name))
        for name in dirs:
            os.makedirs(os.path.join(target, name), exist_ok=True)


def _failed_numastat(_args):
    return SimpleNamespace(returncode=1, stdout="")


@pytest.mark.skipif(os.name == "nt", reason="POSIX signal behavior")
def test_collector_stops_promptly_during_long_interval(
    tmp_path,
    fixtures_dir: str,
) -> None:
    output = tmp_path / "timeline.csv"
    script = Path(__file__).parents[1] / "sysutil-timeline.py"
    process = subprocess.Popen(
        [
            sys.executable,
            str(script),
            "--interval",
            "60",
            "--output",
            str(output),
            "--proc-root",
            os.path.join(fixtures_dir, "proc1"),
            "--sys-root",
            os.path.join(fixtures_dir, "sys"),
        ],
    )

    try:
        deadline = time.monotonic() + 2
        while not output.exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        assert output.exists()

        process.send_signal(signal.SIGINT)
        assert process.wait(timeout=2) == INSUFFICIENT_SAMPLES_EXIT_CODE
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()


def test_collect_writes_csv(tmp_path, fixtures_dir: str) -> None:
    proc_root = tmp_path / "proc"
    proc_root.mkdir()
    _copy_fixture(os.path.join(fixtures_dir, "proc1"), str(proc_root))

    diskstats_path = proc_root / "diskstats"
    if diskstats_path.exists():
        diskstats_path.unlink()

    def update_snapshot() -> None:
        time.sleep(0.12)
        _copy_fixture(os.path.join(fixtures_dir, "proc2"), str(proc_root))
        if diskstats_path.exists():
            diskstats_path.unlink()

    t = threading.Thread(target=update_snapshot, daemon=True)
    t.start()

    output = tmp_path / "timeline.csv"
    run_collect(
        interval=0.1,
        duration=0.25,
        output=str(output),
        proc_root=str(proc_root),
        sys_root="/sys",
        thread_scan_interval=0.1,
        flush=True,
        numa_command_runner=_failed_numastat,
    )

    with open(output, "r", encoding="utf-8") as f:
        rows = list(csv.reader(f))

    assert len(rows) >= 2
    header = rows[0]
    assert header[0] == "ts_utc"
    assert "cpu_total_percent" in header
    assert "iowait_percent" in header
    assert "mem_used_kb" in header
    assert "swap_used_kb" in header
    assert "ctxt_per_s" in header
    assert "intr_per_s" in header
    assert "irq_cpu0_per_s" in header
    assert "irq_cpu1_per_s" in header
    assert "threads_total" in header
    assert "numa_hit_per_s" in header
    assert "numa_remote_percent" in header
    assert "rx_bps_lo" in header
    assert "tx_bps_lo" in header

    for row in rows[1:]:
        assert len(row) == len(header)

    iowait_idx = header.index("iowait_percent")
    assert rows[1][iowait_idx] != ""
    float(rows[1][iowait_idx])

    numa_idx = header.index("numa_hit_per_s")
    assert rows[1][numa_idx] == ""


def test_collect_rejects_fewer_than_two_complete_samples(
    tmp_path,
    fixtures_dir: str,
) -> None:
    output = tmp_path / "timeline.csv"

    with pytest.raises(InsufficientSamplesError, match="collected 1"):
        run_collect(
            interval=0.02,
            duration=0.02,
            output=str(output),
            proc_root=os.path.join(fixtures_dir, "proc1"),
            sys_root=os.path.join(fixtures_dir, "sys"),
            thread_scan_interval=0.02,
            flush=True,
            numa_command_runner=_failed_numastat,
        )

    with open(output, "r", encoding="utf-8") as f:
        rows = list(csv.reader(f))

    assert len(rows) == 2


def test_collect_sample_writes_per_core_irq_rates(tmp_path, fixtures_dir: str) -> None:
    proc_root = tmp_path / "proc"
    proc_root.mkdir()
    _copy_fixture(os.path.join(fixtures_dir, "proc1"), str(proc_root))

    state = init_collector_state(
        proc_root=str(proc_root),
        sys_root="/sys",
        now=0.0,
        numa_command_runner=_failed_numastat,
    )

    _copy_fixture(os.path.join(fixtures_dir, "proc2"), str(proc_root))
    row = collect_sample(
        state,
        proc_root=str(proc_root),
        sys_root="/sys",
        tick_time=2.0,
        elapsed=2.0,
        uptime=2.0,
        thread_scan_interval=1.0,
        numa_command_runner=_failed_numastat,
    )
    header = _build_header(
        cpu_ids=state.cpu_ids,
        disk_devs=state.disk_devs,
        net_ifaces=state.net_ifaces,
        numa_node_ids=state.numa_node_ids,
    )

    assert row[header.index("irq_cpu0_per_s")] == "30.00"
    assert row[header.index("irq_cpu1_per_s")] == "60.00"


def test_collect_sample_blanks_irq_rates_when_interrupts_missing(tmp_path, fixtures_dir: str) -> None:
    proc_root = tmp_path / "proc"
    proc_root.mkdir()
    _copy_fixture(os.path.join(fixtures_dir, "proc1"), str(proc_root))
    os.unlink(proc_root / "interrupts")

    state = init_collector_state(
        proc_root=str(proc_root),
        sys_root="/sys",
        now=0.0,
        numa_command_runner=_failed_numastat,
    )

    row = collect_sample(
        state,
        proc_root=str(proc_root),
        sys_root="/sys",
        tick_time=2.0,
        elapsed=2.0,
        uptime=2.0,
        thread_scan_interval=1.0,
        numa_command_runner=_failed_numastat,
    )
    header = _build_header(
        cpu_ids=state.cpu_ids,
        disk_devs=state.disk_devs,
        net_ifaces=state.net_ifaces,
        numa_node_ids=state.numa_node_ids,
    )

    assert "irq_cpu0_per_s" in header
    assert "irq_cpu1_per_s" in header
    assert row[header.index("irq_cpu0_per_s")] == ""
    assert row[header.index("irq_cpu1_per_s")] == ""


def test_collect_uses_sys_root(tmp_path, fixtures_dir: str) -> None:
    proc_root = tmp_path / "proc"
    proc_root.mkdir()
    _copy_fixture(os.path.join(fixtures_dir, "proc1"), str(proc_root))

    output = tmp_path / "timeline.csv"
    run_collect(
        interval=0.05,
        duration=0.1,
        output=str(output),
        proc_root=str(proc_root),
        sys_root=os.path.join(fixtures_dir, "sys"),
        thread_scan_interval=0.05,
        flush=True,
        numa_command_runner=_failed_numastat,
    )

    with open(output, "r", encoding="utf-8") as f:
        rows = list(csv.reader(f))

    assert len(rows) == 3
    header = rows[0]
    assert "read_iops_sda" in header


def test_collect_writes_numa_values(tmp_path, fixtures_dir: str) -> None:
    proc_root = tmp_path / "proc"
    proc_root.mkdir()
    _copy_fixture(os.path.join(fixtures_dir, "proc1"), str(proc_root))

    outputs = [
        """
                           node0           node1
numa_hit              100             200
numa_miss             10              20
interleave_hit        0               4
local_node            80              120
other_node            5               15
""",
        """
                           node0           node1
numa_hit              120             220
numa_miss             12              24
interleave_hit        0               6
local_node            100             140
other_node            10              20
""",
    ]

    def numastat(_args):
        if len(outputs) > 1:
            output = outputs.pop(0)
        else:
            output = outputs[0]
        return SimpleNamespace(returncode=0, stdout=output)

    output = tmp_path / "timeline.csv"
    run_collect(
        interval=0.05,
        duration=0.1,
        output=str(output),
        proc_root=str(proc_root),
        sys_root="/sys",
        thread_scan_interval=0.05,
        flush=True,
        numa_command_runner=numastat,
    )

    with open(output, "r", encoding="utf-8") as f:
        rows = list(csv.reader(f))

    header = rows[0]
    data = rows[1]
    assert len(data) == len(header)
    assert float(data[header.index("numa_hit_per_s")]) > 0
    assert float(data[header.index("numa_miss_per_s")]) > 0
    assert data[header.index("numa_local_percent")] == "80.00"
    assert data[header.index("numa_remote_percent")] == "20.00"
    assert data[header.index("numa_node0_allocations_per_s")] != ""
    assert data[header.index("numa_node1_allocations_per_s")] != ""


def test_collect_sample_keeps_startup_topology_when_devices_change(tmp_path, fixtures_dir: str) -> None:
    proc_root = tmp_path / "proc"
    sys_root = tmp_path / "sys"
    proc_root.mkdir()
    _copy_fixture(os.path.join(fixtures_dir, "proc1"), str(proc_root))
    with open(proc_root / "diskstats", "a", encoding="utf-8") as f:
        f.write("252 0 vda 10 0 20 0 5 0 30 0 0 0 0 0\n")
    with open(proc_root / "net" / "dev", "a", encoding="utf-8") as f:
        f.write("  eth0: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\n")
    for dev in ("sda", "vda", "nvme0n1"):
        (sys_root / "block" / dev).mkdir(parents=True, exist_ok=True)

    # The collector fixes its output columns from startup topology. Take one
    # sample with the startup devices still present, then remove vda/eth0 and
    # add nvme0n1/docker0 before a second sample. Common devices should keep
    # producing rates, removed devices should be blank, and new devices ignored.
    state = init_collector_state(
        proc_root=str(proc_root),
        sys_root=str(sys_root),
        now=0.0,
        numa_command_runner=_failed_numastat,
    )
    header = _build_header(
        cpu_ids=state.cpu_ids,
        disk_devs=state.disk_devs,
        net_ifaces=state.net_ifaces,
        numa_node_ids=state.numa_node_ids,
    )

    _copy_fixture(os.path.join(fixtures_dir, "proc2"), str(proc_root))
    with open(proc_root / "diskstats", "a", encoding="utf-8") as f:
        f.write("252 0 vda 30 0 60 0 15 0 90 0 0 0 0 0\n")
    with open(proc_root / "net" / "dev", "a", encoding="utf-8") as f:
        f.write("  eth0: 700 0 0 0 0 0 0 0 1000 0 0 0 0 0 0 0\n")

    first_row = collect_sample(
        state,
        proc_root=str(proc_root),
        sys_root=str(sys_root),
        tick_time=2.0,
        elapsed=2.0,
        uptime=2.0,
        thread_scan_interval=1.0,
        numa_command_runner=_failed_numastat,
    )

    assert len(first_row) == len(header)
    assert first_row[header.index("read_iops_sda")] == "10.00"
    assert first_row[header.index("read_bps_sda")] == "15360.00"
    assert first_row[header.index("write_bps_sda")] == "15360.00"
    assert first_row[header.index("read_iops_vda")] == "10.00"
    assert first_row[header.index("read_bps_vda")] == "10240.00"
    assert first_row[header.index("write_bps_vda")] == "15360.00"
    assert first_row[header.index("rx_bps_lo")] == "150.00"
    assert first_row[header.index("rx_bps_eth0")] == "300.00"

    (proc_root / "diskstats").write_text(
        "\n".join(
            [
                "8 0 sda 140 0 320 0 90 0 420 0 0 0 0 0",
                "259 0 nvme0n1 500 0 600 0 300 0 700 0 0 0 0 0",
            ],
        )
        + "\n",
        encoding="utf-8",
    )
    (proc_root / "net" / "dev").write_text(
        """Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1600 0 0 0 0 0 0 0 3200 0 0 0 0 0 0 0
docker0: 5000 0 0 0 0 0 0 0 6000 0 0 0 0 0 0 0
""",
        encoding="utf-8",
    )

    second_row = collect_sample(
        state,
        proc_root=str(proc_root),
        sys_root=str(sys_root),
        tick_time=4.0,
        elapsed=2.0,
        uptime=4.0,
        thread_scan_interval=1.0,
        numa_command_runner=_failed_numastat,
    )

    assert len(second_row) == len(header)
    assert "read_iops_sda" in header
    assert "read_iops_vda" in header
    assert "read_iops_nvme0n1" not in header
    assert "rx_bps_lo" in header
    assert "rx_bps_eth0" in header
    assert "rx_bps_docker0" not in header
    assert second_row[header.index("read_iops_sda")] == "10.00"
    assert second_row[header.index("write_iops_sda")] == "10.00"
    assert second_row[header.index("read_bps_sda")] == "15360.00"
    assert second_row[header.index("write_bps_sda")] == "15360.00"
    assert second_row[header.index("rx_bps_lo")] == "150.00"
    assert second_row[header.index("tx_bps_lo")] == "300.00"
    assert second_row[header.index("read_iops_vda")] == ""
    assert second_row[header.index("write_iops_vda")] == ""
    assert second_row[header.index("read_bps_vda")] == ""
    assert second_row[header.index("write_bps_vda")] == ""
    assert second_row[header.index("rx_bps_eth0")] == ""
    assert second_row[header.index("tx_bps_eth0")] == ""
