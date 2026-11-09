# System Utilisation Query Guide

Use `run_query` to inspect the System Utilisation time series in `flat_table`.

## flat_table schema

Each row is one collection sample. `uptime_s` is seconds from the start of the
recording and is the primary time axis. `ts_utc` is the corresponding UTC wall
clock timestamp.

State fields are the value observed at the sample. Rate, utilisation and
percentage fields describe the preceding collection interval. This distinction
matters when bucketing: average and maximum are meaningful for interval rates,
but a sampled memory, swap or process value is not an interval average.

### CPU and scheduling

- `cpu_total_percent`: aggregate CPU utilisation percentage.
- `cpuN_percent`: utilisation percentage for CPU `N`.
- `iowait_percent`: percentage of aggregate CPU time reported as I/O wait.
- `load1`, `load5`, `load15`: system load averages over one, five and fifteen
  minutes.
- `procs_running`, `procs_blocked`: sampled counts of runnable and blocked
  processes.
- `procs_total`, `threads_total`: sampled total process and thread counts.
- `ctxt_per_s`, `intr_per_s`: context switches and interrupts per second.
- `irq_cpuN_per_s`: hardware interrupts per second for CPU `N`, summed across
  IRQ sources.

### Memory and NUMA

- `mem_total_kb`, `mem_available_kb`, `mem_used_kb`, `mem_used_percent`:
  sampled system memory capacity, available memory, derived used memory and
  used-memory percentage. Used memory is `MemTotal - MemAvailable`.
- `swap_total_kb`, `swap_used_kb`: sampled swap capacity and used swap.
- `page_faults_per_s`, `pgmajfaults_per_s`: page faults and major page faults
  per second.
- `numa_hit_per_s`, `numa_miss_per_s`, `numa_interleave_hit_per_s`:
  rates of NUMA preferred-node hits, preferred-node misses and interleave-policy
  hits.
- `numa_local_node_per_s`, `numa_other_node_per_s`: rates of pages allocated
  from the process's local and other NUMA nodes.
- `numa_local_percent`, `numa_remote_percent`: local and remote allocation
  percentages for the collection interval.
- `numa_nodeN_allocations_per_s`: allocation rate for NUMA node `N`, regardless
  of whether pages are local to the allocating process.

### Storage and network

- `read_iops_<device>`, `write_iops_<device>`: read and write operations per
  second for a discovered block device.
- `read_bps_<device>`, `write_bps_<device>`: read and write throughput in bytes
  per second for a discovered block device.
- `rx_bps_<interface>`, `tx_bps_<interface>`: received and transmitted network
  throughput in bytes per second for a discovered interface.

The collector fixes the columns for each recording at startup. CPU, NUMA node,
block-device and network-interface suffixes therefore depend on the target, but
the column families and their units are as listed above.

## Initial inspection

Start with the time-series overview below. Target-specific CPU, IRQ, NUMA,
storage and network columns are listed in `table_schemas` when they fit. If
`table_schemas_query` is present, discover the relevant column family through
the catalogue for follow-up when the overview shows a material question about
those resources. The overview does not require target-specific column names.

### Time-series overview

```sql
WITH samples AS (
  SELECT
    *,
    coalesce(list_sum(list_transform(
      list_value(unpack(COLUMNS('^(read_iops|write_iops)_.*$'))),
      lambda x: coalesce(try_cast(x AS DOUBLE), 0)
    )), 0) AS total_storage_iops,
    coalesce(list_sum(list_transform(
      list_value(unpack(COLUMNS('^(read_bps|write_bps)_.*$'))),
      lambda x: coalesce(try_cast(x AS DOUBLE), 0)
    )), 0) AS total_storage_bps,
    coalesce(list_sum(list_transform(
      list_value(unpack(COLUMNS('^(rx_bps|tx_bps)_.*$'))),
      lambda x: coalesce(try_cast(x AS DOUBLE), 0)
    )), 0) AS total_network_bps
  FROM flat_table
),
bucketed AS (
  SELECT
    *,
    ntile(100) OVER (ORDER BY uptime_s) AS overview_bucket
  FROM samples
)
SELECT
  min(uptime_s) AS start_s,
  max(uptime_s) AS end_s,
  count(*) AS samples,
  round(avg(cpu_total_percent), 2) AS avg_cpu_percent,
  max(cpu_total_percent) AS max_cpu_percent,
  round(avg(iowait_percent), 2) AS avg_iowait_percent,
  max(iowait_percent) AS max_iowait_percent,
  max(procs_running) AS max_procs_running,
  max(procs_blocked) AS max_procs_blocked,
  min(mem_available_kb) AS min_mem_available_kb,
  max(mem_used_percent) AS max_mem_used_percent,
  max(swap_used_kb) AS max_swap_used_kb,
  round(avg(page_faults_per_s), 2) AS avg_page_faults_per_s,
  max(page_faults_per_s) AS max_page_faults_per_s,
  round(avg(pgmajfaults_per_s), 2) AS avg_pgmajfaults_per_s,
  max(pgmajfaults_per_s) AS max_pgmajfaults_per_s,
  round(avg(total_storage_iops), 2) AS avg_storage_iops,
  max(total_storage_iops) AS max_storage_iops,
  round(avg(total_storage_bps), 2) AS avg_storage_bps,
  max(total_storage_bps) AS max_storage_bps,
  round(avg(total_network_bps), 2) AS avg_network_bps,
  max(total_network_bps) AS max_network_bps
FROM bucketed
GROUP BY overview_bucket
ORDER BY overview_bucket;
```

Recordings with no more than 100 samples retain one row per sample. Longer or
higher-frequency recordings return at most 100 time buckets. For interval rates
and percentages, each bucket reports an average and maximum. For sampled state
fields, it reports the extrema selected by the query. Storage and network
activity is aggregated without requiring target-specific names.

## Interpreting the time series

System Utilisation records aggregate host measurements on a shared time axis.
Use timing when it materially affects an insight, such as distinguishing
sustained, transient or recurring behaviour. Describe timing only at the
resolution supported by the samples or overview buckets.

The measurements cannot identify a specific function or process; attribution
requires a more targeted measurement.

## Follow-up queries

After reviewing the initial results, use follow-up queries for material
questions that remain.

A full-resolution time-series query for selected metrics, optionally limited
to a time range, can use this form. For larger results, apply bucketing suited
to the question.

```sql
SELECT
  uptime_s,
  <relevant metrics>
FROM flat_table
WHERE uptime_s BETWEEN <start_s> AND <end_s>
ORDER BY uptime_s;
```

Omit the time predicate when the relevant metrics are small enough to inspect
across the full recording.

### Grouping contiguous samples

When an insight needs sample-level boundaries for ranges that meet a condition,
flag the relevant samples. The following pattern materialises `lag()` before
calculating a cumulative interval number:

```sql
WITH flagged AS (
  SELECT
    uptime_s,
    <relevant metrics>,
    CASE WHEN <condition> THEN 1 ELSE 0 END AS relevant
  FROM flat_table
),
with_previous AS (
  SELECT
    *,
    coalesce(lag(relevant) OVER (ORDER BY uptime_s), 0)
      AS previous_relevant
  FROM flagged
),
numbered AS (
  SELECT
    *,
    sum(
      CASE WHEN relevant = 1 AND previous_relevant = 0 THEN 1 ELSE 0 END
    ) OVER (
      ORDER BY uptime_s
      ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
    ) AS interval_id
  FROM with_previous
)
SELECT
  interval_id,
  min(uptime_s) AS start_s,
  max(uptime_s) AS end_s,
  count(*) AS samples
FROM numbered
WHERE relevant = 1
GROUP BY interval_id
ORDER BY start_s;
```

### Dynamic column families

The following expression aggregates a target-dependent column family. Change
the regular expression and alias for `write_bps`, `read_iops`, `write_iops`,
`rx_bps` or `tx_bps` as needed:

```sql
coalesce(list_sum(list_transform(
  list_value(unpack(COLUMNS('^read_bps_.*$'))),
  lambda x: coalesce(try_cast(x AS DOUBLE), 0)
)), 0) AS total_read_bps
```

Operation rate and bandwidth over the same interval can be used to estimate
bytes per operation. Storage reads may result from paging as well as application
I/O; memory pressure and major faults can help distinguish them. Recorded rates
do not by themselves establish physical-device saturation; that requires a
relevant capacity limit. A Linux block-device name does not reliably identify
the physical storage technology or capacity; virtualised platforms can expose
generic or NVMe-style names for remotely backed storage.

Use the returned DuckDB error to correct a failed query; simplify it if needed.

Stop when the available evidence is sufficient; continue when a material claim
lacks evidence or results conflict.
