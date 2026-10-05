// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Shared Java presentation for owner-integrated recipes.

const SQL_RENDERER_OUTPUT = {
  name: 'table',
  component_type: { name: 'flat_table', schema_version: '1.0' },
};

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
 * Returns the explicitly selected process. None uses workload-specific defaults.
 *
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 * @returns {string|null}
 */
function getSelectedProcessId(context) {
  const value = context.getRenderParameter('filter_pid');
  if (value === null || value === undefined) {
    return null;
  }

  const pid = String(value);
  if (!/^-?\d+$/.test(pid)) {
    throw new Error(`Invalid Java process ID: ${pid}`);
  }
  return pid;
}

/**
 *
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 * @param {string} ownerParquetRoot
 * @param {any[]} existingVisualizations
 * @returns {import("../docs/jsdocs").RecipeRenderOutput} Includes the existing visualizations with Java data composed in, without modifying the inputs.
 */
function buildJavaAnalysisRender(
  context,
  ownerParquetRoot,
  existingVisualizations = [],
) {
  if (context.getRunDescriptions().length !== 1) {
    return emptyJavaRender(existingVisualizations);
  }

  const selectedProcessId = getSelectedProcessId(context);
  const components = discoverJfrComponents(context, ownerParquetRoot);
  if (!components) return emptyJavaRender(existingVisualizations);
  const predicate = selectRecordingPredicate(
    context,
    components,
    selectedProcessId,
  );
  if (predicate === null) return emptyJavaRender(existingVisualizations);

  const visible = predicate !== 'FALSE';
  const summary = buildJavaSummary(components, predicate, visible);
  const heap = buildHeapTimeline(components, predicate, visible);
  const hasTimeline = existingVisualizations.some(
    ({ id }) => id === 'timeline',
  );
  const visualizations = existingVisualizations.map((visualization) =>
    visualization.id === 'timeline'
      ? mergeHeapSummaryIntoTimeline(visualization, heap.visualization)
      : visualization,
  );
  visualizations.push(summary.visualization);
  if (!hasTimeline) visualizations.push(heap.visualization);

  return {
    renderers: [
      ...buildRawJavaRenderers(components),
      summary.renderer,
      heap.renderer,
    ],
    ui: { visualizations, side_panel_filters: [] },
  };
}

function discoverJfrComponents(context, ownerParquetRoot) {
  const metadataPath = `${ownerParquetRoot}/metadata`;
  const eventsPath = `${ownerParquetRoot}/events`;
  const JFR_COMPONENTS = {
    recordingIndex: `${metadataPath}/jfr_recordings.json`,
    recordings: `${metadataPath}/jfr_recordings.parquet`,
    jvmInfo: `${eventsPath}/jfr_jvm_information.parquet`,
    systemProperties: `${eventsPath}/jfr_initial_system_property.parquet`,
    heapSummary: `${eventsPath}/jfr_gc_heap_summary.parquet`,
    garbageCollection: `${eventsPath}/jfr_garbage_collection.parquet`,
  };
  if (
    !Object.values(JFR_COMPONENTS).every(
      (component) => context.listRunComponents(0, component).length === 1,
    )
  ) {
    return null;
  }

  return JFR_COMPONENTS;
}

function selectRecordingPredicate(context, JFR_COMPONENTS, selectedProcessId) {
  const selectDefaultRecording = ['Launch', 'Attach'].includes(
    context.getRunDescriptions()[0].WorkloadType,
  );

  let recordings;
  try {
    recordings = JSON.parse(
      context.readRunComponent(0, JFR_COMPONENTS.recordingIndex),
    );
    if (
      !Array.isArray(recordings) ||
      !recordings.every(
        (recording) =>
          recording !== null && /^\d+$/.test(String(recording.recording_id)),
      )
    )
      throw new Error('Invalid JFR recording index');
    recordings.sort((a, b) => a.recording_id - b.recording_id);
  } catch (error) {
    context.logWarn(`JFR recording index unavailable: ${error}`);
    return null;
  }
  const selectedRecordings =
    selectedProcessId === null
      ? selectDefaultRecording
        ? recordings.slice(0, 1)
        : []
      : recordings.filter(
          (recording) => String(recording.jvm_pid) === selectedProcessId,
        );
  const recordingIds = selectedRecordings.map((recording) => {
    const id = String(recording.recording_id);
    return id;
  });
  // Keep renderer and widget IDs stable when the Process filter has no JVM.
  // Visibility hides Java views; empty queries preserve the render topology.
  return recordingIds.length
    ? `recording_id IN (${recordingIds.join(', ')})`
    : 'FALSE';
}

function buildRawJavaRenderers(JFR_COMPONENTS) {
  const recordingsRenderer = makeSQLRenderer(
    'jfr_recordings',
    `SELECT
      CAST(recording_id AS HUGEINT) AS "Recording",
      source_jfr_relative_path AS "Source JFR",
      CAST(jvm_pid AS BIGINT) AS "PID",
      CAST(jvm_start_epoch_ns AS BIGINT) AS "JVM Start (epoch ns)",
      CAST(recording_start_epoch_ns AS BIGINT) AS "Recording Start (epoch ns)",
      CAST(recording_end_epoch_ns AS BIGINT) AS "Recording End (epoch ns)",
      CAST(parse_complete AS VARCHAR) AS "Parse Complete",
      parse_error AS "Parse Error"
    FROM read_parquet({{path:${JFR_COMPONENTS.recordings}}})
    ORDER BY recording_id`,
  );

  const jvmInfoRenderer = makeSQLRenderer(
    'jvm_info_raw',
    `SELECT
      CAST(recording_id AS HUGEINT) AS "Recording",
      CAST(jvm_pid AS BIGINT) AS "PID",
      jvm_name AS "JVM Name",
      jvm_version AS "JVM Version",
      java_arguments AS "Java Arguments",
      jvm_arguments AS "JVM Arguments",
      jvm_flags AS "JVM Flags",
      CAST(jvm_start_epoch_ns AS BIGINT) AS "JVM Start (epoch ns)"
    FROM read_parquet({{path:${JFR_COMPONENTS.jvmInfo}}})
    ORDER BY recording_id, jvm_pid`,
  );

  const systemPropertiesRenderer = makeSQLRenderer(
    'jvm_system_properties',
    `SELECT
      CAST(recording_id AS HUGEINT) AS "Recording",
      property_key AS "Property",
      property_value AS "Value"
    FROM read_parquet({{path:${JFR_COMPONENTS.systemProperties}}})
    ORDER BY recording_id, property_key`,
  );

  const heapSummaryRenderer = makeSQLRenderer(
    'jvm_heap_summary',
    `WITH heap_phases AS (
      SELECT
        heap.recording_id,
        heap.gc_id,
        phase.event_start_epoch_ns,
        phase.gc_phase,
        phase.used_bytes,
        phase.start_address,
        phase.committed_end_address,
        phase.committed_size_bytes,
        phase.reserved_end_address,
        phase.reserved_size_bytes
      FROM read_parquet({{path:${JFR_COMPONENTS.heapSummary}}}) AS heap
      CROSS JOIN LATERAL (
        VALUES
          (
            heap.before_event_start_epoch_ns,
            'Before GC',
            heap.before_used_bytes,
            heap.before_heap_space_start_address,
            heap.before_heap_space_committed_end_address,
            heap.before_heap_space_committed_size_bytes,
            heap.before_heap_space_reserved_end_address,
            heap.before_heap_space_reserved_size_bytes
          ),
          (
            heap.after_event_start_epoch_ns,
            'After GC',
            heap.after_used_bytes,
            heap.after_heap_space_start_address,
            heap.after_heap_space_committed_end_address,
            heap.after_heap_space_committed_size_bytes,
            heap.after_heap_space_reserved_end_address,
            heap.after_heap_space_reserved_size_bytes
          )
      ) AS phase(
        event_start_epoch_ns,
        gc_phase,
        used_bytes,
        start_address,
        committed_end_address,
        committed_size_bytes,
        reserved_end_address,
        reserved_size_bytes
      )
    )
    SELECT
      CAST(recording_id AS HUGEINT) AS "Recording",
      CAST(event_start_epoch_ns AS BIGINT) AS "Event Start (epoch ns)",
      CAST(gc_id AS BIGINT) AS "GC ID",
      gc_phase AS "GC Phase",
      CAST(used_bytes AS HUGEINT) AS "Used (bytes)",
      start_address AS "Start Address",
      committed_end_address AS "Committed End Address",
      CAST(committed_size_bytes AS HUGEINT) AS "Committed Size (bytes)",
      reserved_end_address AS "Reserved End Address",
      CAST(reserved_size_bytes AS HUGEINT) AS "Reserved Size (bytes)"
    FROM heap_phases
    WHERE event_start_epoch_ns IS NOT NULL
    ORDER BY recording_id, event_start_epoch_ns, gc_id, gc_phase`,
  );

  const garbageCollectionRenderer = makeSQLRenderer(
    'jvm_garbage_collections',
    `SELECT
      CAST(recording_id AS HUGEINT) AS "Recording",
      CAST(event_start_epoch_ns AS BIGINT) AS "Event Start (epoch ns)",
      CAST(event_duration_ns AS BIGINT) AS "Duration (ns)",
      CAST(gc_id AS BIGINT) AS "GC ID",
      gc_name AS "GC Name",
      gc_cause AS "GC Cause",
      CAST(sum_of_pauses_ns AS BIGINT) AS "Total Pause (ns)",
      CAST(longest_pause_ns AS BIGINT) AS "Longest Pause (ns)",
      event_thread_os_name AS "OS Thread",
      CAST(event_thread_os_thread_id AS BIGINT) AS "OS Thread ID",
      event_thread_java_name AS "Java Thread",
      CAST(event_thread_java_thread_id AS BIGINT) AS "Java Thread ID",
      event_thread_group_name AS "Thread Group",
      event_thread_group_parent_name AS "Parent Thread Group",
      CAST(event_thread_virtual AS VARCHAR) AS "Virtual Thread"
    FROM read_parquet({{path:${JFR_COMPONENTS.garbageCollection}}})
    ORDER BY recording_id, event_start_epoch_ns, gc_id`,
  );

  // Retain raw tables for CLI and MCP queries.
  return [
    recordingsRenderer,
    jvmInfoRenderer,
    systemPropertiesRenderer,
    heapSummaryRenderer,
    garbageCollectionRenderer,
  ];
}

function buildJavaSummary(JFR_COMPONENTS, recordingPredicate, visible) {
  const summaryRenderer = makeSQLRenderer(
    'java_summary',
    `WITH selected_recording AS (
      SELECT
        *
      FROM read_parquet({{path:${JFR_COMPONENTS.recordings}}})
      WHERE ${recordingPredicate}
    ), selected_jvm AS (
      SELECT
        *
      FROM read_parquet({{path:${JFR_COMPONENTS.jvmInfo}}})
      WHERE ${recordingPredicate}
    ), summary_rows AS (
      SELECT 10 AS sort_order, 'Recording' AS section, 'Recording ID' AS property,
        CAST(recording_id AS VARCHAR) AS value
      FROM selected_recording
      UNION ALL
      SELECT 20, 'Recording', 'PID', CAST(jvm_pid AS VARCHAR)
      FROM selected_recording
      UNION ALL
      SELECT 30, 'Recording', 'JVM age',
        CAST(ROUND(CAST(recording_start_epoch_ns - jvm_start_epoch_ns AS DOUBLE) / 1000000000.0, 3) AS VARCHAR) || ' s'
      FROM selected_recording
      UNION ALL
      SELECT 40, 'Recording', 'Recording duration',
        CAST(ROUND(CAST(recording_end_epoch_ns - recording_start_epoch_ns AS DOUBLE) / 1000000000.0, 3) AS VARCHAR) || ' s'
      FROM selected_recording
      UNION ALL
      SELECT 100, 'JVM Information', 'JVM name', jvm_name
      FROM selected_jvm
      UNION ALL
      SELECT 110, 'JVM Information', 'JVM version', jvm_version
      FROM selected_jvm
      UNION ALL
      SELECT 120, 'JVM Information', 'Java arguments', java_arguments
      FROM selected_jvm
      UNION ALL
      SELECT 130, 'JVM Information', 'JVM arguments', jvm_arguments
      FROM selected_jvm
      UNION ALL
      SELECT 200, 'Initial System Properties', property_key, property_value
      FROM read_parquet({{path:${JFR_COMPONENTS.systemProperties}}})
      WHERE ${recordingPredicate}
    )
    SELECT
      sort_order,
      section AS "Section",
      property AS "Property",
      value AS "Value"
    FROM summary_rows
    WHERE value IS NOT NULL AND value <> ''
    ORDER BY sort_order, property`,
  );

  const summaryVisualization = {
    type: 'java_analysis_summary',
    id: 'jvm_info',
    rendererId: 'java_summary',
    title: 'JVM Info',
    description:
      'JVM information, initial system properties, and recording constants.',
    config: {
      visible,
      data_source: {
        tables: {
          table: [{ renderer_id: 'java_summary', output: 'table' }],
        },
      },
    },
  };

  return { renderer: summaryRenderer, visualization: summaryVisualization };
}

function buildHeapTimeline(JFR_COMPONENTS, recordingPredicate, visible) {
  const heapTimelineRenderer = makeSQLRenderer(
    'jvm_heap_timeline',
    `WITH selected_recording AS (
      SELECT
        recording_id,
        recording_start_epoch_ns
      FROM read_parquet({{path:${JFR_COMPONENTS.recordings}}})
      WHERE ${recordingPredicate}
    )
    SELECT
      CAST(heap.after_event_start_epoch_ns - recording.recording_start_epoch_ns AS BIGINT) AS time_ns,
      CAST(heap.after_used_bytes AS DOUBLE) / 1048576.0 AS used_after_gc_mib,
      CAST(heap.after_heap_space_committed_size_bytes AS DOUBLE) /
        1048576.0 AS committed_mib,
      CAST(heap.after_heap_space_reserved_size_bytes AS DOUBLE) /
        1048576.0 AS reserved_mib
    FROM read_parquet({{path:${JFR_COMPONENTS.heapSummary}}}) AS heap
    JOIN selected_recording AS recording USING (recording_id)
    WHERE heap.after_event_start_epoch_ns IS NOT NULL
    ORDER BY heap.after_event_start_epoch_ns, heap.gc_id`,
  );

  const timelineVisualization = {
    type: 'timeline',
    id: 'timeline',
    rendererId: 'jvm_heap_timeline',
    title: 'Timeline',
    description:
      'Performance counters and JVM heap usage over the capture period.',
    config: {
      visible,
      xAxisUnit: 's',
      xAxisDisplayScale: 1e-9,
      data_source: {
        tables: {
          heap_summary: [{ renderer_id: 'jvm_heap_timeline', output: 'table' }],
        },
      },
      groups: {
        heap_summary: {
          visible,
          title: 'Heap Summary',
          type: 'line',
          index: 0,
          description:
            'Point-in-time heap snapshots captured by JFR. Garbage collection duration events are not shown.',
          config: {
            xAxisTitle: 'Time (s)',
            yAxisTitle: 'Heap size',
            yAxisUnit: 'mebibyte',
            yAxisDisplayRange: { min: 0 },
            series: [
              {
                type: 'single',
                name: 'Used heap after GC',
                xColumn: 'time_ns',
                yColumn: 'used_after_gc_mib',
              },
              {
                type: 'single',
                name: 'Committed heap',
                xColumn: 'time_ns',
                yColumn: 'committed_mib',
              },
              {
                type: 'single',
                name: 'Reserved heap',
                xColumn: 'time_ns',
                yColumn: 'reserved_mib',
              },
            ],
          },
        },
      },
    },
  };

  return {
    renderer: heapTimelineRenderer,
    visualization: timelineVisualization,
  };
}

function mergeHeapSummaryIntoTimeline(timeline, javaTimeline) {
  const tables = { ...timeline.config.data_source.tables };
  const javaTables = javaTimeline.config.data_source.tables;
  const heapSource = javaTables.heap_summary;
  const heapGroup = {
    ...javaTimeline.config.groups.heap_summary,
    config: { ...javaTimeline.config.groups.heap_summary.config },
  };
  const referenceGroup = Object.values(timeline.config.groups).find((group) =>
    Array.isArray(group.lods),
  );

  if (referenceGroup) {
    heapGroup.lods = referenceGroup.lods.map(({ binDuration }) => {
      const sourceKey = `heap_summary_${binDuration}`;
      tables[sourceKey] = heapSource;
      return { binDuration, sourceKey };
    });
    heapGroup.config.customQuery = {
      query: `SELECT *
        FROM {table}
        WHERE time_ns >= {rangeStart}
          AND time_ns < {rangeEnd}
        ORDER BY time_ns`,
      tableNamePlaceholder: '{table}',
      rangeStartPlaceholder: '{rangeStart}',
      rangeEndPlaceholder: '{rangeEnd}',
    };
  } else {
    tables.heap_summary = heapSource;
  }

  heapGroup.index =
    Math.max(
      ...Object.values(timeline.config.groups).map((group) => group.index),
    ) + 1;
  return {
    ...timeline,
    description: javaTimeline.description,
    config: {
      ...timeline.config,
      data_source: { ...timeline.config.data_source, tables },
      groups: { ...timeline.config.groups, heap_summary: heapGroup },
    },
  };
}

function emptyJavaRender(visualizations = []) {
  return {
    renderers: [],
    ui: { visualizations, side_panel_filters: [] },
  };
}

module.exports = { buildJavaAnalysisRender };
