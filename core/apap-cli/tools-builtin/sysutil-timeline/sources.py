# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import os
import subprocess
from dataclasses import dataclass
from typing import Optional


NUMASTAT_COUNTERS = (
    "numa_hit",
    "numa_miss",
    "interleave_hit",
    "local_node",
    "other_node",
)
PROC_INTERRUPTS_GLOBAL_ROWS = {"ERR:", "MIS:"}


@dataclass(frozen=True)
class CpuSnapshot:
    """CPU time snapshot from /proc/stat."""
    total: int
    idle: int
    iowait: int


def _snapshot_from_procstat_parts(parts: list[str]) -> CpuSnapshot:
    """Parse a cpu line from /proc/stat into a CpuSnapshot."""
    values = [int(x) for x in parts[1:]]
    total = sum(values)
    # Keep idle and iowait separate so both total CPU busy and iowait can be derived.
    idle = values[3] if len(values) > 3 else 0
    iowait = values[4] if len(values) > 4 else 0
    return CpuSnapshot(total=total, idle=idle, iowait=iowait)


def read_all_cpu_snapshots(proc_root: str = "/proc") -> dict[str, CpuSnapshot]:
    """
    Return dict like {"cpu": snap, "cpu0": snap, "cpu1": snap, ...}
    """
    snaps: dict[str, CpuSnapshot] = {}
    path = os.path.join(proc_root, "stat")
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            if not line.startswith("cpu"):
                break
            parts = line.split()
            if not parts:
                continue
            cpu_id = parts[0]
            if cpu_id == "cpu" or (cpu_id.startswith("cpu") and cpu_id[3:].isdigit()):
                snaps[cpu_id] = _snapshot_from_procstat_parts(parts)

    if "cpu" not in snaps:
        raise RuntimeError("Failed to parse total CPU line from /proc/stat")
    return snaps


@dataclass(frozen=True)
class MemStats:
    """Memory and swap stats from /proc/meminfo."""
    mem_total_kb: int
    mem_available_kb: int
    swap_total_kb: int
    swap_free_kb: int

    @property
    def mem_used_kb(self) -> int:
        return max(0, self.mem_total_kb - self.mem_available_kb)

    @property
    def swap_used_kb(self) -> int:
        return max(0, self.swap_total_kb - self.swap_free_kb)

    @property
    def mem_used_pct(self) -> float:
        if self.mem_total_kb <= 0:
            return 0.0
        return (self.mem_used_kb / self.mem_total_kb) * 100.0


def read_proc_meminfo(proc_root: str = "/proc") -> MemStats:
    """Read MemTotal/MemAvailable/SwapTotal/SwapFree from /proc/meminfo."""
    mem_total = None
    mem_avail = None
    swap_total = None
    swap_free = None

    path = os.path.join(proc_root, "meminfo")
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            if line.startswith("MemTotal:"):
                mem_total = int(line.split()[1])
            elif line.startswith("MemAvailable:"):
                mem_avail = int(line.split()[1])
            elif line.startswith("SwapTotal:"):
                swap_total = int(line.split()[1])
            elif line.startswith("SwapFree:"):
                swap_free = int(line.split()[1])

            if (
                mem_total is not None
                and mem_avail is not None
                and swap_total is not None
                and swap_free is not None
            ):
                break

    if mem_total is None or mem_avail is None or swap_total is None or swap_free is None:
        raise RuntimeError("Could not parse Mem/Swap stats from /proc/meminfo")

    return MemStats(
        mem_total_kb=mem_total,
        mem_available_kb=mem_avail,
        swap_total_kb=swap_total,
        swap_free_kb=swap_free,
    )


def read_loadavg(proc_root: str = "/proc") -> tuple[float, float, float]:
    """Return the 1m, 5m, 15m load averages."""
    path = os.path.join(proc_root, "loadavg")
    with open(path, "r", encoding="utf-8") as f:
        parts = f.read().strip().split()
    if len(parts) < 3:
        raise RuntimeError("Could not parse load averages from /proc/loadavg")
    return float(parts[0]), float(parts[1]), float(parts[2])


def read_proc_stat_counters(proc_root: str = "/proc") -> dict[str, int]:
    """Read select counters from /proc/stat."""
    want = {"ctxt", "intr", "procs_running", "procs_blocked"}
    out: dict[str, int] = {}
    path = os.path.join(proc_root, "stat")
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            parts = line.split()
            if not parts:
                continue
            key = parts[0]
            if key in want:
                if key == "intr":
                    out[key] = int(parts[1])
                else:
                    out[key] = int(parts[1])
            if want.issubset(out.keys()):
                break
    if not want.issubset(out.keys()):
        missing = want - out.keys()
        raise RuntimeError(f"Missing /proc/stat fields: {', '.join(sorted(missing))}")
    return out


def parse_proc_interrupts(output: str) -> Optional[dict[str, int]]:
    """Parse /proc/interrupts and return total interrupt counts per CPU."""
    # /proc/interrupts starts with a CPU header, then each interrupt source has
    # one count column per CPU. For example:
    #
    #            CPU0       CPU1
    #   0:        100        200   IO-APIC   2-edge      timer
    #   1:          5         15   IO-APIC   1-edge      i8042
    # NMI:         10         20   Non-maskable interrupts
    # ERR:          7
    # MIS:          2
    #
    # ERR and MIS are global summary counters, not per-CPU interrupt sources,
    # so they are omitted from the per-core IRQ totals.
    lines = [line for line in output.splitlines() if line.strip()]
    if not lines:
        return None

    header = lines[0].split()
    cpu_names = [
        part.lower()
        for part in header
        if part.startswith("CPU") and part[3:].isdigit()
    ]
    if not cpu_names:
        return None

    totals = {cpu_name: 0 for cpu_name in cpu_names}
    for line in lines[1:]:
        parts = line.split()
        if parts[0] in PROC_INTERRUPTS_GLOBAL_ROWS:
            continue
        if len(parts) < len(cpu_names) + 1:
            continue

        values: list[int] = []
        for value in parts[1 : len(cpu_names) + 1]:
            try:
                values.append(int(value))
            except ValueError:
                values = []
                break
        if not values:
            continue

        for cpu_name, value in zip(cpu_names, values):
            totals[cpu_name] += value

    return totals


def read_proc_interrupts(proc_root: str = "/proc") -> Optional[dict[str, int]]:
    """Read /proc/interrupts and return total interrupt counts per CPU, or None if unavailable."""
    path = os.path.join(proc_root, "interrupts")
    try:
        with open(path, "r", encoding="utf-8") as f:
            return parse_proc_interrupts(f.read())
    except OSError:
        return None


def read_proc_vmstat(proc_root: str = "/proc") -> dict[str, int]:
    """Read select counters from /proc/vmstat."""
    want = {"pgfault", "pgmajfault"}
    out: dict[str, int] = {}
    path = os.path.join(proc_root, "vmstat")
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            parts = line.split()
            if len(parts) != 2:
                continue
            key, val = parts
            if key in want:
                out[key] = int(val)
            if want.issubset(out.keys()):
                break
    if not want.issubset(out.keys()):
        missing = want - out.keys()
        raise RuntimeError(f"Missing /proc/vmstat fields: {', '.join(sorted(missing))}")
    return out


@dataclass(frozen=True)
class DiskStats:
    """Disk counters from /proc/diskstats."""
    reads_completed: int
    sectors_read: int
    writes_completed: int
    sectors_written: int


def _is_top_level_block_device(name: str, sys_root: str = "/sys") -> bool:
    """Filter to top-level block devices (exclude loop/ram/etc)."""
    if name.startswith(("loop", "ram", "fd")):
        return False
    return os.path.exists(os.path.join(sys_root, "block", name))


def read_diskstats(proc_root: str = "/proc", sys_root: str = "/sys") -> dict[str, DiskStats]:
    """Read basic disk stats for top-level block devices."""
    path = os.path.join(proc_root, "diskstats")
    if not os.path.exists(path):
        return {}

    out: dict[str, DiskStats] = {}
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            parts = line.split()
            if len(parts) < 14:
                continue
            name = parts[2]
            if not _is_top_level_block_device(name, sys_root=sys_root):
                continue
            reads_completed = int(parts[3])
            sectors_read = int(parts[5])
            writes_completed = int(parts[7])
            sectors_written = int(parts[9])
            out[name] = DiskStats(
                reads_completed=reads_completed,
                sectors_read=sectors_read,
                writes_completed=writes_completed,
                sectors_written=sectors_written,
            )
    return out


def read_sector_size(dev: str, sys_root: str = "/sys") -> int:
    """Return disk sector size in bytes, defaulting to 512 on errors."""
    path = os.path.join(sys_root, "block", dev, "queue", "hw_sector_size")
    try:
        with open(path, "r", encoding="utf-8") as f:
            return int(f.read().strip())
    except OSError:
        return 512


@dataclass(frozen=True)
class NetDevStats:
    """Network counters from /proc/net/dev."""
    bytes_recv: int
    bytes_sent: int


def read_net_dev(proc_root: str = "/proc") -> dict[str, NetDevStats]:
    """Read per-interface rx/tx byte counters from /proc/net/dev."""
    path = os.path.join(proc_root, "net", "dev")
    if not os.path.exists(path):
        return {}

    out: dict[str, NetDevStats] = {}
    with open(path, "r", encoding="utf-8") as f:
        lines = f.readlines()
    for line in lines[2:]:
        if ":" not in line:
            continue
        iface, rest = line.split(":", 1)
        iface = iface.strip()
        parts = rest.split()
        if len(parts) < 16:
            continue
        bytes_recv = int(parts[0])
        bytes_sent = int(parts[8])
        out[iface] = NetDevStats(bytes_recv=bytes_recv, bytes_sent=bytes_sent)
    return out


@dataclass(frozen=True)
class NumaStats:
    """Aggregated default numastat counters across NUMA nodes."""
    counters: dict[str, int]
    node_allocations: dict[str, int]


def parse_numastat_output(output: str) -> Optional[NumaStats]:
    """Parse default numastat output and aggregate node columns."""
    counters: dict[str, int] = {}
    per_counter_values: dict[str, list[int]] = {}
    node_names: list[str] = []
    wanted = set(NUMASTAT_COUNTERS)

    for line in output.splitlines():
        parts = line.split()
        if len(parts) >= 1 and all(part.startswith("node") for part in parts):
            node_names = parts
            continue
        if len(parts) < 2 or parts[0] not in wanted:
            continue

        values: list[int] = []
        for value in parts[1:]:
            try:
                values.append(int(value))
            except ValueError:
                return None
        counters[parts[0]] = sum(values)
        per_counter_values[parts[0]] = values

    if set(counters.keys()) != wanted:
        return None
    if not node_names:
        return NumaStats(counters=counters, node_allocations={})

    local_values = per_counter_values["local_node"]
    other_values = per_counter_values["other_node"]
    if len(node_names) != len(local_values) or len(node_names) != len(other_values):
        return None

    node_allocations = {
        node: local_values[i] + other_values[i]
        for i, node in enumerate(node_names)
    }
    return NumaStats(counters=counters, node_allocations=node_allocations)


def read_numastat(command_runner=None) -> Optional[NumaStats]:
    """Run numastat and return aggregated counters, or None when unavailable."""
    if command_runner is None:
        command_runner = lambda args: subprocess.run(
            args,
            capture_output=True,
            text=True,
            check=False,
        )

    try:
        result = command_runner(["numastat"])
    except OSError:
        return None

    if result.returncode != 0:
        return None

    return parse_numastat_output(result.stdout)


def read_total_procs(proc_root: str = "/proc") -> int:
    """Return total process count by counting numeric entries in /proc."""
    try:
        entries = os.listdir(proc_root)
    except OSError as exc:
        raise RuntimeError(f"Could not list {proc_root}") from exc
    return sum(1 for entry in entries if entry.isdigit())


def read_total_threads(proc_root: str = "/proc") -> int:
    """Return total thread count by summing /proc/<pid>/status Threads."""
    total = 0
    try:
        entries = os.listdir(proc_root)
    except OSError as exc:
        raise RuntimeError(f"Could not list {proc_root}") from exc

    for entry in entries:
        if not entry.isdigit():
            continue
        status_path = os.path.join(proc_root, entry, "status")
        try:
            with open(status_path, "r", encoding="utf-8") as f:
                for line in f:
                    if line.startswith("Threads:"):
                        total += int(line.split()[1])
                        break
        except OSError:
            continue
    return total
