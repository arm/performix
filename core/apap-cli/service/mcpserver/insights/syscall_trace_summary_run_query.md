# Syscall Trace Query Guide

Use `run_query` to inspect the Syscall Trace summary tables and Syscall Frequency heatmap. Start with aggregate and timeline overviews, then narrow the queries to evidence that can materially affect the conclusion.

## Available tables

The descriptive renderer names are not DuckDB table names. Use the unqualified queryable names exactly as shown; do not reuse generated schema qualifiers discovered during another `run_query` call because each call creates a new render.

| Descriptive renderer name | Queryable table | Contents |
| --- | --- | --- |
| `syscall_summary_metrics` | `flat_table` | Run-wide syscall count, failure rate, traced duration, timed-event count and distinct PID count as `Metric`/`Value` rows. |
| `syscall_frequency_heatmap` | `flat_table_1` | Syscall rate in precomputed relative `time_s` buckets for up to 100 syscall names selected by run-wide count. |
| `syscall_top_syscalls_by_count` | `flat_table_2` | Up to 100 syscalls ranked by run-wide count, with share, failures and duration statistics. |
| `syscall_slowest_syscall_events` | `flat_table_3` | Up to 100 globally slowest timed syscall events, including timestamp, PID, result, errno and arguments. |
| `syscall_time_by_syscall` | `flat_table_4` | Up to 100 syscalls ranked by total traced time, with average and maximum duration. |
| `syscall_failures_by_syscall` | `flat_table_5` | Up to 100 syscalls ranked by failure count, with failure share and rate. |
| `syscall_count_and_time_by_pid` | `flat_table_6` | Up to 100 PIDs ranked by syscall count, with failures, duration and distinct syscall count. |

## Analysis workflow

Inspect the summary metrics in `flat_table` and the aggregate tables first. Start with the highest-count and highest-time syscalls, then check failures or PID distribution when those dimensions could affect the diagnosis. Select only useful columns and use bounded results. These table queries are independent and can run concurrently with the timeline overview.

The aggregate tables establish whether activity is material and whether it is driven by count, duration, failures or particular PIDs. The timeline establishes when and how that activity occurs. Always interpret them together: a timeline burst can be insignificant in the run-wide totals, while a large aggregate can represent sustained activity, periodic bursts, startup noise or legitimate sparse waiting.

## Aggregate overviews

Issue these independent queries together with the timeline overview. Use the exact quoted column names shown rather than guessing names from their descriptions.

```sql
SELECT "Metric", "Value"
FROM flat_table
ORDER BY "Metric"
```

```sql
SELECT
  "Syscall",
  "Count",
  "Share (%)",
  "Failures",
  "Failure rate (%)",
  "Timed events",
  "Total time (ms)",
  "Average duration (ms)",
  "Max duration (ms)"
FROM flat_table_2
ORDER BY "Count" DESC
LIMIT 20
```

```sql
SELECT
  "Syscall",
  "Timed events",
  "Total time (ms)",
  "Share of traced time (%)",
  "Average duration (ms)",
  "Max duration (ms)"
FROM flat_table_4
ORDER BY "Total time (ms)" DESC
LIMIT 20
```

## Timeline overview

Collapse the heatmap to one row per time bucket to locate bursts, quiet periods and changes in the dominant selected syscall before selecting a time range or syscall for follow-up:

```sql
WITH bucket_summary AS (
  SELECT
    time_s,
    SUM(syscalls_per_second) AS selected_total_rate,
    COUNT(*) FILTER (WHERE syscalls_per_second > 0)
      AS active_selected_syscalls,
    arg_max(syscall, syscalls_per_second)
      FILTER (WHERE syscalls_per_second > 0)
      AS dominant_selected_syscall,
    MAX(syscalls_per_second) AS dominant_selected_rate
  FROM flat_table_1
  GROUP BY time_s
)
SELECT
  time_s,
  ROUND(selected_total_rate, 2) AS selected_syscalls_per_second,
  active_selected_syscalls,
  dominant_selected_syscall,
  ROUND(dominant_selected_rate, 2)
    AS dominant_selected_syscalls_per_second
FROM bucket_summary
ORDER BY time_s
```

`flat_table_1` is already materialised with up to 100 syscall names selected by run-wide count and approximately 100 precomputed time buckets over the interval from the first to the last traced syscall. The overview query performs no further time bucketing; it returns one row for every precomputed bucket and describes only the selected syscall names, not all traced syscalls.

The heatmap bins syscall start timestamps and does not represent time spent inside each call. A long blocking call contributes one event to its starting bucket, and a quiet bucket means no selected syscall starts, not necessarily that the workload was idle. Use the overview to distinguish sustained, recurring, bursty and sparse activity, then compare candidate patterns with their count, time, failure and PID totals.

Inspect the ordered overview across the full recorded interval before concluding that an aggregate leader is sustained. Do not reduce the timeline to only its first and last timestamps or number of active buckets: that hides changes within the run. Treat active regions separated by quiet buckets as separate bursts, compare their rates and syscall mix, and report approximate phase boundaries when behaviour changes. A material rate change can indicate a new phase even when the same syscall remains dominant.

Before producing insights, classify each material aggregate or timeline pattern as sustained, periodic, bursty or sparse. Explicitly state any dense phases, quiet gaps or transitions that affect the diagnosis. If the overview suggests multiple phases, use focused queries on representative intervals from each phase rather than querying one interval and generalising it to the whole run.

## Focused timeline query

Filter the timeline to a material interval and syscall set:

```sql
SELECT
  time_s,
  syscall,
  ROUND(syscalls_per_second, 2) AS syscalls_per_second
FROM flat_table_1
WHERE time_s BETWEEN <start_s> AND <end_s>
  AND syscall IN (<quoted syscall names>)
  AND syscalls_per_second > 0
ORDER BY time_s, syscalls_per_second DESC
```

Choose the interval from the overview and the syscall set from both the timeline and aggregate leaders. Query the selected names separately in the relevant aggregate tables and interpret those results with the focused timeline. The heatmap is already bucketed, so filtering it narrows the result but does not increase its time resolution. Use `flat_table_3` for event-level duration, result, errno or argument context when that evidence could change the interpretation. Use `flat_table_6` when attribution to one or many processes matters.

## Optional detail queries

Use these exact templates only when failures, process attribution or individual slow events could affect the diagnosis.

```sql
SELECT
  "Syscall",
  "Failures",
  "Share of failures (%)",
  "Total syscalls",
  "Failure rate (%)"
FROM flat_table_5
ORDER BY "Failures" DESC
LIMIT 20
```

```sql
SELECT
  "PID",
  "Syscalls",
  "Share (%)",
  "Failures",
  "Failure rate (%)",
  "Timed events",
  "Total time (ms)",
  "Average duration (ms)",
  "Max duration (ms)",
  "Distinct syscalls"
FROM flat_table_6
ORDER BY "Syscalls" DESC
LIMIT 20
```

```sql
SELECT
  "Timestamp",
  "PID",
  "Syscall",
  "Duration (ms)",
  "Result",
  "Errno",
  "Args"
FROM flat_table_3
ORDER BY "Duration (ms)" DESC
LIMIT 20
```

## Interpreting counts, time and failures

- High count and high duration are different signals. A frequent fast syscall can indicate avoidable call overhead; an infrequent long syscall can indicate intentional blocking.
- Syscall duration is elapsed time from syscall entry to return, not CPU time. Blocking calls such as `poll`, `read`, `futex` or waits may dominate traced time because the application is efficiently idle. Use call count, cadence, results and surrounding activity before recommending a change.
- Durations from concurrent threads and processes can overlap, so summed traced time may not match wall-clock duration.
- A failed syscall is not automatically a performance or correctness problem. Errors such as `ENOENT`, `EAGAIN` or `EINTR` can be expected control flow. Use failure rate, recurrence and available errno or argument evidence before deciding that failures are avoidable or harmful.
- A large distinct-PID count can reflect intentional child processes. Treat it as process-churn evidence only when syscall and timeline data show recurring creation, execution and wait activity.
- Startup, dynamic-loader and teardown syscalls commonly add low-volume noise. Prioritise repeated or materially expensive patterns during the workload interval.

Use the returned DuckDB error to correct a failed query. If `run_query` reports truncation, reduce the selected columns, row limit or syscall set rather than drawing a conclusion from incomplete rows.

Stop when the available evidence supports the material findings. It is valid to conclude that prominent syscall time is legitimate waiting or that the recording does not justify an optimisation.
