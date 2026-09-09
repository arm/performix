// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

const TOOL_JITDUMP_JVM = {
  name: 'jitdump-jvm',
  version: '1.0.0',
};

const JFR_PARQUET_DIR = 'tool/jitdump-jvm/0/parquet';
const PARQUET_METADATA_PATH = `${JFR_PARQUET_DIR}/metadata`;
const PARQUET_EVENTS_PATH = `${JFR_PARQUET_DIR}/events`;
const JFR_COMPONENTS = {
  recordings: `${PARQUET_METADATA_PATH}/jfr_recordings.parquet`,
  jvmInfo: `${PARQUET_EVENTS_PATH}/jfr_jvm_information.parquet`,
  systemProperties: `${PARQUET_EVENTS_PATH}/jfr_initial_system_property.parquet`,
  heapSummary: `${PARQUET_EVENTS_PATH}/jfr_gc_heap_summary.parquet`,
  garbageCollection: `${PARQUET_EVENTS_PATH}/jfr_garbage_collection.parquet`,
};

const SQL_RENDERER_OUTPUT = {
  name: 'table',
  component_type: { name: 'flat_table', schema_version: '1.0' },
};

const RECORDING_FILTER_PARAMETER = 'recording_id';

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
 * Returns a validated recording ID from the render parameters. A missing
 * parameter intentionally selects the first recording so the initial render
 * matches the filter's implicit first option.
 *
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 * @returns {string|null}
 */
function getSelectedRecordingId(context) {
  const value = context.getRenderParameter(RECORDING_FILTER_PARAMETER);
  if (value === null || value === undefined) {
    return null;
  }

  const recordingId = String(value);
  if (!/^\d+$/.test(recordingId)) {
    throw new Error(`Invalid Java recording ID: ${recordingId}`);
  }
  return recordingId;
}

/**
 * Builds a predicate that selects either the requested recording or the first
 * recording captured in the run.
 *
 * @param {string|null} selectedRecordingId
 * @returns {string}
 */
function makeRecordingPredicate(selectedRecordingId) {
  const recordingId =
    selectedRecordingId ??
    `(SELECT MIN(recording_id) FROM read_parquet({{path:${JFR_COMPONENTS.recordings}}}))`;
  return `recording_id = ${recordingId}`;
}

/**
 * @param {import("./docs/jsdocs").Workload} workload
 * @returns {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function buildJavaAnalysisTools(workload) {
  return {
    toolConfigs: [
      {
        name: TOOL_JITDUMP_JVM.name,
        params: {},
        workload,
        env: {},
      },
    ],
  };
}

/**
 * Performs readiness checks for the Java Analysis recipe.
 *
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @returns {import("./docs/jsdocs").RecipeReadyOutput}
 */

function readyJavaAnalysis(context) {
  const tools = buildJavaAnalysisTools(context.getWorkload());
  const toolResponses = context.probeTools(tools);
  const advice = collectToolAdvice(tools, toolResponses);

  return {
    status: toolStatusToRecipeStatus(advice),
    advice,
  };
}
/**
 * Runs the Java Analysis recipe.
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runJavaAnalysis(context) {
  const tools = buildJavaAnalysisTools(context.getWorkload());
  context.runTools(tools);
}

/**
 *
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 * @returns {import("./docs/jsdocs").RecipeRenderOutput}
 */
function renderJavaAnalysis(context) {
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
    'jvm_info',
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

  const selectedRecordingId = getSelectedRecordingId(context);
  const recordingOptionsRenderer = makeSQLRenderer(
    'jfr_recording_options',
    `SELECT
      CAST(recording_id AS VARCHAR) AS value,
      'PID ' || COALESCE(CAST(jvm_pid AS VARCHAR), 'unknown') ||
        ' — Recording ' ||
        CAST(recording_id AS VARCHAR) AS label
    FROM read_parquet({{path:${JFR_COMPONENTS.recordings}}})
    ORDER BY recording_id`,
  );

  const summaryRenderer = makeSQLRenderer(
    'java_summary',
    `WITH selected_recording AS (
      SELECT
        *
      FROM read_parquet({{path:${JFR_COMPONENTS.recordings}}})
      WHERE ${makeRecordingPredicate(selectedRecordingId)}
    ), selected_jvm AS (
      SELECT
        *
      FROM read_parquet({{path:${JFR_COMPONENTS.jvmInfo}}})
      WHERE ${makeRecordingPredicate(selectedRecordingId)}
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
      WHERE ${makeRecordingPredicate(selectedRecordingId)}
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

  const heapTimelineRenderer = makeSQLRenderer(
    'jvm_heap_timeline',
    `WITH selected_recording AS (
      SELECT
        recording_id,
        recording_start_epoch_ns
      FROM read_parquet({{path:${JFR_COMPONENTS.recordings}}})
      WHERE ${makeRecordingPredicate(selectedRecordingId)}
    )
    SELECT
      CAST(heap.after_event_start_epoch_ns - recording.recording_start_epoch_ns AS DOUBLE) /
        1000000000.0 AS time_s,
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

  const recordingFilter = {
    type: 'single_selection_list_filter',
    id: 'jfr_recording',
    rendererId: 'jfr_recording_options',
    title: 'Recording',
    description: 'Select the JVM recording shown in the analysis views.',
    parameterBindings: {
      value: RECORDING_FILTER_PARAMETER,
    },
    config: {
      allowNone: false,
      search: false,
      data_source: {
        tables: {
          recordings: [
            { renderer_id: 'jfr_recording_options', output: 'table' },
          ],
        },
      },
      optionsQuery: {
        dataSource: 'recordings',
        query:
          'SELECT value, label FROM __RECORDINGS__ ORDER BY CAST(value AS HUGEINT)',
        tableNamePlaceholder: '__RECORDINGS__',
      },
      emptyMessage: 'No JFR recordings were captured.',
    },
  };

  const summaryVisualization = {
    type: 'java_analysis_summary',
    id: 'java_analysis_summary',
    rendererId: 'java_summary',
    title: 'Summary',
    description:
      'JVM information, initial system properties, and recording constants.',
    config: {
      data_source: {
        tables: {
          table: [{ renderer_id: 'java_summary', output: 'table' }],
        },
      },
    },
  };

  const garbageCollectionVisualization = {
    type: 'timeline',
    id: 'java_analysis_garbage_collection',
    rendererId: 'jvm_heap_timeline',
    title: 'Garbage Collection',
    description:
      'Heap occupancy and capacity snapshots captured around garbage collection events.',
    config: {
      xAxisUnit: 's',
      data_source: {
        tables: {
          heap_summary: [{ renderer_id: 'jvm_heap_timeline', output: 'table' }],
        },
      },
      groups: {
        heap_summary: {
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
                xColumn: 'time_s',
                yColumn: 'used_after_gc_mib',
              },
              {
                type: 'single',
                name: 'Committed heap',
                xColumn: 'time_s',
                yColumn: 'committed_mib',
              },
              {
                type: 'single',
                name: 'Reserved heap',
                xColumn: 'time_s',
                yColumn: 'reserved_mib',
              },
            ],
          },
        },
      },
    },
  };

  return {
    renderers: [
      { type: 'TargetInfoRenderer', id: 'target_info' },
      // These raw tables are intentionally retained for CLI and MCP queries,
      // even though the GUI now presents the dedicated summary visualization.
      recordingsRenderer,
      jvmInfoRenderer,
      systemPropertiesRenderer,
      heapSummaryRenderer,
      garbageCollectionRenderer,
      recordingOptionsRenderer,
      summaryRenderer,
      heapTimelineRenderer,
    ],
    ui: {
      visualizations: [summaryVisualization, garbageCollectionVisualization],
      side_panel_filters: [recordingFilter],
    },
  };
}

const recipe = {
  name: 'java_analysis',
  title: 'Java Analysis',
  version: '1.0.0',
  api_version: '1.0.0',
  status: 'experimental',
  description:
    'Collects Java Flight Recorder (JFR) data and presents JVM information.',
  mcp_guidance:
    'Supports Java workloads (launch/attach/system-wide). Attach mode captures activity only after attachment and cannot recover earlier JVM activity.',
  deployments: [
    {
      appliesTo: [
        { architecture: 'x86_64', os: 'Linux' },
        { architecture: 'aarch64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_JITDUMP_JVM.name,
          version: TOOL_JITDUMP_JVM.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],

  // java_analysis phase one fixes JFR settings to "profile"
  parameters: [],

  renderParameters: [
    {
      id: RECORDING_FILTER_PARAMETER,
      config: {
        type: 'string',
      },
    },
  ],

  readyStages: [
    {
      name: 'Check Java Analysis readiness',
      description:
        'Check that Java Flight Recorder (JFR) collection is available on the target system.',
      exec: readyJavaAnalysis,
    },
  ],

  runStages: [
    {
      name: 'Collect Java runtime data',
      description: 'Collect and convert JFR data for the selected workload.',
      exec: runJavaAnalysis,
    },
  ],

  renderStages: [
    {
      name: 'Render Java analysis results',
      description: 'Render the collected JFR data into a human-readable format',
      exec: renderJavaAnalysis,
    },
  ],
};
