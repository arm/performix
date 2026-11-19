// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const TOOL_WPERF = { name: 'wperf', version: '1.0.1' };
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const { getComparisonFilters } = require('../lib/comparison_filters');
const CORE_TYPE_RENDER_PARAMETER = 'filter_core_type';

/**
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 * @param {string} parameterId
 * @returns {number | null}
 */
function getRenderParameterIfExists(context, parameterId) {
  const param = context.getRenderParameter(parameterId);
  return param === null || param === undefined ? null : Number(param);
}

const timeRangeFilter = {
  id: 'time_range',
  type: 'time_range_filter',
  title: 'Time range',
  rendererId: 'time_range',
  description: 'Select the time region of interest',
  parameterBindings: {
    filter_start_time: 'filter_start_time_ns',
    filter_end_time: 'filter_end_time_ns',
  },
  config: {
    data_source: {
      tables: {
        timeLimits: [{ renderer_id: 'time_range', output: 'time_limits' }],
      },
    },
    rangeQuery: {
      dataSource: 'timeLimits',
      query:
        'SELECT CAST(MIN(start_time_ns) AS DOUBLE) AS start_time, CAST(MAX(end_time_ns) AS DOUBLE) AS end_time FROM __table__',
      tableNamePlaceholder: '__table__',
    },
    initialValuesQuery: {
      dataSource: 'timeLimits',
      query:
        'SELECT CAST(MIN(start_time_ns) AS DOUBLE) AS filter_start_time, CAST(MAX(end_time_ns) AS DOUBLE) AS filter_end_time FROM __table__',
      tableNamePlaceholder: '__table__',
    },
    unit: 'ns',
  },
};

const processFilter = {
  id: 'process',
  type: 'process_filter',
  title: 'Processes',
  rendererId: 'processes_and_threads',
  description: 'Include data from a selected process.',
  parameterBindings: {
    pid: 'filter_pid',
    // changing pid should clear tid
    tid: 'filter_tid',
  },
  config: {
    data_source: {
      tables: {
        processes: [
          { renderer_id: 'processes_and_threads', output: 'processes' },
        ],
      },
    },
    optionsQuery: {
      dataSource: 'processes',
      query:
        'SELECT CAST(pid AS INTEGER) AS pid, name FROM __table__ ORDER BY pid',
      tableNamePlaceholder: '__table__',
    },
  },
};

const threadFilter = {
  id: 'thread',
  type: 'thread_filter',
  title: 'Threads',
  rendererId: 'processes_and_threads',
  description: 'Include data from a selected thread.',
  parameterBindings: {
    // the process selected by `processFilter` is an input to thread filtering
    // as only a thread from that process can be selected
    pid: 'filter_pid',
    tid: 'filter_tid',
  },
  config: {
    data_source: {
      tables: {
        threads: [{ renderer_id: 'processes_and_threads', output: 'threads' }],
      },
    },
    optionsQuery: {
      dataSource: 'threads',
      query:
        'SELECT CAST(pid AS INTEGER) AS pid, CAST(tid AS INTEGER) AS tid, name FROM __table__ ORDER BY pid, tid',
      tableNamePlaceholder: '__table__',
    },
  },
};

const coreTypeFilter = {
  id: 'core_type',
  type: 'single_selection_list_filter',
  title: 'Core type',
  rendererId: 'supported_core_types',
  description: 'Include data from a selected CPU core type.',
  parameterBindings: {
    value: CORE_TYPE_RENDER_PARAMETER,
  },
  config: {
    data_source: {
      tables: {
        coreTypes: [
          {
            renderer_id: 'supported_core_types',
            output: 'supported_core_types',
          },
        ],
      },
    },
    optionsQuery: {
      dataSource: 'coreTypes',
      query:
        'SELECT name AS value, name AS label FROM __table__ GROUP BY name ORDER BY MIN(core_number), name',
      tableNamePlaceholder: '__table__',
    },
    allowNone: false,
    emptyMessage: 'No supported core types are available for this run.',
  },
};

function enableFilterIfAvailable(filter, runDescription, hasRichCapture) {
  // Treat missing parameters as disabled; only an explicit true enables global filtering.
  const richDataCaptureEnabled =
    runDescription.Parameters.rich_data_capture === true ||
    runDescription.Parameters.include_raw_data === true;

  if (!richDataCaptureEnabled) {
    return {
      ...filter,
      disabled: {
        reason:
          'Global filtering is unavailable for this run. Re-run the recipe with "Collect rich data" enabled.',
      },
    };
  }
  if (!runDescription.IsRunPhaseTwoComplete) {
    return {
      ...filter,
      disabled: {
        reason: runDescription.IsRunInProgress
          ? 'Unavailable until all capture data has been retrieved from the target.'
          : 'Unavailable because the run ended before all capture data was retrieved from the target.',
      },
    };
  }
  if (!hasRichCapture) {
    return {
      ...filter,
      disabled: {
        reason:
          'Global filtering is unavailable because this run has no retained rich capture data.',
      },
    };
  }
  return filter;
}

function enableCoreTypeFilterIfAvailable(
  filter,
  runDescription,
  selectedCoreType,
  hasRichCapture,
) {
  if (
    runDescription.Parameters.rich_data_capture !== true &&
    runDescription.Parameters.include_raw_data !== true
  ) {
    return {
      ...filter,
      disabled: {
        reason: `This run contains data for ${selectedCoreType} only. Re-run the recipe with "Collect rich data" enabled to view other core types.`,
      },
    };
  }
  return enableFilterIfAvailable(filter, runDescription, hasRichCapture);
}

/**
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 */
function renderCPUMicroarchitecture(context) {
  const runTools = context
    .getRunDescriptions()
    .map((run) =>
      run.ToolsUsed?.includes(TOOL_WPERF.name) ? TOOL_WPERF.name : 'neoprof',
    );
  if (new Set(runTools).size > 1)
    throw {
      code: readinessMessageCode,
      metadata: {
        message:
          'Compare CPU Microarchitecture runs collected with the same tool.',
      },
    };
  const tool_name = runTools[0];
  const windowsCapture = tool_name === TOOL_WPERF.name;
  const isComparison = context.getRunDescriptions().length === 2;
  const runDescription = context.getRunDescriptions()[0];
  const supportsCoreTypeFiltering =
    context.isRerenderingEnabled() && !isComparison && !windowsCapture;
  const hasRichCapture =
    supportsCoreTypeFiltering &&
    context
      .listRunComponents(0, `tool/${tool_name}/0/**`)
      .some((component) => component.componentType.name === 'capture_apc');
  const coreTypeFilterIsAvailable =
    supportsCoreTypeFiltering &&
    (runDescription.Parameters.rich_data_capture === true ||
      runDescription.Parameters.include_raw_data === true) &&
    runDescription.IsRunPhaseTwoComplete &&
    hasRichCapture;
  if (supportsCoreTypeFiltering) {
    context.setDefaultRenderParameter(
      CORE_TYPE_RENDER_PARAMETER,
      context.getFirstSupportedCpuName(0),
    );
  }

  const filterPid = getRenderParameterIfExists(context, 'filter_pid');
  const filterTid = getRenderParameterIfExists(context, 'filter_tid');
  const filterStartTimeNs = getRenderParameterIfExists(
    context,
    'filter_start_time_ns',
  );
  const filterEndTimeNs = getRenderParameterIfExists(
    context,
    'filter_end_time_ns',
  );
  let filterCoreType = null;
  if (supportsCoreTypeFiltering) {
    filterCoreType =
      context.getRenderParameter(CORE_TYPE_RENDER_PARAMETER) || null;
  }
  let noDataMessageConfig = {};

  let renderers = [];
  let visualizations = [];

  const dataSourceSingle = {
    tables: {
      symbols: [{ renderer_id: 'streamline_symbols', output: 'symbols' }],
      images: [{ renderer_id: 'streamline_symbols', output: 'images' }],
      target_info_cpus: [
        { renderer_id: 'target_info', output: 'target_info_cpus' },
      ],
    },
  };

  const dataSourceComparison = {
    tables: {
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
      target_info_cpus: [
        {
          renderer_id: 'target_info',
          output: 'target_info_cpus',
          content_index: 0,
        },
        {
          renderer_id: 'target_info',
          output: 'target_info_cpus',
          content_index: 1,
        },
      ],
    },
  };

  const sourceFilesSingle = {
    tables: {
      source_files: [
        { renderer_id: 'streamline_symbols', output: 'source_files' },
      ],
    },
  };

  const sourceFilesComparison = {
    tables: {
      source_files: [
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 1,
        },
      ],
    },
  };

  const disassemblySingle = {
    tables: {
      source_files: [
        { renderer_id: 'streamline_symbols', output: 'source_files' },
      ],
      images: [{ renderer_id: 'streamline_symbols', output: 'images' }],
      symbols: [{ renderer_id: 'streamline_symbols', output: 'symbols' }],
    },
  };

  const disassemblyComparison = {
    tables: {
      source_files: [
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
    },
  };

  const dataSource = isComparison ? dataSourceComparison : dataSourceSingle;
  const sourceFiles = isComparison ? sourceFilesComparison : sourceFilesSingle;
  const disassembly = isComparison ? disassemblyComparison : disassemblySingle;
  const slAnalyzeRerenderDependency = [{ renderer_id: 'sl_analyze' }];
  let isSlAnalyzeRerendering = false;

  function withSlAnalyzeRerenderDependency(dataSource) {
    return isSlAnalyzeRerendering
      ? { ...dataSource, renderers: slAnalyzeRerenderDependency }
      : dataSource;
  }

  const dataSourceCompareDrilldownStacks = {
    tables: {
      drilldown: [
        { renderer_id: 'drilldown', output: 'drilldown', content_index: 0 },
        { renderer_id: 'drilldown', output: 'drilldown', content_index: 1 },
      ],
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
    },
  };

  const dataSourceCompareDrilldownFlat = {
    tables: {
      drilldown: [
        { renderer_id: 'flat', output: 'drilldown', content_index: 0 },
        { renderer_id: 'flat', output: 'drilldown', content_index: 1 },
      ],
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
    },
  };

  const filters = [];
  renderers.push({
    type: 'TargetInfoRenderer',
    id: 'target_info',
  });
  if (supportsCoreTypeFiltering) {
    const slAnalyzeConfig = { entity: `tool/${tool_name}/0/` };
    let isFiltering = false;
    if (filterTid !== null && Number.isFinite(filterTid)) {
      slAnalyzeConfig.filter_tid = filterTid;
      isFiltering = true;
    } else if (filterPid !== null && Number.isFinite(filterPid)) {
      slAnalyzeConfig.filter_pid = filterPid;
      isFiltering = true;
    }
    if (
      filterStartTimeNs !== null &&
      Number.isFinite(filterStartTimeNs) &&
      filterStartTimeNs >= 0
    ) {
      slAnalyzeConfig.filter_start_time_ns = Math.round(filterStartTimeNs);
      isFiltering = true;
    }
    if (
      filterEndTimeNs !== null &&
      Number.isFinite(filterEndTimeNs) &&
      filterEndTimeNs >= 0
    ) {
      slAnalyzeConfig.filter_end_time_ns = Math.round(filterEndTimeNs);
      isFiltering = true;
    }
    if (coreTypeFilterIsAvailable && filterCoreType !== null) {
      slAnalyzeConfig.filter_core_type = filterCoreType;
      slAnalyzeConfig.data_source = {
        tables: {
          target_info_cpus: [
            { renderer_id: 'target_info', output: 'target_info_cpus' },
          ],
        },
      };
      isFiltering = true;
    }

    if (isFiltering) {
      noDataMessageConfig = {
        noDataMessage:
          'No samples match the selected filter values. Try loosening or clearing filters.',
      };
    }

    renderers.push(
      {
        type: 'SlAnalyzeRenderer',
        id: 'sl_analyze',
        config: slAnalyzeConfig,
      },
      {
        type: 'ProcessesAndThreadsParser',
        id: 'processes_and_threads',
        config: { entity: `tool/${tool_name}/0/` },
      },
      {
        type: 'TimeRangeParser',
        id: 'time_range',
        config: { entity: `tool/${tool_name}/0/` },
      },
      {
        type: 'SupportedCoreTypes',
        id: 'supported_core_types',
        config: {
          data_source: {
            tables: {
              target_info_cpus: [
                { renderer_id: 'target_info', output: 'target_info_cpus' },
              ],
            },
          },
        },
      },
    );
    isSlAnalyzeRerendering = true;
    filters.push(
      enableCoreTypeFilterIfAvailable(
        coreTypeFilter,
        runDescription,
        filterCoreType,
        hasRichCapture,
      ),
    );
    filters.push(
      enableFilterIfAvailable(timeRangeFilter, runDescription, hasRichCapture),
    );
    filters.push(
      enableFilterIfAvailable(processFilter, runDescription, hasRichCapture),
    );
    filters.push(
      enableFilterIfAvailable(threadFilter, runDescription, hasRichCapture),
    );
  }

  renderers.push(
    {
      type: 'StreamlineAnalyzeSymbols',
      id: 'streamline_symbols',
      config: {
        entity: `tool/${tool_name}/0/`,
        data_source: withSlAnalyzeRerenderDependency({}),
      },
    },
    {
      type: 'StreamlineAnalyzeFlatFunctions2',
      id: 'flat',
      config: {
        'compute-metrics': [
          {
            type: 'percentage',
            'total-from': 'Sample Count (self)',
            columns: ['Sample Count (self)'],
            'relative-order-priority': 'higher',
          },
        ],
        data_source: withSlAnalyzeRerenderDependency(dataSource),
        entity: `tool/${tool_name}/0/`,
        ...(filterCoreType === null ? {} : { cpu_name: filterCoreType }),
      },
    },
    {
      type: 'StreamlineAnalyzeFunctionProfileRenderer2',
      id: 'drilldown',
      config: {
        data_source: withSlAnalyzeRerenderDependency(dataSource),
        entity: `tool/${tool_name}/0/`,
        ...(filterCoreType === null ? {} : { cpu_name: filterCoreType }),
      },
    },
    {
      type: 'SourceCodeAttribution',
      id: 'source_code_attribution',
      config: {
        data_source: withSlAnalyzeRerenderDependency(sourceFiles),
        entity: `tool/${tool_name}/0/`,
      },
    },
    {
      type: 'DisassemblyRenderer',
      id: 'disassembly',
      config: {
        entity: `tool/${tool_name}/0/`,
        data_source: withSlAnalyzeRerenderDependency(disassembly),
      },
    },
  );
  visualizations.push(
    isComparison
      ? {
          type: 'topdown_node_graph_comparison',
          id: 'node_graph',
          rendererId: 'compare_drilldown',
          title: 'Summary Comparison',
          description:
            'Compare changes in the frontend, backend, retiring, and bad speculation metrics between the current run and the baseline.',
          config: {
            data_source: {
              tables: {
                callstackDeltas: [
                  { renderer_id: 'compare_drilldown', output: 'delta' },
                ],
                callstackMeasurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                callstackMeasurementOrder: [
                  { renderer_id: 'drilldown', output: 'measurement_order' },
                ],
                flatFunctionsCurrentRun: [
                  {
                    renderer_id: 'flat',
                    output: 'drilldown',
                    content_index: 1,
                  },
                ],
                flatFunctionsBaselineRun: [
                  {
                    renderer_id: 'flat',
                    output: 'drilldown',
                    content_index: 0,
                  },
                ],
                flatFunctionsDeltas: [
                  { renderer_id: 'compare_flat', output: 'delta_flat' },
                ],
                flatFunctionsMeasurements: [
                  { renderer_id: 'flat', output: 'measurements' },
                ],
                flatFunctionsMeasurementOrder: [
                  { renderer_id: 'flat', output: 'measurement_order' },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                    content_index: 1,
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
                    content_index: 1,
                  },
                ],
              },
            },
          },
        }
      : {
          type: 'topdown_node_graph',
          id: 'node_graph',
          rendererId: 'drilldown',
          title: 'Summary',
          description: windowsCapture
            ? 'Estimated CPU metrics from WindowsPerf samples across all cores. Multiplexing uses nominal exposure. Missing measurements remain unavailable.'
            : 'View a summary of frontend, backend, retiring, and bad speculation metrics.',
          config: {
            ...noDataMessageConfig,
            data_source: {
              tables: {
                callstack: [{ renderer_id: 'drilldown', output: 'drilldown' }],
                callstackMeasurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                callstackMeasurementOrder: [
                  { renderer_id: 'drilldown', output: 'measurement_order' },
                ],
                flatFunctions: [{ renderer_id: 'flat', output: 'drilldown' }],
                flatFunctionsMeasurements: [
                  { renderer_id: 'flat', output: 'measurements' },
                ],
                flatFunctionsMeasurementOrder: [
                  { renderer_id: 'flat', output: 'measurement_order' },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
                  },
                ],
              },
            },
          },
        },
    isComparison
      ? {
          type: 'flat_functions_comparison',
          id: 'functions',
          rendererId: 'compare_flat',
          title: 'Functions Comparison',
          description:
            'Comparison of how much CPU time different functions comsume, and of other per-function performance metrics.',
          config: {
            data_source: {
              tables: {
                flatFunctionsCurrentRun: [
                  {
                    renderer_id: 'flat',
                    output: 'drilldown',
                    content_index: 1,
                  },
                ],
                flatFunctionsBaselineRun: [
                  {
                    renderer_id: 'flat',
                    output: 'drilldown',
                    content_index: 0,
                  },
                ],
                deltas: [{ renderer_id: 'compare_flat', output: 'delta_flat' }],
                measurements: [{ renderer_id: 'flat', output: 'measurements' }],
                measurementOrder: [
                  { renderer_id: 'flat', output: 'measurement_order' },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                    content_index: 1,
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
                    content_index: 1,
                  },
                ],
                source_files: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'source_files',
                    content_index: 1,
                  },
                ],
              },
            },
          },
        }
      : {
          type: 'flat_functions',
          id: 'functions',
          rendererId: 'flat',
          title: 'Functions',
          description: 'Identify functions that consume the most CPU time.',
          config: {
            ...noDataMessageConfig,
            data_source: {
              tables: {
                flatFunctions: [{ renderer_id: 'flat', output: 'drilldown' }],
                measurements: [{ renderer_id: 'flat', output: 'measurements' }],
                measurementOrder: [
                  { renderer_id: 'flat', output: 'measurement_order' },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
                  },
                ],
                source_files: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'source_files',
                  },
                ],
              },
            },
          },
        },
    isComparison
      ? {
          type: 'call_stack_comparison',
          id: 'call_stack',
          rendererId: 'compare_drilldown',
          title: 'Call Stack Comparison',
          description:
            'Compare performance metrics grouped by call path between runs.',
          config: {
            data_source: {
              tables: {
                deltas: [{ renderer_id: 'compare_drilldown', output: 'delta' }],
                measurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                measurementOrder: [
                  { renderer_id: 'drilldown', output: 'measurement_order' },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                    content_index: 1,
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
                    content_index: 1,
                  },
                ],
                source_files: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'source_files',
                    content_index: 1,
                  },
                ],
              },
            },
          },
        }
      : {
          type: 'call_stack',
          id: 'call_stack',
          rendererId: 'drilldown',
          title: 'Call Stack',
          description:
            'Investigate performance metrics grouped by call path. This information helps you analyze where and how functions are used during execution as well as their costs.',
          config: {
            ...noDataMessageConfig,
            data_source: {
              tables: {
                drilldown: [{ renderer_id: 'drilldown', output: 'drilldown' }],
                measurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                measurementOrder: [
                  { renderer_id: 'drilldown', output: 'measurement_order' },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
                  },
                ],
                source_files: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'source_files',
                  },
                ],
              },
            },
          },
        },
  );

  if (isComparison) {
    renderers.push(
      {
        type: 'CompareDrilldownCallStacks',
        id: 'compare_drilldown',
        config: { data_source: dataSourceCompareDrilldownStacks },
      },
      {
        type: 'CompareDrilldownFlat',
        id: 'compare_flat',
        config: { data_source: dataSourceCompareDrilldownFlat },
      },
    );
  }
  return {
    renderers: windowsCapture
      ? renderers.filter((renderer) => renderer.id !== 'disassembly')
      : renderers,
    ui: {
      visualizations,
      side_panel_filters:
        isComparison && context.isRerenderingEnabled() && !windowsCapture
          ? getComparisonFilters(context)
          : filters,
    },
  };
}

const renderParameters = [
  {
    id: 'filter_pid',
    config: {
      type: 'number',
    },
  },
  {
    id: 'filter_tid',
    config: {
      type: 'number',
    },
  },
  {
    id: 'filter_start_time_ns',
    config: {
      type: 'number',
    },
  },
  {
    id: 'filter_end_time_ns',
    config: {
      type: 'number',
    },
  },
  {
    id: CORE_TYPE_RENDER_PARAMETER,
    config: {
      type: 'string',
    },
  },
];

module.exports = {
  id: 'cpu_microarchitecture',
  renderParameters: renderParameters,
  render: renderCPUMicroarchitecture,
  enableFilterIfAvailable,
};
