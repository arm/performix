# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os

from sources import read_net_dev, read_total_procs, read_total_threads


def test_net_dev_parsing(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    stats = read_net_dev(proc_root=proc1)
    assert "lo" in stats
    assert stats["lo"].bytes_recv == 1000
    assert stats["lo"].bytes_sent == 2000


def test_net_dev_preserves_common_interface_names(tmp_path) -> None:
    proc_root = tmp_path / "proc"
    net_dir = proc_root / "net"
    net_dir.mkdir(parents=True)
    (net_dir / "dev").write_text(
        """Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 1000 0 0 0 0 0 0 0 2000 0 0 0 0 0 0 0
  ens3: 1100 0 0 0 0 0 0 0 2100 0 0 0 0 0 0 0
enp1s0: 1200 0 0 0 0 0 0 0 2200 0 0 0 0 0 0 0
docker0: 1300 0 0 0 0 0 0 0 2300 0 0 0 0 0 0 0
vethabc: 1400 0 0 0 0 0 0 0 2400 0 0 0 0 0 0 0
br-123: 1500 0 0 0 0 0 0 0 2500 0 0 0 0 0 0 0
""",
        encoding="utf-8",
    )

    stats = read_net_dev(proc_root=str(proc_root))

    assert set(stats) == {
        "eth0",
        "ens3",
        "enp1s0",
        "docker0",
        "vethabc",
        "br-123",
    }
    assert stats["eth0"].bytes_recv == 1000
    assert stats["br-123"].bytes_sent == 2500


def test_total_threads(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    total = read_total_threads(proc_root=proc1)
    assert total == 12


def test_total_procs(fixtures_dir: str) -> None:
    proc1 = os.path.join(fixtures_dir, "proc1")
    total = read_total_procs(proc_root=proc1)
    assert total == 2
