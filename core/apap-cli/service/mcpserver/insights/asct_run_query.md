# ASCT Query Guide

Use `run_query` to inspect the ASCT system information and benchmark results for
the supplied run. ASCT characterizes the platform. It does not profile an
application. Do not infer an application bottleneck from these microbenchmarks
alone. Follow the user's requested scope: when they ask about system information
or a specific benchmark family, focus the response on that evidence. Query other
benchmark families only when they provide a material cross-check, and do not let
them displace the requested analysis. For a general ASCT analysis, use system
context to interpret the available measurements and consider all available
benchmark families before prioritizing material findings.

## Resolve the available tables

Do not hard-code rendered table names. Start with:

```json
{
  "run_id": "<run_id>",
  "include_resolved_tables": true,
  "sql": "SELECT 1"
}
```

Use the first table at each returned `resolved_tables` path to replace the
corresponding `<..._table>` placeholder:

- System information: `asct_system_info_table.systemInformation`.
- Idle-latency matrix: `asct_analysis.numaLatencyMatrix`.
- Cross-NUMA-bandwidth matrix: `asct_analysis.numaBandwidthMatrix`.
- Peak bandwidth: `asct_analysis.peakBandwidthGrid`.
- Core-to-core latency: `asct_analysis.coreToCoreLatencyHeatmap`.
- Loaded latency: `asct_analysis.loadedLatencyGrid` or
  `asct_analysis.loadedLatencyLineChart`.
- Latency-sweep summary: `asct_analysis.latencySweepGrid`.
- Bandwidth-sweep summary: `asct_analysis.bandwidthSweepGrid`.
- Detailed latency sweep: `asct_analysis.latencySweepLineChart`.
- Detailed bandwidth sweep: `asct_analysis.bandwidthSweepLineChart`.

Use only table names returned by the current run's mapping. Never infer or query
a `flat_table` name from its numbering. Use recipe parameters to interpret absent
output. Describe an explicitly disabled benchmark as not collected. Report an
explicitly requested benchmark with no mapping or usable rows as a collection
gap. Otherwise say it is unavailable without assuming why. If supporting
metadata required for interpretation is missing, state the limitation and skip
dependent calculations. Leave `include_resolved_tables` disabled on subsequent
queries.

## System context

Query all available system information. All values are strings:

```sql
SELECT "key", "value"
FROM <system_info_table>
ORDER BY "key"
```

`memory.peak_theoretical_bw` is bytes per second. Treat missing information as
unavailable, not zero. If `report.run_as_root` is false or absent, privileged
platform details may be incomplete. For a system-information request, prioritize
the available CPU model and count, operating system and kernel, total memory and
memory configuration, socket and NUMA topology, ASCT version, and collection
privilege context. Include other firmware, mitigation or performance-feature
fields when they materially help characterize the platform. Do not substitute
benchmark conclusions or tuning recommendations for the requested system
characterization. A field need only be described as unavailable when it is
material to the requested analysis; do not enumerate every absent field.

## Latency and bandwidth sweeps

The compact summary tables provide ASCT's level labels, selected working-set
sizes and representative measurements:

```sql
SELECT
  column0 AS level,
  "Lower Bound" AS lower_bound_bytes,
  "Upper Bound" AS upper_bound_bytes,
  "Optimum Datasize" AS optimum_size_bytes,
  "Latency [ns]" AS latency_ns
FROM <latency_sweep_summary_table>
ORDER BY
  CASE column0
    WHEN 'L1' THEN 1
    WHEN 'L2' THEN 2
    WHEN 'LLC' THEN 3
    WHEN 'DRAM' THEN 4
    ELSE 5
  END
```

```sql
SELECT
  "Level" AS level,
  "Datasize Used" AS data_size_bytes,
  "Bandwidth [GB/s]" AS bandwidth_gbps
FROM <bandwidth_sweep_summary_table>
ORDER BY "Datasize Used"
```

An ASCT summary row's selected or optimum data size is a representative
working-set point, not a hardware cache capacity or exact cache boundary.

Use the detailed curve when explaining a transition. Substitute the table and
value metric below:

```sql
WITH curve AS (
  SELECT
    row_key,
    max(CASE WHEN metric = 'sizes'
      THEN try_cast(value AS DOUBLE) END) AS size_bytes,
    max(CASE WHEN metric = '<value_metric>'
      THEN try_cast(value AS DOUBLE) END) AS measurement
  FROM <detailed_sweep_table>
  WHERE metric IN ('sizes', '<value_metric>')
  GROUP BY row_key
)
SELECT size_bytes, measurement
FROM curve
WHERE size_bytes IS NOT NULL
  AND measurement IS NOT NULL
ORDER BY size_bytes
```

- Latency: `<detailed_latency_sweep_table>` and `average_latency_ns` (ns).
- Bandwidth: `<detailed_bandwidth_sweep_table>` and
  `total_bandwidth_mbps` (MB/s).

Latency should generally rise and bandwidth fall as working sets move from
caches to DRAM, but system activity, sharing, frequency, firmware and
virtualization can shift transitions.

Describe sweep transitions with concrete measured size/value pairs from the
curve. Do not invent working-set ranges or average points into cache regions
unless the queried data defines those regions. Keep units clear, and treat cache
boundaries as approximate.

Bandwidth-sweep results are single-core measurements. Cross-NUMA and peak
bandwidth are aggregate multi-core measurements, so avoid drawing conclusions
from direct numerical comparisons between them. Compare bandwidth-sweep results
with published platform figures only when the measurement scope, including core
count, CPU, frequency, method and units are comparable.

## NUMA matrices

Matrix columns depend on the number of NUMA nodes. Run this template for the
idle-latency and cross-NUMA-bandwidth mappings rather than hard-coding node
columns:

```sql
WITH matrix AS (
  UNPIVOT <numa_matrix_table>
  ON COLUMNS(* EXCLUDE (column0))
  INTO NAME memory_node VALUE measurement
)
SELECT
  column0 AS cpu_node,
  memory_node,
  measurement,
  CASE
    WHEN column0 = memory_node THEN 'local'
    ELSE 'remote'
  END AS locality
FROM matrix
ORDER BY cpu_node, memory_node
```

Idle latency is in ns and cross-NUMA bandwidth is in GB/s. Compare local and
remote entries and directional symmetry. Do not claim a remote-NUMA cost for a
single-node system, and skip cross-NUMA aggregate checks when fewer than two
NUMA nodes are reported.

On a multi-node system, sum local cross-NUMA bandwidth without naming dynamic
node columns:

```sql
WITH matrix AS (
  UNPIVOT <numa_bandwidth_table>
  ON COLUMNS(* EXCLUDE (column0))
  INTO NAME memory_node VALUE bandwidth_gbps
)
SELECT sum(try_cast(bandwidth_gbps AS DOUBLE)) AS total_local_bandwidth_gbps
FROM matrix
WHERE column0 = memory_node
```

## Peak bandwidth

```sql
SELECT * EXCLUDE (column0)
FROM <peak_bandwidth_table>
ORDER BY "Peak BW [GB/s]" DESC
```

Report every available traffic row. For each row, include its traffic type,
absolute bandwidth in GB/s and ASCT's `% of Peak Theoretical` value when that
column is present. That percentage uses ASCT's theoretical reference; it is not
the relative difference from the All Reads row. Derived differences may be
supplemental, but must not replace the provided percentages. If the percentage
column is absent, report absolute bandwidth and do not reconstruct it.
`memory.peak_theoretical_bw` is recorded system information, not a value to
infer from a measured row and percentage. You may report it when present and
clearly attribute it to the run; do not reconstruct it when absent. Likewise,
support any system-configuration detail or cross-benchmark comparison with
queried evidence from the current run. Treat provided percentages below 60% as
a reason to investigate configuration, frequency, memory population and
background load, not proof of a defect.

## Loaded latency

Inspect the full loaded-latency curve:

```sql
SELECT * EXCLUDE (column0)
FROM <loaded_latency_table>
ORDER BY "Bandwidth [GB/s]"
```

Use ASCT's `% of Peak Theoretical BW` column when present. Do not derive it when
the column is absent. Explicitly state that more injected NOPs mean less
background traffic and that the highest-NOP sample is the low-load baseline.
Compare it with latency-sweep DRAM when available, and claim a saturation region
only when the trend across adjacent samples supports it. Describe a transition
as an observed range bounded by measured samples. Do not convert it into a
recommended operating limit, utilization target or universal percentage
threshold.

## Core-to-core latency

Summarize the measurements by the node of `CPUA` and the NUMA node to which the
benchmark memory was bound:

```sql
SELECT
  "CPUA_NODE" AS cpua_node,
  "MEMBIND_NODE" AS memory_node,
  count(*) AS measured_pairs,
  round(median("LATENCY"), 2) AS median_latency_ns,
  round(avg("LATENCY"), 2) AS mean_latency_ns,
  round(max("LATENCY"), 2) AS max_latency_ns
FROM <c2c_latency_table>
GROUP BY "CPUA_NODE", "MEMBIND_NODE"
ORDER BY cpua_node, memory_node
```

Inspect the highest-latency pairs when an aggregate shows a material spread:

```sql
SELECT
  "CPUA" AS source_cpu,
  "CPUB" AS target_cpu,
  "CPUA_NODE" AS source_node,
  "MEMBIND_NODE" AS memory_node,
  "LATENCY" AS latency_ns
FROM <c2c_latency_table>
ORDER BY "LATENCY" DESC
LIMIT 20
```

`CPUA_NODE` identifies the source CPU's node. `MEMBIND_NODE` identifies the
benchmark memory-binding node, not the node of `CPUB`. Use a CPU-to-node mapping
or other explicit topology evidence before assigning `CPUB` to a node. When
making that assignment, state the separate topology evidence used, such as a
`sys_hw.numa_nodes.<node>` system-information entry that contains the target CPU.
Do not infer `CPUB` locality from `CPUA_NODE` or `MEMBIND_NODE` alone. Treat
directional asymmetry as meaningful only when both directions were measured
under comparable memory binding.

## Cross-checks and interpretation

When both measurements are available, check whether:

- Latency-sweep DRAM latency is reasonably close to the diagonal
  same-node idle-latency measurements.
- The sizes of latency transitions and bandwidth drops broadly align.
- The loaded-latency highest-NOP point is reasonably close to the
  latency-sweep DRAM result.
- On a multi-node system, the dynamic local cross-NUMA bandwidth sum is
  comparable to peak All Reads bandwidth.

These are approximate comparisons. Quantify differences and account for core
counts, traffic patterns and load conditions before calling one anomalous.

Keep every result bounded with projections, predicates, aggregation or
`LIMIT`. If a column is unclear, inspect a small sample. If `run_query` reports
truncation, narrow the query rather than drawing a conclusion from incomplete
rows. Stop when the evidence is sufficient, and explicitly report unavailable
benchmarks rather than substituting assumptions.
