// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const TOOL_SYSCALL_TRACE = {
  name: 'syscall-trace',
  version: '1.0.0',
};

const READINESS_MESSAGE_CODE =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const UNSUPPORTED_WORKLOAD_CODE =
  'tool_integrations.syscall_trace.UNSUPPORTED_WORKLOAD';

const SYSCALL_PARQUET_PATH = 'tool/syscall-trace/0/syscalls.parquet';
const FLAT_TABLE_COMPONENT = { name: 'flat_table', schema_version: '1.0' };
const SQL_RENDERER_OUTPUT = {
  name: 'table',
  component_type: FLAT_TABLE_COMPONENT,
};
const SYSCALL_FREQUENCY_HEATMAP_GROUP = 'syscall_frequency';
const SYSCALL_FREQUENCY_HEATMAP_TARGET_BUCKET_COUNT = 100;
const SYSCALL_FREQUENCY_HEATMAP_MIN_BUCKET_US = 1000;
const SYSCALL_FREQUENCY_HEATMAP_ZERO_DURATION_BUCKET_US = 1000000;
const SYSCALL_FREQUENCY_HEATMAP_SYSCALL_LIMIT = 100;
const SUMMARY_METRICS = {
  'Total syscalls': {
    description: 'Total number of syscall events captured for this run.',
  },
  'Failed syscalls': {
    description: 'Number of syscall events that reported an error result.',
  },
  'Failed syscall rate (%)': {
    description: 'Failed syscalls as a percentage of the total syscall count.',
  },
  'Total traced syscall time (ms)': {
    description:
      'Sum of syscall duration data, in milliseconds. This is empty when no duration data is available.',
  },
  'Timed syscall events': {
    description: 'Number of syscall events with duration data available.',
  },
  'Distinct PIDs': {
    description: 'Number of unique process IDs observed in the trace.',
  },
};
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
const recipe = {
  name: 'syscall_trace_summary',
  title: 'Syscall Trace Summary',
  version: '1.0.0',
  api_version: '1.0.0',
  status: 'preview',
  description: 'Summarizes Linux syscall activity collected with strace.',
  mcp_guidance:
    'This recipe supports launch and attach workloads only. Do not run it with the system workload because system-wide syscall tracing is not supported.',
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_SYSCALL_TRACE.name,
          version: TOOL_SYSCALL_TRACE.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [],
  readyStages: [
    {
      name: 'Check Syscall Trace Summary readiness',
      description: 'Check that syscall tracing can run on the target.',
      exec: readySyscallTrace,
    },
  ],
  runStages: [
    {
      name: 'Validate workload type',
      description:
        'Ensure syscall tracing is run for launch or attach workloads.',
      exec: validateWorkloadType,
    },
    {
      name: 'Collect syscall trace data',
      description: 'Collect syscall trace output for the selected workload.',
      exec: runSyscallTrace,
    },
  ],
  renderStages: [
    {
      name: 'Render syscall trace summary',
      description: 'Create syscall trace summary tables from Parquet output.',
      exec: renderSyscallTraceSummary,
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").Workload} workload
 * @returns {boolean}
 */
function isSystemWideWorkload(workload) {
  return workload?.Type === 'systemWide';
}

/**
 * @returns {import("./docs/jsdocs").ToolConfigurationsArg}
 * @param {import("./docs/jsdocs").Workload} workload
 */
function buildSyscallTraceTools(workload) {
  return {
    toolConfigs: [
      {
        name: TOOL_SYSCALL_TRACE.name,
        params: {},
        workload,
        env: {},
      },
    ],
  };
}

/**
 * @param {import("./docs/jsdocs").Workload} workload
 * @returns {import("./docs/jsdocs").RecipeReadyAdvice | undefined}
 */
function unsupportedWorkloadAdvice(workload) {
  if (!isSystemWideWorkload(workload)) {
    return undefined;
  }
  return {
    ToolName: TOOL_SYSCALL_TRACE.name,
    AdviceSeverity: 'error',
    MessageCode: READINESS_MESSAGE_CODE,
    Metadata: {
      message:
        'Syscall Trace Summary supports launch and attach workloads. System-wide tracing is not supported.',
    },
    Cause: '',
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readySyscallTrace(context) {
  const workload = context.getWorkload();
  const workloadAdvice = unsupportedWorkloadAdvice(workload);
  if (workloadAdvice) {
    return {
      status: 'error',
      advice: [workloadAdvice],
    };
  }

  const tools = buildSyscallTraceTools(workload);
  const toolResponses = context.probeTools(tools);
  const allAdvice = collectToolAdvice(tools, toolResponses);
  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function validateWorkloadType(context) {
  if (isSystemWideWorkload(context.getWorkload())) {
    throw {
      code: UNSUPPORTED_WORKLOAD_CODE,
      metadata: { workloadType: 'system-wide' },
    };
  }
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runSyscallTrace(context) {
  context.runTools(buildSyscallTraceTools(context.getWorkload()));
}

function syscallEventsTableSQL() {
  return `read_parquet({{path:${SYSCALL_PARQUET_PATH}}})`;
}

/**
 * @param {string} id
 * @param {string} sql
 * @returns {import("./docs/jsdocs").Renderer}
 */
function makeSQLRenderer(id, sql) {
  return {
    type: 'SQL',
    id,
    config: {
      sql,
      output: SQL_RENDERER_OUTPUT,
    },
  };
}

/**
 * @param {string} id
 * @param {string} rendererId
 * @param {string} title
 * @param {string} description
 * @param {string} [noDataMessage]
 * @returns {import("./docs/jsdocs").Widget}
 */
function makeGrid(id, rendererId, title, description, noDataMessage) {
  return {
    type: 'generic_grid',
    id,
    rendererId,
    title,
    description,
    config: {
      data_source: {
        tables: {
          table: [{ renderer_id: rendererId, output: 'table' }],
        },
      },
      ...(noDataMessage ? { noDataMessage, noDataStatus: 'info' } : {}),
    },
  };
}

/**
 * @param {{ renderer_id: string, output: string }[]} dataSource
 */
function makeSyscallFrequencyHeatmapConfig(dataSource) {
  return {
    xAxisUnit: 's',
    data_source: {
      tables: {
        [SYSCALL_FREQUENCY_HEATMAP_GROUP]: dataSource,
      },
    },
    groups: {
      [SYSCALL_FREQUENCY_HEATMAP_GROUP]: {
        title: 'Syscall Frequency',
        type: 'heatmap',
        index: 0,
        description:
          'Syscall rate by time bucket for the top syscall names observed in this run.',
        config: {
          xAxisTitle: 'Time (s)',
          yAxisTitle: 'Syscalls',
          yAxisUnit: 'syscalls/s',
          customQuery: {
            tableNamePlaceholder: '{table}',
            query:
              'SELECT ' +
              '"time_s", ' +
              '"syscall", ' +
              '"syscalls_per_second" ' +
              'FROM {table} ' +
              'ORDER BY "syscall_order", "time_s"',
          },
          series: [
            {
              type: 'single',
              name: 'Syscall rate',
              xColumn: 'time_s',
              yColumn: 'syscalls_per_second',
            },
          ],
        },
      },
    },
  };
}

/**
 * @param {string} id
 * @param {string} rendererId
 * @param {string} heatmapRendererId
 * @param {string} title
 * @param {string} description
 * @returns {import("./docs/jsdocs").Widget}
 */
function makeSyscallTraceSummary(
  id,
  rendererId,
  heatmapRendererId,
  title,
  description,
) {
  const heatmapDataSource = [
    { renderer_id: heatmapRendererId, output: 'table' },
  ];

  return {
    type: 'syscall_trace_summary',
    id,
    rendererId,
    title,
    description,
    config: {
      data_source: {
        tables: {
          table: [{ renderer_id: rendererId, output: 'table' }],
          [SYSCALL_FREQUENCY_HEATMAP_GROUP]: heatmapDataSource,
        },
      },
      summaryMetrics: SUMMARY_METRICS,
      heatmap: makeSyscallFrequencyHeatmapConfig(heatmapDataSource),
    },
  };
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 */
function renderSyscallTraceSummary(context) {
  const events = syscallEventsTableSQL();
  const summaryMetricsRenderer = makeSQLRenderer(
    'syscall_summary_metrics',
    `WITH totals AS (
      SELECT
        COUNT(*) AS total_syscalls,
        COUNT(*) FILTER (WHERE errno IS NOT NULL AND errno <> '') AS failed_syscalls,
        COUNT(duration_us) AS timed_syscalls,
        COUNT(DISTINCT pid) AS distinct_pids,
        SUM(duration_us) AS total_duration_us
      FROM ${events}
    )
    SELECT
      'Total syscalls' AS Metric,
      CAST(total_syscalls AS VARCHAR) AS Value
    FROM totals
    UNION ALL
    SELECT
      'Failed syscalls',
      CAST(failed_syscalls AS VARCHAR)
    FROM totals
    UNION ALL
    SELECT
      'Failed syscall rate (%)',
      printf('%.2f', CASE WHEN total_syscalls = 0 THEN 0 ELSE failed_syscalls * 100.0 / total_syscalls END)
    FROM totals
    UNION ALL
    SELECT
      'Total traced syscall time (ms)',
      CASE WHEN timed_syscalls = 0 THEN NULL ELSE printf('%.3f', total_duration_us / 1000.0) END
    FROM totals
    UNION ALL
    SELECT
      'Timed syscall events',
      CAST(timed_syscalls AS VARCHAR)
    FROM totals
    UNION ALL
    SELECT
      'Distinct PIDs',
      CAST(distinct_pids AS VARCHAR)
    FROM totals`,
  );
  const syscallFrequencyHeatmapRenderer = makeSQLRenderer(
    'syscall_frequency_heatmap',
    `WITH syscall_events AS (
      SELECT
        ts_utc,
        syscall
      FROM ${events}
      WHERE ts_utc IS NOT NULL
        AND syscall IS NOT NULL
        AND syscall <> ''
    ),
    event_bounds AS (
      SELECT
        MIN(ts_utc) AS start_ts,
        MAX(ts_utc) AS end_ts,
        COALESCE(date_diff('microsecond', MIN(ts_utc), MAX(ts_utc)), 0)::DOUBLE AS span_us,
        ${SYSCALL_FREQUENCY_HEATMAP_TARGET_BUCKET_COUNT}::DOUBLE AS target_bucket_count
      FROM syscall_events
    ),
    bucket_size AS (
      SELECT
        start_ts,
        end_ts,
        span_us,
        CASE
          WHEN span_us <= 0 THEN ${SYSCALL_FREQUENCY_HEATMAP_ZERO_DURATION_BUCKET_US}.0
          ELSE GREATEST(
            span_us / target_bucket_count,
            ${SYSCALL_FREQUENCY_HEATMAP_MIN_BUCKET_US}.0
          )
        END AS bucket_us
      FROM event_bounds
    ),
    syscall_totals AS (
      SELECT
        syscall,
        COUNT(*) AS total_syscalls
      FROM syscall_events
      GROUP BY syscall
    ),
    selected_syscalls AS (
      SELECT syscall
      FROM syscall_totals
      ORDER BY total_syscalls DESC, syscall ASC
      LIMIT ${SYSCALL_FREQUENCY_HEATMAP_SYSCALL_LIMIT}
    ),
    ordered_syscalls AS (
      SELECT
        syscall,
        ROW_NUMBER() OVER (ORDER BY syscall ASC) AS syscall_order
      FROM selected_syscalls
    ),
    buckets AS (
      SELECT
        bucket_index,
        bucket_us,
        ROUND(bucket_index * bucket_us / 1000000.0, 6) AS time_s
      FROM bucket_size,
        range(
          0,
          CASE
            WHEN span_us <= 0 THEN 1
            ELSE FLOOR(span_us / bucket_us)::BIGINT + 1
          END
        ) AS bucket(bucket_index)
    ),
    event_buckets AS (
      SELECT
        CAST(
          FLOOR(
            date_diff('microsecond', bucket_size.start_ts, syscall_events.ts_utc)::DOUBLE
              / bucket_size.bucket_us
          ) AS BIGINT
        ) AS bucket_index,
        syscall_events.syscall,
        COUNT(*) AS syscall_count
      FROM syscall_events
      CROSS JOIN bucket_size
      JOIN ordered_syscalls ON ordered_syscalls.syscall = syscall_events.syscall
      GROUP BY bucket_index, syscall_events.syscall
    ),
    dense_counts AS (
      SELECT
        buckets.time_s,
        buckets.bucket_us,
        ordered_syscalls.syscall,
        ordered_syscalls.syscall_order,
        COALESCE(event_buckets.syscall_count, 0) AS syscall_count
      FROM buckets
      CROSS JOIN ordered_syscalls
      LEFT JOIN event_buckets
        ON event_buckets.bucket_index = buckets.bucket_index
        AND event_buckets.syscall = ordered_syscalls.syscall
    )
    SELECT
      time_s,
      syscall,
      syscall_order,
      CAST(syscall_count AS DOUBLE) * 1000000.0 / bucket_us AS syscalls_per_second
    FROM dense_counts
    ORDER BY syscall_order, time_s`,
  );
  const topSyscallsByCountRenderer = makeSQLRenderer(
    'syscall_top_syscalls_by_count',
    `WITH totals AS (
      SELECT COUNT(*) AS total_syscalls
      FROM ${events}
    )
    SELECT
      syscall AS "Syscall",
      COUNT(*) AS "Count",
      ROUND(COUNT(*) * 100.0 / NULLIF(total_syscalls, 0), 2) AS "Share (%)",
      COUNT(*) FILTER (WHERE errno IS NOT NULL AND errno <> '') AS "Failures",
      ROUND(
        COUNT(*) FILTER (WHERE errno IS NOT NULL AND errno <> '') * 100.0 / NULLIF(COUNT(*), 0),
        2
      ) AS "Failure rate (%)",
      COUNT(duration_us) AS "Timed events",
      CASE
        WHEN COUNT(duration_us) = 0 THEN NULL
        ELSE ROUND(SUM(duration_us) / 1000.0, 3)
      END AS "Total time (ms)",
      ROUND(AVG(duration_us) / 1000.0, 3) AS "Average duration (ms)",
      ROUND(MAX(duration_us) / 1000.0, 3) AS "Max duration (ms)"
    FROM ${events}
    CROSS JOIN totals
    GROUP BY syscall, total_syscalls
    ORDER BY "Count" DESC, "Total time (ms)" DESC, "Syscall" ASC
    LIMIT 100`,
  );
  const slowestSyscallEventsRenderer = makeSQLRenderer(
    'syscall_slowest_syscall_events',
    `SELECT
      strftime(ts_utc AT TIME ZONE 'UTC', '%H:%M:%S.%f') AS "Timestamp",
      pid AS "PID",
      syscall AS "Syscall",
      ROUND(duration_us / 1000.0, 3) AS "Duration (ms)",
      result AS "Result",
      errno AS "Errno",
      args AS "Args"
    FROM ${events}
    WHERE duration_us IS NOT NULL
    ORDER BY duration_us DESC, ts_utc ASC
    LIMIT 100`,
  );
  const syscallTimeBySyscallRenderer = makeSQLRenderer(
    'syscall_time_by_syscall',
    `WITH syscall_times AS (
      SELECT
        syscall,
        COUNT(*) AS timed_events,
        SUM(duration_us) AS total_duration_us,
        AVG(duration_us) AS average_duration_us,
        MAX(duration_us) AS max_duration_us
      FROM ${events}
      WHERE duration_us IS NOT NULL
      GROUP BY syscall
    )
    SELECT
      syscall AS "Syscall",
      timed_events AS "Timed events",
      ROUND(total_duration_us / 1000.0, 3) AS "Total time (ms)",
      ROUND(
        total_duration_us * 100.0 / NULLIF(SUM(total_duration_us) OVER (), 0),
        2
      ) AS "Share of traced time (%)",
      ROUND(average_duration_us / 1000.0, 3) AS "Average duration (ms)",
      ROUND(max_duration_us / 1000.0, 3) AS "Max duration (ms)"
    FROM syscall_times
    ORDER BY "Total time (ms)" DESC, "Timed events" DESC, "Syscall" ASC
    LIMIT 100`,
  );
  const failuresBySyscallRenderer = makeSQLRenderer(
    'syscall_failures_by_syscall',
    `WITH failures AS (
      SELECT
        syscall,
        COUNT(*) AS failures
      FROM ${events}
      WHERE errno IS NOT NULL AND errno <> ''
      GROUP BY syscall
    ),
    syscall_totals AS (
      SELECT
        syscall,
        COUNT(*) AS total_syscalls
      FROM ${events}
      GROUP BY syscall
    ),
    failure_totals AS (
      SELECT SUM(failures) AS total_failures
      FROM failures
    )
    SELECT
      failures.syscall AS "Syscall",
      failures.failures AS "Failures",
      ROUND(
        failures.failures * 100.0 / NULLIF(failure_totals.total_failures, 0),
        2
      ) AS "Share of failures (%)",
      syscall_totals.total_syscalls AS "Total syscalls",
      ROUND(
        failures.failures * 100.0 / NULLIF(syscall_totals.total_syscalls, 0),
        2
      ) AS "Failure rate (%)"
    FROM failures
    JOIN syscall_totals ON syscall_totals.syscall = failures.syscall
    CROSS JOIN failure_totals
    ORDER BY "Failures" DESC, "Failure rate (%)" DESC, "Syscall" ASC
    LIMIT 100`,
  );
  const syscallCountAndTimeByPidRenderer = makeSQLRenderer(
    'syscall_count_and_time_by_pid',
    `WITH totals AS (
      SELECT COUNT(*) AS total_syscalls
      FROM ${events}
    )
    SELECT
      pid AS "PID",
      COUNT(*) AS "Syscalls",
      ROUND(COUNT(*) * 100.0 / NULLIF(total_syscalls, 0), 2) AS "Share (%)",
      COUNT(*) FILTER (WHERE errno IS NOT NULL AND errno <> '') AS "Failures",
      ROUND(
        COUNT(*) FILTER (WHERE errno IS NOT NULL AND errno <> '') * 100.0 / NULLIF(COUNT(*), 0),
        2
      ) AS "Failure rate (%)",
      COUNT(duration_us) AS "Timed events",
      CASE
        WHEN COUNT(duration_us) = 0 THEN NULL
        ELSE ROUND(SUM(duration_us) / 1000.0, 3)
      END AS "Total time (ms)",
      ROUND(AVG(duration_us) / 1000.0, 3) AS "Average duration (ms)",
      ROUND(MAX(duration_us) / 1000.0, 3) AS "Max duration (ms)",
      COUNT(DISTINCT syscall) AS "Distinct syscalls"
    FROM ${events}
    CROSS JOIN totals
    GROUP BY pid, total_syscalls
    ORDER BY "Syscalls" DESC, "Total time (ms)" DESC, "PID" ASC
    LIMIT 100`,
  );

  return {
    renderers: [
      summaryMetricsRenderer,
      syscallFrequencyHeatmapRenderer,
      topSyscallsByCountRenderer,
      slowestSyscallEventsRenderer,
      syscallTimeBySyscallRenderer,
      failuresBySyscallRenderer,
      syscallCountAndTimeByPidRenderer,
    ],
    visualizations: [
      makeSyscallTraceSummary(
        'syscall_summary_metrics_summary',
        'syscall_summary_metrics',
        'syscall_frequency_heatmap',
        'Summary Metrics',
        'High-level syscall counts, failure rate, traced duration, and process coverage.',
      ),
      makeGrid(
        'syscall_top_syscalls_by_count_grid',
        'syscall_top_syscalls_by_count',
        'Top Syscalls by Count',
        'Top 100 syscalls by call count, with failure and duration totals.',
        'No syscall events were captured for this run.',
      ),
      makeGrid(
        'syscall_slowest_syscall_events_grid',
        'syscall_slowest_syscall_events',
        'Slowest Syscall Events',
        'Top 100 slowest syscall events with duration data.',
        'No syscall events with duration data were captured for this run.',
      ),
      makeGrid(
        'syscall_time_by_syscall_grid',
        'syscall_time_by_syscall',
        'Top Syscalls by Time',
        'Top 100 syscalls by total traced time, with average and maximum duration.',
        'No timed syscall events were captured for this run.',
      ),
      makeGrid(
        'syscall_failures_by_syscall_grid',
        'syscall_failures_by_syscall',
        'Top Failing Syscalls',
        'Top 100 syscalls by failure count, with failure rate and total call count.',
        'No failed syscall events were captured for this run.',
      ),
      makeGrid(
        'syscall_count_and_time_by_pid_grid',
        'syscall_count_and_time_by_pid',
        'Top PIDs by Syscall Count',
        'Top 100 PIDs by syscall count, with failure rate and duration totals.',
        'No syscall events were captured for this run.',
      ),
    ],
  };
}
