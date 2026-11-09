// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Adapt scheduler events to the viewer's canonical trace tables. Tracks are
// data-driven, so their identities are assigned in SQL rather than in recipe
// configuration (which cannot inspect Parquet rows).
const ROOT = 'tool/neoprof/0/output/parquet/';
const FILES = {
  switches: `${ROOT}timeline/scheduler_context_switch.parquet`,
  capture: `${ROOT}metadata/capture_metadata.json`,
  threads: `${ROOT}metadata/threads.json`,
  processes: `${ROOT}metadata/processes.json`,
};

// Neoprof reserves these UTIDs for idle and system-wide/unknown execution.
// Keep UINT64_MAX as text: a JavaScript Number would round it before SQL sees it.
const IDLE_THREAD_ID = 0;
const UNKNOWN_THREAD_ID = '18446744073709551615';

// Each physical interval reserves one positive event ID for each lane type.
const EVENT_IDS_PER_INTERVAL = 2;
const CPU_EVENT_ID_OFFSET = 1;
const THREAD_EVENT_ID_OFFSET = 2;
// Exclusive bound for zero-based interval IDs: both event IDs must be safe integers.
const INTERVAL_ID_LIMIT = Math.floor(
  Number.MAX_SAFE_INTEGER / EVENT_IDS_PER_INTERVAL,
);

const TRACK_KIND_ORDER = {
  cpuGroup: 0,
  cpu: 1,
  threadGroup: 2,
  process: 3,
  thread: 4,
};
const SECONDS_PER_NANOSECOND = 1e-9;

function sqlRenderer(id, sql, componentName, sources = {}) {
  const inputs = Object.entries(sources).map(([name, renderer]) => ({
    ...renderer.config.output,
    name,
  }));
  return {
    type: 'SQL',
    id,
    config: {
      sql,
      inputs,
      data_source: {
        tables: Object.fromEntries(
          Object.entries(sources).map(([name, renderer]) => [
            name,
            [{ renderer_id: renderer.id, output: renderer.config.output.name }],
          ]),
        ),
      },
      output: {
        name: 'table',
        cardinality: 'one',
        component_type: {
          name: componentName,
          schema_version: componentName.startsWith('trace_') ? '0.1' : '1.0',
        },
      },
    },
  };
}

function makeIntervals(duration) {
  return sqlRenderer(
    'scheduler_intervals',
    `
      WITH threads AS (
        SELECT UNNEST(threads) AS t
        FROM read_json({{path:${FILES.threads}}}, columns = {
          threads: 'STRUCT(unique_thread_id UBIGINT, end_timestamp BIGINT)[]'
        })
      ), cpu_switches AS (
        SELECT *, LEAD(timestamp, 1, ${duration}) OVER (
          PARTITION BY cpu ORDER BY timestamp, sequence
        ) AS next_timestamp
        FROM read_parquet({{path:${FILES.switches}}}, file_row_number = true)
      ), switches AS (
        SELECT *,
        -- A normal thread cannot run on two CPUs at once. A later switch-in
        -- also bounds its previous slice when the switch-out was not captured.
        CASE WHEN utid <> ${UNKNOWN_THREAD_ID}::UBIGINT THEN
          LEAD(timestamp) OVER (PARTITION BY utid ORDER BY timestamp, sequence)
        END AS next_thread_timestamp
        -- Idle switches close CPU intervals, but have no displayed spans.
        -- Drop them before sorting by thread and materializing execution facts.
        FROM cpu_switches WHERE utid <> ${IDLE_THREAD_ID}
      ), intervals AS (
        -- The single immutable Parquet file supplies a stable identity even
        -- when execution order differs from physical row order. Reserve two
        -- positive, exactly representable JavaScript IDs for each interval.
        SELECT CASE WHEN s.file_row_number < ${INTERVAL_ID_LIMIT}
          THEN s.file_row_number
          ELSE error('Scheduler interval identity exceeds the safe event ID range')
        END::BIGINT AS interval_id, s.cpu, s.utid,
          GREATEST(0, s.timestamp)::BIGINT AS start_ns,
          -- Metadata already contains the analyzed lifetime, including inferred
          -- exits. Open lifetimes are bounded by the capture and CPU switches.
          LEAST(${duration}, s.next_timestamp, s.next_thread_timestamp,
            t.end_timestamp)::BIGINT AS end_ns
        FROM switches s
        LEFT JOIN threads ON t.unique_thread_id = s.utid
      )
      SELECT interval_id, cpu, utid, start_ns, end_ns
      FROM intervals WHERE end_ns > start_ns
    `,
    'scheduler-execution-intervals',
  );
}

function makeThreads(intervals) {
  return sqlRenderer(
    'scheduler_threads',
    `
      WITH threads AS (
        SELECT UNNEST(threads) AS t
        FROM read_json({{path:${FILES.threads}}}, columns = {
          threads: 'STRUCT(unique_thread_id UBIGINT, thread_id BIGINT,
            unique_process_id UBIGINT, name VARCHAR, is_idle BOOLEAN)[]'
        })
      ), processes AS (
        SELECT UNNEST(processes) AS p
        FROM read_json({{path:${FILES.processes}}}, columns = {
          processes: 'STRUCT(unique_process_id UBIGINT, process_id BIGINT, name VARCHAR)[]'
        })
      )
      SELECT i.utid, t.unique_process_id AS upid,
        (i.utid = ${IDLE_THREAD_ID} OR COALESCE(t.is_idle, FALSE)) AS is_idle,
        -- Idle and system-wide IDs are shared across CPUs, not thread lanes.
        (i.utid NOT IN (${IDLE_THREAD_ID}, ${UNKNOWN_THREAD_ID}::UBIGINT)
          AND NOT COALESCE(t.is_idle, FALSE)) AS is_thread,
        CASE WHEN i.utid = ${IDLE_THREAD_ID} OR t.is_idle THEN 'Idle'
          WHEN i.utid = ${UNKNOWN_THREAD_ID}::UBIGINT THEN 'Unknown thread'
          WHEN t.unique_thread_id IS NULL THEN 'Thread ' || i.utid
          ELSE COALESCE(NULLIF(t.name, ''), 'Thread') || ' (TID ' || t.thread_id || ')'
        END AS thread_name,
        CASE WHEN t.unique_process_id IS NULL THEN 'Unknown process'
          WHEN p.unique_process_id IS NULL THEN 'Process ' || t.unique_process_id
          ELSE COALESCE(NULLIF(p.name, ''), 'Process') || ' (PID ' || p.process_id || ')'
        END AS process_name
      FROM (SELECT DISTINCT utid FROM {{table:intervals}}) i
      LEFT JOIN threads ON t.unique_thread_id = i.utid
      LEFT JOIN processes ON p.unique_process_id = t.unique_process_id
    `,
    'scheduler-thread-dimensions',
    { intervals },
  );
}

function makeTracks(duration, threads) {
  return sqlRenderer(
    'scheduler_tracks',
    `
      WITH candidates AS (
        SELECT 'cpus' AS key, NULL::VARCHAR AS parent_key,
          'CPU execution' AS name, 'group' AS track_kind, ${TRACK_KIND_ORDER.cpuGroup} AS kind_order,
          0::HUGEINT AS identity, NULL::UINTEGER AS cpu, NULL::UBIGINT AS utid
        UNION ALL
        SELECT 'threads', NULL, 'Thread execution', 'group', ${TRACK_KIND_ORDER.threadGroup}, 0, NULL, NULL
        UNION ALL
        SELECT 'cpu/' || cpu, 'cpus', 'CPU ' || cpu, 'span', ${TRACK_KIND_ORDER.cpu}, cpu, cpu, NULL
        -- Discover lanes independently of execution facts so idle-only CPUs
        -- retain their tracks without storing every idle interval.
        FROM (SELECT DISTINCT cpu FROM read_parquet({{path:${FILES.switches}}})
          WHERE timestamp < ${duration} AND ${duration} > 0)
        UNION ALL
        SELECT DISTINCT 'process/' || COALESCE(upid::VARCHAR, 'unknown'),
          'threads', process_name, 'group', ${TRACK_KIND_ORDER.process}, COALESCE(upid::HUGEINT, -1), NULL, NULL
        FROM {{table:threads}} WHERE is_thread
        UNION ALL
        SELECT 'thread/' || utid,
          'process/' || COALESCE(upid::VARCHAR, 'unknown'), thread_name,
          'span', ${TRACK_KIND_ORDER.thread}, utid, NULL, utid
        FROM {{table:threads}} WHERE is_thread
      ), numbered AS (
        SELECT *, ROW_NUMBER() OVER (ORDER BY kind_order, identity, key)::BIGINT AS track_id
        FROM candidates
      )
      SELECT t.track_id, p.track_id AS parent_track_id, t.name, t.track_kind,
        t.track_id::INTEGER AS sort_order,
        CASE WHEN t.track_kind = 'span' THEN 1::UINTEGER ELSE NULL END AS row_count,
        NULL::VARCHAR AS category, t.cpu, t.utid
      FROM numbered t LEFT JOIN numbered p ON p.key = t.parent_key
    `,
    'trace_tracks',
    { threads },
  );
}

function makeSpans(intervals, threads, tracks) {
  return sqlRenderer(
    'scheduler_spans',
    `
      WITH lanes AS (
        SELECT i.interval_id * ${EVENT_IDS_PER_INTERVAL} + ${CPU_EVENT_ID_OFFSET} AS event_id,
          t.track_id, i.start_ns, i.end_ns, d.thread_name AS name
        FROM {{table:intervals}} i
        JOIN {{table:tracks}} t ON t.cpu = i.cpu
        JOIN {{table:threads}} d ON d.utid = i.utid
        -- Metadata-marked idle threads also display as gaps.
        WHERE NOT d.is_idle
        UNION ALL
        SELECT i.interval_id * ${EVENT_IDS_PER_INTERVAL} + ${THREAD_EVENT_ID_OFFSET} AS event_id,
          t.track_id, i.start_ns, i.end_ns, 'CPU ' || i.cpu AS name
        FROM {{table:intervals}} i JOIN {{table:tracks}} t ON t.utid = i.utid
      )
      SELECT event_id, track_id, start_ns, end_ns, 0::UINTEGER AS row, name,
        NULL::BIGINT AS parent_event_id, NULL::VARCHAR AS category,
        NULL::VARCHAR AS color_key, NULL::VARCHAR AS color, NULL::UINTEGER AS flags
      FROM lanes
    `,
    'trace_spans',
    { intervals, threads, tracks },
  );
}

/** Build a separate, full-capture scheduler trace when the export is present. */
function buildSchedulerTrace(context) {
  // Only expose the scheduler when its switch data and metadata are present.
  if (
    Object.values(FILES).some(
      (path) => context.listRunComponents(0, path).length !== 1,
    )
  ) {
    return { renderers: [], visualizations: [] };
  }
  const capture = JSON.parse(context.readRunComponent(0, FILES.capture));
  if (
    !Number.isSafeInteger(capture.duration) ||
    capture.duration < 0 ||
    capture.time_unit !== 'nanoseconds'
  ) {
    throw new Error('Invalid scheduler capture duration or time unit');
  }

  const intervals = makeIntervals(capture.duration);
  const threads = makeThreads(intervals);
  const tracks = makeTracks(capture.duration, threads);
  const spans = makeSpans(intervals, threads, tracks);
  // Store execution facts once and keep labels/hierarchy in small dimensions.
  // The span view projects either lane only when requested; stable IDs let
  // track/time predicates reach the facts without a global numbering window.
  intervals.config.materialized = true;
  intervals.config.output.component_type.schema_version = '2.0';
  threads.config.materialized = true;
  tracks.config.materialized = true;
  const viewport = {
    type: 'TraceViewportRenderer',
    id: 'scheduler_trace',
    config: {
      data_source: {
        tables: {
          tracks: [{ renderer_id: tracks.id, output: 'table' }],
          spans: [{ renderer_id: spans.id, output: 'table' }],
        },
      },
    },
  };
  return {
    renderers: [intervals, threads, tracks, spans, viewport],
    visualizations: [
      {
        type: 'timeline',
        id: 'scheduler',
        rendererId: viewport.id,
        title: 'CPU scheduling',
        description:
          'CPU execution and thread execution grouped by process across the full capture. CPU lanes show running threads; thread lanes show the CPU used. Gaps in CPU lanes indicate idle or unobserved time; gaps in thread lanes indicate time without observed execution.',
        config: {
          timeDomain: { start: 0, end: capture.duration, unit: 'ns' },
          xAxisUnit: 's',
          xAxisDisplayScale: SECONDS_PER_NANOSECOND,
          data_source: {
            tables: {
              tracks: [{ renderer_id: tracks.id, output: 'table' }],
              spans: [{ renderer_id: spans.id, output: 'table' }],
            },
          },
          groups: {
            scheduler: {
              index: 0,
              type: 'trace',
              title: 'CPU scheduling',
              config: {
                datasetId: viewport.id,
                trackSourceKey: 'tracks',
                spanSourceKey: 'spans',
                xAxisTitle: '',
              },
            },
          },
        },
      },
    ],
  };
}

module.exports = { buildSchedulerTrace };
