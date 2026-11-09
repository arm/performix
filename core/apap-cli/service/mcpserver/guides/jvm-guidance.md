# JVM Analysis Guide

Read this guide before interpreting JVM data or recommending JVM changes.
Use `run_query` with the opened render session's `session_id`. If schemas were
omitted, use `table_schemas_query` to discover the columns.

## Java Flight Recorder evidence

Find the JFR relations in the render session's `table_schemas` by their columns:
the recording index has `Source JFR` and `Parse Complete`; garbage collections
have `GC Cause` and `Total Pause (ns)`; heap summaries have `GC Phase` and
`Used (bytes)`. Use each relation's physical `name` in SQL. Without a recording
index, do not claim JFR evidence.

From the recording index, read `"Recording"`, `"PID"`, `"Recording Start (epoch ns)"`,
`"Recording End (epoch ns)"`, `"Parse Complete"`, and `"Parse Error"`. Select a
parsed recording matching the workload PID and sample window. Filter later JFR
queries by `"Recording"`. Check process or thread tables if PID is unclear.
Limit trend claims to the recording window.

### JVM Internals

For the selected recording, check the JVM version, arguments, properties and
flags relevant to the measured problem. Find their tables from the supplied
schemas. Use rows with `Recording` so settings from different JVMs are kept
separate; the UI summary does not identify the recording on each flag row.

Use the latest captured value for each flag. For example, check heap sizes
and G1 selection with:

```sql
SELECT "Flag", "Value", "Origin", "Type", "Event Start (epoch ns)"
FROM <jvm_flags_table>
WHERE "Recording" = <recording_id>
  AND "Flag" IN ('InitialHeapSize', 'MaxHeapSize', 'UseG1GC')
QUALIFY ROW_NUMBER() OVER (
  PARTITION BY "Recording", "Flag" ORDER BY "Event Start (epoch ns)" DESC
) = 1
ORDER BY "Flag"
```

Arguments show requested settings; captured flags show values actually used.
Missing settings are unknown. Properties describe startup values. Read flag
values using their type and units; automatic values do not establish CPU or
memory limits.

When a setting helps explain measured cost, suggest a specific change and
explain why it could help. Check available host or container resource limits
before suggesting changes such as a larger heap or more threads. If limits are
unknown, make the advice conditional on sufficient resources.

Recommend an upgrade only when a reliable source describes an improvement
relevant to the recorded JVM vendor/version, target architecture and measured
work. Identify which versions include the improvement; age alone is not enough.

### Heap and garbage collection

When investigating heap growth or retention, use the heap query below with the
correct table name and recording ID from the supplied schema and recording
index. The query returns
the first and last few _after-GC_ heap samples; `"After GC"` is a value in
`"GC Phase"`, not a column:

```sql
WITH h AS (
  SELECT "Event Start (epoch ns)" AS event_ns, "GC ID" AS gc_id,
         "Used (bytes)" AS used_bytes,
         ROW_NUMBER() OVER (ORDER BY "Event Start (epoch ns)", "GC ID") AS n,
         COUNT(*) OVER () AS total
  FROM <jvm_heap_summary_table>
  WHERE "Recording" = <recording_id> AND "GC Phase" = 'After GC'
)
SELECT event_ns, gc_id, ROUND(used_bytes / 1048576.0, 2) AS used_mib
FROM h WHERE n <= 8 OR n > total - 8
ORDER BY event_ns, gc_id
```

Report after-GC values if relevant to the finding, in time order: say
whether they rise, level off, or keep rising. Check source to distinguish
expected growth from unnecessary retention; suggest a fix only when supported.
The retaining code need not be a CPU hotspot.
JFR cannot identify the owner. Small pauses
do not erase a heap trend.

Check GC even when CPU work dominates. Group the garbage-collection relation by
`"GC Cause"` and `"GC Name"`; count events and sum `"Total Pause (ns)"`.
`"Duration (ns)"` is not pause time. `System.gc()` shows an explicit request,
not its caller; name a caller only from CPU samples or source. Base advice on
measured pause time or CPU cost.

For allocation findings, use CPU samples to measure cost and sampled paths or
source to identify likely application work. Stop querying once the evidence
supports the finding.
If after-GC samples end early, report that limit; do not infer a flat heap
afterward. Before-GC growth alone is not a leak. JFR cannot name the allocator.
