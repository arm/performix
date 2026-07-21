<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# sysutil-timeline

Collect system utilization metrics from `/proc` and `/sys` and write a fixed-schema CSV timeline.

## Usage

```bash
./sysutil-timeline.py --interval 1.0 --duration 0 --output timeline.csv --flush
```

Options:
- `--interval` seconds between samples (default: `1.0`)
- `--duration` run length in seconds, `0` means until stopped (default: `0`)
- `--output` CSV path (default: `timeline.csv`)
- `--flush` flush each row to disk
- `--proc-root` alternate `/proc` root (fixtures/tests)
- `--sys-root` alternate `/sys` root (fixtures/tests)
- `--thread-scan-interval` seconds between thread scans (default: same as `--interval`)

## Metrics

All metrics are sampled on an interval and written as a single CSV row.

- CPU total + per-core utilization percent from `/proc/stat`
- Total CPU iowait percent derived from `/proc/stat` CPU time accounting
- Load averages from `/proc/loadavg`
- Memory and swap usage from `/proc/meminfo`
- Run queue and blocked processes from `/proc/stat` (`procs_running`, `procs_blocked`)
- Context switches/sec and IRQ/sec from `/proc/stat` (`ctxt`, `intr`)
- Per-core IRQ/sec from `/proc/interrupts`
- Page faults/sec from `/proc/vmstat` (`pgfault`, `pgmajfault`)
- Total process count from counting `/proc/<pid>` files
- Total thread count from summing `Threads:` in `/proc/<pid>/status`
- NUMA memory page rates, per-node page-rate balance, and locality percentages from default `numastat` output when available
- Disk IOPS and bandwidth from `/proc/diskstats` + `/sys/block/<dev>/queue/hw_sector_size`
- Network bandwidth per interface from `/proc/net/dev` (includes `lo`)

## CSV schema

Columns are locked at startup to keep a stable schema. If new CPUs, NUMA nodes, disks, or interfaces appear during a run, they will not be added until the next run.

Header example:

```
ts_utc,uptime_s,load1,load5,load15,cpu_total_percent,cpu0_percent,...,iowait_percent,
mem_total_kb,mem_available_kb,mem_used_kb,mem_used_percent,swap_total_kb,swap_used_kb,
procs_running,procs_blocked,ctxt_per_s,intr_per_s,irq_cpu0_per_s,...,page_faults_per_s,pgmajfaults_per_s,procs_total,
threads_total,numa_hit_per_s,numa_miss_per_s,numa_interleave_hit_per_s,
numa_local_node_per_s,numa_other_node_per_s,numa_local_percent,numa_remote_percent,
numa_node0_allocations_per_s,numa_node1_allocations_per_s,...,
read_iops_sda,write_iops_sda,read_bps_sda,write_bps_sda,rx_bps_lo,tx_bps_lo
```

Aggregate NUMA fields are always present in the CSV schema. The collector reads
default `numastat` counters at startup as a baseline, then writes per-interval
rates instead of raw since-boot counter values. `numa_local_percent` and
`numa_remote_percent` are derived from `local_node` and `other_node` deltas for
the sample interval. If `numastat` is missing, unsupported, malformed, or fails,
aggregate NUMA fields are left empty and collection continues.
Per-node page-rate columns are derived from each node's `local_node` plus
`other_node` counters, are added only when `numastat` is available at startup,
and show memory page balance across nodes, not local-versus-remote split.

## Performance notes

Thread counting walks `/proc/<pid>/status` and can be expensive on busy systems. Use `--thread-scan-interval` to scan less frequently (for example, `5.0` seconds).

## Limitations

- Columns are fixed at startup; hotplugged CPUs, NUMA nodes, disks, or interfaces will not appear until a restart.
- Counter resets (interface down/up, device resets) are treated as zero-rate for that interval.
- Disk stats are limited to devices present under `/sys/block` (partitions are excluded).
- NUMA page-rate and locality stats are aggregated across nodes; per-node output is
  limited to total memory page rates. Per-process `numastat` output is
  not collected.

## Tests

```bash
pytest apap-cli/tools-builtin/sysutil-timeline/tests
```

## Source and Bundle Versioning

This directory is the maintained `sysutil-timeline` source.
The deployable tool bundle version follows the Performix engine version for the
current build, so `scripts/get-tools.py` packages this source under
`sysutil-timeline/<engine-version>/`. Snapshot builds use the same `-dev`
version suffix as the engine binaries.

Recipes reference the `sysutil-timeline` tool integration version, not this
deployed bundle version. The `sysutil-timeline` tool integration keeps its own
independent recipe-facing version, currently `1.0.0`, and uses the
engine-versioned bundle only as an internal deployment dependency.
