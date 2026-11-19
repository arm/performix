// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

function sqlRenderer(id, sql, componentName, sources) {
  return {
    type: 'SQL',
    id,
    config: {
      sql,
      inputs: Object.entries(sources).map(([name, renderer]) => ({
        ...renderer.config.output,
        name,
      })),
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
        component_type: { name: componentName, schema_version: '0.1' },
      },
    },
  };
}

function buildThreadAnalysisRender(context, { intervals, threads, duration }) {
  const { processes } = JSON.parse(
    context.readRunComponent(
      0,
      'tool/neoprof/0/output/parquet/metadata/processes.json',
    ),
  );
  const selectedProcesses = selectProcesses(context, processes);
  const processFilter =
    selectedProcesses === processes
      ? ''
      : `AND upid IN (${selectedProcesses.map((process) => process.unique_process_id).join(',')})`;

  const tracks = sqlRenderer(
    'thread_analysis_tracks',
    `SELECT ROW_NUMBER() OVER (ORDER BY utid)::BIGINT AS track_id,
      NULL::BIGINT AS parent_track_id, thread_name AS name, 'span' AS track_kind,
      track_id AS sort_order, 1::UINTEGER AS row_count,
      NULL::VARCHAR AS category, utid, upid
    FROM {{table:threads}} AS threads
    WHERE is_thread ${processFilter}`,
    'trace_tracks',
    { threads },
  );
  tracks.config.materialized = true;

  const spans = sqlRenderer(
    'thread_analysis_spans',
    `WITH execution AS (
      SELECT i.interval_id, i.start_ns, i.end_ns, t.track_id,
        LEAD(i.start_ns) OVER (
          PARTITION BY i.utid ORDER BY i.start_ns, i.interval_id
        ) AS next_start_ns
      FROM {{table:intervals}} i JOIN {{table:tracks}} t ON t.utid = i.utid
    ), activity AS (
      SELECT interval_id * 2 + 1 AS event_id, track_id, start_ns, end_ns,
        'on-cpu' AS category
      FROM execution
      UNION ALL
      SELECT interval_id * 2 + 2 AS event_id, track_id,
        end_ns AS start_ns, next_start_ns AS end_ns, 'off-cpu' AS category
      FROM execution WHERE next_start_ns > end_ns
    )
    SELECT event_id, track_id, start_ns, end_ns,
      0::UINTEGER AS row,
      -- Hack to force known colours: six spaces hash to red, one space to green.
      CASE WHEN category = 'off-cpu' THEN '      ' ELSE ' ' END AS name,
      NULL::BIGINT AS parent_event_id, category,
      NULL::VARCHAR AS color_key, NULL::VARCHAR AS color, NULL::UINTEGER AS flags
    FROM activity`,
    'trace_spans',
    { intervals, tracks },
  );
  spans.config.materialized = true;

  const reasonPath =
    'tool/neoprof/0/output/parquet/timeline/scheduler_reason_metadata.parquet';
  const reasons =
    context.listRunComponents(0, reasonPath).length === 1
      ? `SELECT reason, name FROM read_parquet({{path:${reasonPath}}})`
      : 'SELECT NULL::BIGINT AS reason, NULL::VARCHAR AS name WHERE false';
  const switchOuts = sqlRenderer(
    'thread_analysis_switch_outs',
    `WITH reasons AS (${reasons})
    SELECT sw.interval_id, sw.switch_out_ns, r.name AS reason
    FROM {{table:intervals}} sw LEFT JOIN reasons r ON r.reason = sw.switch_out_reason`,
    'thread-analysis-switch-outs',
    { intervals },
  );
  switchOuts.config.materialized = true;

  const metadata = sqlRenderer(
    'thread_analysis_metadata',
    `WITH entries AS (
      SELECT s.event_id AS owner_id,
        -- Projection UNNEST lets the event-ID filter reach spans before formatting.
        unnest(map_entries(json_object(
          'State', CASE WHEN s.category = 'on-cpu' THEN 'Executing'
            ELSE format('Off-CPU ({})', COALESCE(sw.reason, 'unknown')) END,
          'Duration', CASE
            WHEN s.end_ns - s.start_ns >= 1000000000
              THEN format('{} s', round((s.end_ns - s.start_ns) / 1000000000.0, 2))
            WHEN s.end_ns - s.start_ns >= 1000000
              THEN format('{} ms', round((s.end_ns - s.start_ns) / 1000000.0, 2))
            WHEN s.end_ns - s.start_ns >= 1000
              THEN format('{} µs', round((s.end_ns - s.start_ns) / 1000.0, 2))
            ELSE format('{} ns', s.end_ns - s.start_ns) END,
          'CPU', CASE WHEN s.category = 'on-cpu' THEN i.cpu END,
          'Thread', t.name
        )::MAP(VARCHAR, JSON))) AS entry
      FROM {{table:spans}} s
      JOIN {{table:intervals}} i ON i.interval_id = (s.event_id - 1) // 2
      JOIN {{table:tracks}} t ON t.track_id = s.track_id
      -- Only attribute a reason when the observed CPU switch ended this interval.
      LEFT JOIN {{table:switchOuts}} sw ON sw.interval_id = i.interval_id
        AND i.end_ns = sw.switch_out_ns
    )
    SELECT 'span' AS owner_kind, owner_id, entry.key, entry.value
    FROM entries WHERE entry.value IS NOT NULL`,
    'trace_metadata',
    { spans, intervals, tracks, switchOuts },
  );

  const dataSource = {
    tables: {
      tracks: [{ renderer_id: tracks.id, output: 'table' }],
      spans: [{ renderer_id: spans.id, output: 'table' }],
    },
  };
  const viewport = {
    type: 'TraceViewportRenderer',
    id: 'thread_analysis_trace',
    config: { data_source: dataSource },
  };
  const renderers = [tracks, spans, viewport, switchOuts, metadata];
  const tables = {
    spans: dataSource.tables.spans,
    metadata: [{ renderer_id: metadata.id, output: 'table' }],
  };
  const groups = {};
  for (const process of selectedProcesses) {
    if (process.process_id === 0) continue;
    const key = `process_${process.unique_process_id}`;
    const processTracks = sqlRenderer(
      `thread_analysis_${key}_tracks`,
      `SELECT * FROM {{table:tracks}} WHERE upid = ${process.unique_process_id}`,
      'trace_tracks',
      { tracks },
    );
    renderers.push(processTracks);
    tables[key] = [{ renderer_id: processTracks.id, output: 'table' }];
    groups[key] = {
      index: process.process_id,
      type: 'trace',
      title: `${process.name || 'Unknown process'} (${process.process_id})`,
      config: {
        datasetId: viewport.id,
        trackSourceKey: key,
        spanSourceKey: 'spans',
        metadataSourceKey: 'metadata',
        xAxisTitle: '',
      },
    };
  }
  return {
    renderers,
    visualizations: [
      {
        type: 'timeline',
        id: 'thread_analysis',
        rendererId: viewport.id,
        title: 'Threads',
        description:
          'Thread execution grouped by process across the full capture.',
        config: {
          timeDomain: { start: 0, end: duration, unit: 'ns' },
          xAxisUnit: 's',
          xAxisDisplayScale: 1e-9,
          data_source: { tables },
          groups,
        },
      },
    ],
  };
}

/**
 * @param {import('../docs/jsdocs').RenderExecutionContext} context
 * @param {Array<{process_id: number, unique_process_id: number, parent_unique_process_id?: number, name?: string}>} processes
 */
function selectProcesses(context, processes) {
  const workloadType = context.getRunDescriptions()[0].WorkloadType;
  const launchLogPath = 'tool/neoprof/0/capture_log_err.txt';
  let targetPid;
  if (workloadType === 'Attach') {
    const runMetadata = JSON.parse(
      context.readRunComponent(0, 'metadata.json'),
    );
    targetPid = runMetadata['run.workload.pid'];
  } else if (
    workloadType === 'Launch' &&
    context.listRunComponents(0, launchLogPath).length === 1
  ) {
    // The collector documents this message as an interface for external programs.
    const launchLog = context.readRunComponent(0, launchLogPath);
    targetPid = Number(
      launchLog.match(/Successfully forked child process \{pid: (\d+),/)?.[1],
    );
  }
  const targetProcess = processes.find(
    (process) => process.process_id === targetPid,
  );
  if (!targetProcess) return processes;

  const processIds = new Set([targetProcess.unique_process_id]);
  let previousSize;
  do {
    previousSize = processIds.size;
    for (const process of processes) {
      if (processIds.has(process.parent_unique_process_id)) {
        processIds.add(process.unique_process_id);
      }
    }
  } while (processIds.size !== previousSize);
  return processes.filter((process) =>
    processIds.has(process.unique_process_id),
  );
}

module.exports = { buildThreadAnalysisRender };
