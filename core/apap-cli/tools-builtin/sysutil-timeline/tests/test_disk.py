# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os
import shutil
from types import SimpleNamespace

import pytest

from collector import _build_header, collect_sample, init_collector_state

from compute import rate_from_delta
from sources import read_diskstats, read_sector_size


def _write_diskstats(proc_root, device_names: list[str]) -> None:
    proc_root.mkdir(parents=True, exist_ok=True)
    lines = [
        f"8 {index} {name} 100 0 200 0 50 0 300 0 0 0 0 0"
        for index, name in enumerate(device_names)
    ]
    (proc_root / "diskstats").write_text(
        "\n".join(lines) + "\n",
        encoding="utf-8",
    )


def _make_sys_block(sys_root, device_names: list[str]) -> None:
    for name in device_names:
        (sys_root / "block" / name).mkdir(parents=True, exist_ok=True)


def test_diskstats_parsing(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    sys_root = os.path.join(fixtures_dir, "sys")

    stats = read_diskstats(proc_root=proc1, sys_root=sys_root)
    assert "sda" in stats
    assert "sda1" not in stats
    sda = stats["sda"]
    assert sda.reads_completed == 100
    assert sda.sectors_read == 200
    assert sda.writes_completed == 50
    assert sda.sectors_written == 300

    sector_size = read_sector_size("sda", sys_root=sys_root)
    assert sector_size == 4096


def test_disk_rate_from_delta() -> None:
    read_bps = rate_from_delta(100, 200, 2.0) * 512
    assert read_bps == 25600.0


def test_diskstats_accepts_common_top_level_device_names(tmp_path) -> None:
    proc_root = tmp_path / "proc"
    sys_root = tmp_path / "sys"
    top_level_devices = ["nvme0n1", "mmcblk0", "vda", "xvda", "dm-0", "md0"]
    partition_names = ["nvme0n1p1", "mmcblk0p1"]
    excluded_devices = ["loop0", "ram0", "fd0"]

    _write_diskstats(
        proc_root,
        [*top_level_devices, *partition_names, *excluded_devices],
    )
    _make_sys_block(sys_root, top_level_devices)

    stats = read_diskstats(proc_root=str(proc_root), sys_root=str(sys_root))

    assert set(stats) == set(top_level_devices)


def test_sector_size_defaults_to_512_when_sysfs_file_missing(tmp_path) -> None:
    sys_root = tmp_path / "sys"
    (sys_root / "block" / "vda" / "queue").mkdir(parents=True)

    assert read_sector_size("vda", sys_root=str(sys_root)) == 512


@pytest.mark.parametrize("sector_size", [4096, 512, None], ids=["4kn", "512", "missing"])
def test_collector_disk_rates_use_512_byte_sectors(tmp_path, fixtures_dir, sector_size):
    sys_root = tmp_path / "sys"
    shutil.copytree(os.path.join(fixtures_dir, "sys"), sys_root)
    sector_file = sys_root / "block" / "sda" / "queue" / "hw_sector_size"
    if sector_size is None:
        sector_file.unlink()
    else:
        sector_file.write_text(str(sector_size), encoding="utf-8")

    def failed_numastat(_args):
        return SimpleNamespace(returncode=1, stdout="")

    state = init_collector_state(
        os.path.join(fixtures_dir, "proc1"), str(sys_root), now=0,
        numa_command_runner=failed_numastat,
    )
    header = _build_header(
        state.cpu_ids, state.disk_devs, state.net_ifaces, state.numa_node_ids,
    )
    row = collect_sample(
        state,
        proc_root=os.path.join(fixtures_dir, "proc2"),
        sys_root=str(sys_root),
        tick_time=2,
        elapsed=2,
        uptime=2,
        thread_scan_interval=10,
        numa_command_runner=failed_numastat,
    )
    sample = dict(zip(header, row))

    # Both sector counters increase by 60 over two seconds: 60 * 512 / 2.
    assert sample["read_bps_sda"] == "15360.00"
    assert sample["write_bps_sda"] == "15360.00"
    assert sample["read_iops_sda"] == "10.00"
    assert sample["write_iops_sda"] == "10.00"
