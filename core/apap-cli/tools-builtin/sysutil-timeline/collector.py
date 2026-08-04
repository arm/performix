# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import argparse
import math
import os
import signal
import sys
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Optional

from compute import cpu_percents, iowait_percent, rate_from_delta
from sources import (
    NUMASTAT_COUNTERS,
    NumaStats,
    read_all_cpu_snapshots,
    read_diskstats,
    read_loadavg,
    read_net_dev,
    read_numastat,
    read_proc_interrupts,
    read_proc_meminfo,
    read_proc_stat_counters,
    read_proc_vmstat,
    read_sector_size,
    read_total_procs,
    read_total_threads,
)
from writer import CsvWriter


MIN_INTERVAL_SECONDS = 0.01
MAX_INTERVAL_SECONDS = 60.0
MIN_COMPLETE_SAMPLES = 2
INSUFFICIENT_SAMPLES_EXIT_CODE = 3

_STOP_EVENT = threading.Event()

NUMA_COLUMNS = [
    "numa_hit_per_s",
    "numa_miss_per_s",
    "numa_interleave_hit_per_s",
    "numa_local_node_per_s",
    "numa_other_node_per_s",
    "numa_local_percent",
    "numa_remote_percent",
]


class InsufficientSamplesError(RuntimeError):
    """Raised when collection stops before enough complete samples exist."""

    def __init__(self, samples_collected: int) -> None:
        self.samples_collected = samples_collected
        super().__init__(
            f"collected {samples_collected} complete samples; "
            f"at least {MIN_COMPLETE_SAMPLES} are required"
        )


def _minimum_collection_duration(interval: float) -> float:
    return interval * MIN_COMPLETE_SAMPLES


def _has_sufficient_collection_duration(
    interval: float,
    duration: float,
) -> bool:
    if duration == 0:
        return True
    minimum_duration = _minimum_collection_duration(interval)
    tolerance = max(1e-9, minimum_duration * 1e-9)
    return duration + tolerance >= minimum_duration


def _handle_signal(_signum: int, _frame) -> None:
    """Signal handler to request a clean stop in the main loop."""
    _STOP_EVENT.set()


def _utc_iso_now() -> str:
    """Return an ISO8601 UTC timestamp suitable for timeline CSVs."""
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


def _cpu_ids_in_stable_order(snaps: dict[str, object]) -> list[str]:
    """Return cpu ids in stable order: total first, then cpu0..cpuN."""
    per_core = [k for k in snaps.keys() if k != "cpu" and k.startswith("cpu") and k[3:].isdigit()]
    per_core_sorted = sorted(per_core, key=lambda x: int(x[3:]))
    return ["cpu"] + per_core_sorted


def _numa_node_ids_in_stable_order(node_allocations: dict[str, int]) -> list[str]:
    """Return NUMA node ids in numeric order for nodeN names."""
    return sorted(node_allocations.keys(), key=lambda node: int(node[4:]))


def _fmt_float(value: Optional[float], decimals: int = 2) -> str:
    """Format floats for CSV output, using empty string for missing values."""
    if value is None:
        return ""
    return f"{value:.{decimals}f}"


def _fmt_int(value: Optional[int]) -> str:
    """Format ints for CSV output, using empty string for missing values."""
    if value is None:
        return ""
    return str(value)


def parse_args(argv: list[str]) -> argparse.Namespace:
    """Parse CLI arguments for collector execution."""
    p = argparse.ArgumentParser(description="Collect system utilization stats to timeline CSV.")
    p.add_argument("--interval", type=float, default=1.0, help="Collection interval in seconds (default: 1.0).")
    p.add_argument(
        "--duration",
        type=float,
        default=0.0,
        help="Run duration in seconds. 0 means run until stopped (default: 0).",
    )
    p.add_argument(
        "--output",
        type=str,
        default="timeline.csv",
        help="Path to output CSV (default: timeline.csv).",
    )
    p.add_argument(
        "--flush",
        action="store_true",
        help="Flush CSV after each row (safer for abrupt exits, slightly slower).",
    )
    p.add_argument(
        "--proc-root",
        type=str,
        default="/proc",
        help="Alternate /proc root (for fixtures/tests).",
    )
    p.add_argument(
        "--sys-root",
        type=str,
        default="/sys",
        help="Alternate /sys root (for fixtures/tests).",
    )
    p.add_argument(
        "--thread-scan-interval",
        type=float,
        default=None,
        help="Thread scan interval in seconds (default: same as --interval).",
    )
    return p.parse_args(argv)


def ensure_parent_dir(path: str) -> None:
    """Ensure the output directory exists for the given file path."""
    parent = os.path.dirname(os.path.abspath(path))
    if parent and not os.path.exists(parent):
        os.makedirs(parent, exist_ok=True)


def _build_header(
    cpu_ids: list[str],
    disk_devs: list[str],
    net_ifaces: list[str],
    numa_node_ids: list[str],
) -> list[str]:
    """Build the CSV header based on detected CPU/disk/net devices."""
    cpu_cols = [
        ("cpu_total_percent" if cid == "cpu" else f"{cid}_percent")
        for cid in cpu_ids
    ]
    cpu_cols.append("iowait_percent")
    irq_cols = [
        f"irq_{cid}_per_s"
        for cid in cpu_ids
        if cid != "cpu"
    ]
    disk_cols: list[str] = []
    for dev in disk_devs:
        disk_cols.extend(
            [
                f"read_iops_{dev}",
                f"write_iops_{dev}",
                f"read_bps_{dev}",
                f"write_bps_{dev}",
            ]
        )
    net_cols: list[str] = []
    for iface in net_ifaces:
        net_cols.extend([f"rx_bps_{iface}", f"tx_bps_{iface}"])
    numa_node_cols = [
        f"numa_{node}_allocations_per_s"
        for node in numa_node_ids
    ]

    return [
        "ts_utc",
        "uptime_s",
        "load1",
        "load5",
        "load15",
        *cpu_cols,
        "mem_total_kb",
        "mem_available_kb",
        "mem_used_kb",
        "mem_used_percent",
        "swap_total_kb",
        "swap_used_kb",
        "procs_running",
        "procs_blocked",
        "ctxt_per_s",
        "intr_per_s",
        *irq_cols,
        "page_faults_per_s",
        "pgmajfaults_per_s",
        "procs_total",
        "threads_total",
        *NUMA_COLUMNS,
        *numa_node_cols,
        *disk_cols,
        *net_cols,
    ]


@dataclass
class CollectorState:
    """Mutable collection state for computing deltas between samples."""
    prev_cpu: dict[str, object]
    prev_disk: dict[str, object]
    prev_net: dict[str, object]
    prev_numa: Optional[NumaStats]
    prev_irq: Optional[dict[str, int]]
    prev_stat: dict[str, int]
    prev_vmstat: dict[str, int]
    procs_total: int
    threads_total: int
    last_thread_scan: float
    cpu_ids: list[str]
    disk_devs: list[str]
    net_ifaces: list[str]
    numa_node_ids: list[str]
    sector_sizes: dict[str, int]


@dataclass
class TickScheduler:
    """Schedule ticks for the collection loop with injectable time/wait."""
    interval: float
    prev_tick: float
    next_tick: float

    @classmethod
    def start(cls, interval: float, now: float) -> "TickScheduler":
        return cls(interval=interval, prev_tick=now, next_tick=now + interval)

    def wait_for_tick(
        self,
        now_fn,
        wait_fn,
        deadline: Optional[float] = None,
    ) -> Optional[tuple[float, float]]:
        """Wait until the next tick, a deadline, or a stop request.

        wait_fn follows threading.Event.wait semantics: it returns True when a
        stop was requested and False when its timeout elapsed.
        """
        now = now_fn()
        deadline_tolerance = max(1e-9, self.interval * 1e-9)
        if (
            deadline is not None
            and self.next_tick - deadline > deadline_tolerance
        ):
            if now < deadline:
                wait_fn(deadline - now)
            return None

        if now < self.next_tick and wait_fn(self.next_tick - now):
            return None

        tick_time = now_fn()
        if tick_time < self.next_tick:
            return None

        elapsed = tick_time - self.prev_tick
        if elapsed <= 0:
            elapsed = self.interval

        self.prev_tick = tick_time
        self.next_tick += self.interval
        return tick_time, elapsed


def init_collector_state(proc_root: str, sys_root: str, now: float, numa_command_runner=None) -> CollectorState:
    """Initialize collection state from the current system snapshots."""
    prev_cpu = read_all_cpu_snapshots(proc_root=proc_root)
    cpu_ids = _cpu_ids_in_stable_order(prev_cpu)

    prev_disk = read_diskstats(proc_root=proc_root, sys_root=sys_root)
    disk_devs = sorted(prev_disk.keys())
    sector_sizes = {dev: read_sector_size(dev, sys_root=sys_root) for dev in disk_devs}

    prev_net = read_net_dev(proc_root=proc_root)
    net_ifaces = sorted(prev_net.keys())

    prev_numa = read_numastat(command_runner=numa_command_runner)
    numa_node_ids = _numa_node_ids_in_stable_order(prev_numa.node_allocations) if prev_numa is not None else []

    prev_irq = read_proc_interrupts(proc_root=proc_root)
    prev_stat = read_proc_stat_counters(proc_root=proc_root)
    prev_vmstat = read_proc_vmstat(proc_root=proc_root)

    procs_total = read_total_procs(proc_root=proc_root)
    threads_total = read_total_threads(proc_root=proc_root)

    return CollectorState(
        prev_cpu=prev_cpu,
        prev_disk=prev_disk,
        prev_net=prev_net,
        prev_numa=prev_numa,
        prev_irq=prev_irq,
        prev_stat=prev_stat,
        prev_vmstat=prev_vmstat,
        procs_total=procs_total,
        threads_total=threads_total,
        last_thread_scan=now,
        cpu_ids=cpu_ids,
        disk_devs=disk_devs,
        net_ifaces=net_ifaces,
        numa_node_ids=numa_node_ids,
        sector_sizes=sector_sizes,
    )


def _counter_delta(prev: NumaStats, curr: NumaStats, counter: str) -> int:
    delta = curr.counters[counter] - prev.counters[counter]
    if delta < 0:
        return 0
    return delta


def _aggregate_numa_values(prev: Optional[NumaStats], curr: Optional[NumaStats], elapsed: float) -> list[str]:
    """Return aggregate numastat counter rates and locality percentages.

    These values come from the standard numastat counters summed across NUMA
    nodes, such as preferred-node hits/misses and local versus remote pages.
    """
    if prev is None or curr is None:
        return [""] * len(NUMA_COLUMNS)

    rates = []
    for counter in NUMASTAT_COUNTERS:
        rates.append(_fmt_float(rate_from_delta(prev.counters[counter], curr.counters[counter], elapsed), 2))

    local_delta = _counter_delta(prev, curr, "local_node")
    remote_delta = _counter_delta(prev, curr, "other_node")
    locality_total = local_delta + remote_delta
    if locality_total <= 0:
        rates.extend(["", ""])
    else:
        rates.append(_fmt_float((local_delta / locality_total) * 100.0, 2))
        rates.append(_fmt_float((remote_delta / locality_total) * 100.0, 2))

    return rates


def _per_node_numa_values(
    prev: Optional[NumaStats],
    curr: Optional[NumaStats],
    elapsed: float,
    numa_node_ids: list[str],
) -> list[str]:
    """Return per-node page allocation rates.

    These values come from each node's total page count and show how many pages
    were allocated from each NUMA node, independent of whether those pages were
    local or remote to the process that triggered the allocation.
    """
    if prev is None or curr is None:
        return [""] * len(numa_node_ids)

    values = []
    for node in numa_node_ids:
        prev_value = prev.node_allocations.get(node)
        curr_value = curr.node_allocations.get(node)
        if prev_value is None or curr_value is None:
            values.append("")
            continue
        values.append(_fmt_float(rate_from_delta(prev_value, curr_value, elapsed), 2))
    return values


def _per_core_irq_values(
    prev: Optional[dict[str, int]],
    curr: Optional[dict[str, int]],
    elapsed: float,
    cpu_ids: list[str],
) -> list[str]:
    """Return per-core IRQ rates in the same order as per-core CPU columns."""
    per_core_cpu_ids = [cid for cid in cpu_ids if cid != "cpu"]
    if prev is None or curr is None:
        return [""] * len(per_core_cpu_ids)

    values = []
    for cpu_id in per_core_cpu_ids:
        prev_value = prev.get(cpu_id)
        curr_value = curr.get(cpu_id)
        if prev_value is None or curr_value is None:
            values.append("")
            continue
        values.append(_fmt_float(rate_from_delta(prev_value, curr_value, elapsed), 2))
    return values


def collect_sample(
    state: CollectorState,
    *,
    proc_root: str,
    sys_root: str,
    tick_time: float,
    elapsed: float,
    uptime: float,
    thread_scan_interval: float,
    numa_command_runner=None,
) -> list[str]:
    """Collect a single row of samples and update state for the next tick."""
    prev_cpu = state.prev_cpu
    curr_cpu = read_all_cpu_snapshots(proc_root=proc_root)
    cpu_pcts = cpu_percents(prev_cpu, curr_cpu)
    total_iowait_pct = iowait_percent(prev_cpu["cpu"], curr_cpu["cpu"])
    state.prev_cpu = curr_cpu

    load1, load5, load15 = read_loadavg(proc_root=proc_root)
    mem = read_proc_meminfo(proc_root=proc_root)

    curr_stat = read_proc_stat_counters(proc_root=proc_root)
    ctxt_per_s = rate_from_delta(state.prev_stat["ctxt"], curr_stat["ctxt"], elapsed)
    intr_per_s = rate_from_delta(state.prev_stat["intr"], curr_stat["intr"], elapsed)
    procs_running = curr_stat["procs_running"]
    procs_blocked = curr_stat["procs_blocked"]
    state.prev_stat = curr_stat

    curr_vmstat = read_proc_vmstat(proc_root=proc_root)
    pgfault_per_s = rate_from_delta(state.prev_vmstat["pgfault"], curr_vmstat["pgfault"], elapsed)
    pgmajfault_per_s = rate_from_delta(state.prev_vmstat["pgmajfault"], curr_vmstat["pgmajfault"], elapsed)
    state.prev_vmstat = curr_vmstat

    curr_disk = read_diskstats(proc_root=proc_root, sys_root=sys_root)
    curr_net = read_net_dev(proc_root=proc_root)
    curr_numa = read_numastat(command_runner=numa_command_runner)
    curr_irq = read_proc_interrupts(proc_root=proc_root)
    numa_values = _aggregate_numa_values(state.prev_numa, curr_numa, elapsed)
    numa_node_values = _per_node_numa_values(state.prev_numa, curr_numa, elapsed, state.numa_node_ids)
    irq_values = _per_core_irq_values(state.prev_irq, curr_irq, elapsed, state.cpu_ids)

    state.procs_total = read_total_procs(proc_root=proc_root)
    if tick_time - state.last_thread_scan >= thread_scan_interval:
        state.threads_total = read_total_threads(proc_root=proc_root)
        state.last_thread_scan = tick_time

    cpu_values = [_fmt_float(cpu_pcts.get(cid), 2) for cid in state.cpu_ids]

    row: list[str] = [
        _utc_iso_now(),
        _fmt_float(uptime, 3),
        _fmt_float(load1, 2),
        _fmt_float(load5, 2),
        _fmt_float(load15, 2),
        *cpu_values,
        _fmt_float(total_iowait_pct, 2),
        _fmt_int(mem.mem_total_kb),
        _fmt_int(mem.mem_available_kb),
        _fmt_int(mem.mem_used_kb),
        _fmt_float(mem.mem_used_pct, 2),
        _fmt_int(mem.swap_total_kb),
        _fmt_int(mem.swap_used_kb),
        _fmt_int(procs_running),
        _fmt_int(procs_blocked),
        _fmt_float(ctxt_per_s, 2),
        _fmt_float(intr_per_s, 2),
        *irq_values,
        _fmt_float(pgfault_per_s, 2),
        _fmt_float(pgmajfault_per_s, 2),
        _fmt_int(state.procs_total),
        _fmt_int(state.threads_total),
        *numa_values,
        *numa_node_values,
    ]

    for dev in state.disk_devs:
        prev = state.prev_disk.get(dev)
        curr = curr_disk.get(dev)
        if prev is None or curr is None:
            row.extend(["", "", "", ""])
            continue
        read_iops = rate_from_delta(prev.reads_completed, curr.reads_completed, elapsed)
        write_iops = rate_from_delta(prev.writes_completed, curr.writes_completed, elapsed)
        sector_size = state.sector_sizes.get(dev, 512)
        read_bps = rate_from_delta(prev.sectors_read, curr.sectors_read, elapsed) * sector_size
        write_bps = rate_from_delta(prev.sectors_written, curr.sectors_written, elapsed) * sector_size
        row.extend(
            [
                _fmt_float(read_iops, 2),
                _fmt_float(write_iops, 2),
                _fmt_float(read_bps, 2),
                _fmt_float(write_bps, 2),
            ]
        )

    for iface in state.net_ifaces:
        prev = state.prev_net.get(iface)
        curr = curr_net.get(iface)
        if prev is None or curr is None:
            row.extend(["", ""])
            continue
        rx_bps = rate_from_delta(prev.bytes_recv, curr.bytes_recv, elapsed)
        tx_bps = rate_from_delta(prev.bytes_sent, curr.bytes_sent, elapsed)
        row.extend([_fmt_float(rx_bps, 2), _fmt_float(tx_bps, 2)])

    state.prev_disk = curr_disk
    state.prev_net = curr_net
    state.prev_numa = curr_numa
    state.prev_irq = curr_irq
    return row


def run_collect(
    interval: float,
    duration: float,
    output: str,
    proc_root: str,
    sys_root: str,
    thread_scan_interval: float,
    flush: bool,
    numa_command_runner=None,
) -> None:
    """Collect stats on a timer and append rows to the CSV."""
    _STOP_EVENT.clear()
    signal.signal(signal.SIGINT, _handle_signal)
    signal.signal(signal.SIGTERM, _handle_signal)

    ensure_parent_dir(output)

    baseline_time = time.monotonic()
    state = init_collector_state(
        proc_root=proc_root,
        sys_root=sys_root,
        now=baseline_time,
        numa_command_runner=numa_command_runner,
    )

    header = _build_header(
        cpu_ids=state.cpu_ids,
        disk_devs=state.disk_devs,
        net_ifaces=state.net_ifaces,
        numa_node_ids=state.numa_node_ids,
    )
    writer = CsvWriter(output, header=header, flush=flush)
    collection_start = time.monotonic()
    state.last_thread_scan = collection_start
    end_time: Optional[float] = (
        None if duration == 0 else collection_start + duration
    )
    scheduler = TickScheduler.start(interval=interval, now=collection_start)
    samples_collected = 0
    try:
        while True:
            if _STOP_EVENT.is_set():
                break

            tick = scheduler.wait_for_tick(
                time.monotonic,
                _STOP_EVENT.wait,
                deadline=end_time,
            )
            if tick is None or _STOP_EVENT.is_set():
                break
            tick_time, elapsed = tick
            uptime = tick_time - collection_start

            row = collect_sample(
                state,
                proc_root=proc_root,
                sys_root=sys_root,
                tick_time=tick_time,
                elapsed=elapsed,
                uptime=uptime,
                thread_scan_interval=thread_scan_interval,
                numa_command_runner=numa_command_runner,
            )
            writer.write_row(row)
            samples_collected += 1
    finally:
        writer.close()

    if samples_collected < MIN_COMPLETE_SAMPLES:
        raise InsufficientSamplesError(samples_collected)


def main(argv: list[str]) -> int:
    """CLI entrypoint returning an exit code without raising SystemExit."""
    args = parse_args(argv)
    if (
        not math.isfinite(args.interval)
        or args.interval < MIN_INTERVAL_SECONDS
        or args.interval > MAX_INTERVAL_SECONDS
    ):
        print(
            f"ERROR: --interval must be between {MIN_INTERVAL_SECONDS:g} "
            f"and {MAX_INTERVAL_SECONDS:g} seconds",
            file=sys.stderr,
        )
        return 2
    if not math.isfinite(args.duration) or args.duration < 0:
        print("ERROR: --duration must be >= 0", file=sys.stderr)
        return 2
    if not _has_sufficient_collection_duration(args.interval, args.duration):
        minimum_duration = _minimum_collection_duration(args.interval)
        print(
            f"ERROR: at least {MIN_COMPLETE_SAMPLES} complete samples are "
            f"required; --duration must be at least {minimum_duration:g} "
            f"seconds for --interval {args.interval:g}",
            file=sys.stderr,
        )
        return INSUFFICIENT_SAMPLES_EXIT_CODE

    thread_scan_interval = args.thread_scan_interval
    if thread_scan_interval is None:
        thread_scan_interval = args.interval
    if thread_scan_interval <= 0:
        print("ERROR: --thread-scan-interval must be > 0", file=sys.stderr)
        return 2

    try:
        run_collect(
            interval=args.interval,
            duration=args.duration,
            output=args.output,
            proc_root=args.proc_root,
            sys_root=args.sys_root,
            thread_scan_interval=thread_scan_interval,
            flush=args.flush,
        )
    except InsufficientSamplesError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return INSUFFICIENT_SAMPLES_EXIT_CODE
    except RuntimeError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
