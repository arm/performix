// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

let tool_neoprof_name = 'neoprof';
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
function renderInstructionMix(context) {
  let runDescriptions = context.getRunDescriptions();
  const isComparison = context.getRunDescriptions().length === 2;
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

  let noDataMessageConfig = {};

  const fallbackMode = 'dynamic';

  let mode = runDescriptions[0].Parameters.mode || fallbackMode;
  let canRender = runDescriptions.every(
    (run) => (run.Parameters.mode || fallbackMode) === mode,
  );

  // For now, comparing 'both' mode with 'static' or 'dynamic' modes isn't supported
  if (!canRender) {
    // No worry if index out of bounds as this can only be reached if there are two runs to be compared
    let mode2 = runDescriptions[1].Parameters.mode || fallbackMode;
    throw {
      code: 'recipes.instruction_mix.COMPARE_MISMATCHED_MODES',
      metadata: { mode1: mode, mode2: mode2 },
    };
  }

  let renderDynamic = mode === 'dynamic' || mode === 'both';
  let renderStatic = mode === 'static' || mode === 'both';
  const runDescription = runDescriptions[0];
  const supportsCoreTypeFiltering =
    renderDynamic && context.isRerenderingEnabled() && !isComparison;
  const hasRichCapture =
    supportsCoreTypeFiltering &&
    context
      .listRunComponents(0, `tool/${tool_neoprof_name}/0/**`)
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
  const filterCoreType = supportsCoreTypeFiltering
    ? (context.getRenderParameter(CORE_TYPE_RENDER_PARAMETER) ?? null)
    : null;
  let renderers = [];
  let visualizations = [];
  const filters = [];

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

  if (renderDynamic) {
    renderers.push({ type: 'TargetInfoRenderer', id: 'target_info' });
    if (context.isRerenderingEnabled() && !isComparison) {
      const slAnalyzeConfig = { entity: `tool/${tool_neoprof_name}/0/` };
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
      if (
        filterEndTimeNs !== null &&
        Number.isFinite(filterEndTimeNs) &&
        filterEndTimeNs >= 0
      ) {
        slAnalyzeConfig.filter_end_time_ns = Math.round(filterEndTimeNs);
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
          config: { entity: `tool/${tool_neoprof_name}/0/` },
        },
        {
          type: 'TimeRangeParser',
          id: 'time_range',
          config: { entity: `tool/${tool_neoprof_name}/0/` },
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
        enableFilterIfAvailable(
          timeRangeFilter,
          runDescription,
          hasRichCapture,
        ),
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
          config: { entity: `tool/${tool_neoprof_name}/0/` },
          data_source: withSlAnalyzeRerenderDependency({}),
        },
      },
      {
        type: 'StreamlineAnalyzeFlatFunctions2',
        id: 'flat',
        config: {
          data_source: withSlAnalyzeRerenderDependency(dataSource),
          entity: `tool/${tool_neoprof_name}/0/`,
          ...(filterCoreType === null ? {} : { cpu_name: filterCoreType }),
        },
      },
      {
        type: 'StreamlineAnalyzeFunctionProfileRenderer2',
        id: 'drilldown',
        config: {
          data_source: withSlAnalyzeRerenderDependency(dataSource),
          entity: `tool/${tool_neoprof_name}/0/`,
          ...(filterCoreType === null ? {} : { cpu_name: filterCoreType }),
        },
      },
      {
        type: 'SourceCodeAttribution',
        id: 'source_code_attribution',
        config: {
          data_source: withSlAnalyzeRerenderDependency(sourceFiles),
          entity: `tool/${tool_neoprof_name}/0/`,
        },
      },
      {
        type: 'DisassemblyRenderer',
        id: 'disassembly',
        config: {
          entity: `tool/${tool_neoprof_name}/0/`,
          data_source: withSlAnalyzeRerenderDependency(disassembly),
        },
      },
    );

    visualizations.push(
      isComparison
        ? {
            type: 'instruction_mix_summary_comparison',
            id: 'instruction_mix',
            rendererId: 'compare_drilldown',
            title: 'Dynamic Analysis Comparison',
            description:
              'Compare the proportion of executed instructions in each category between runs. Note that the percentages across categories might not sum to 100% because the values are derived from sampled and categorized data.',
            config: {
              data_source: {
                tables: {
                  deltas: [
                    { renderer_id: 'compare_drilldown', output: 'delta' },
                  ],
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
                },
              },
            },
          }
        : {
            type: 'instruction_mix_summary',
            id: 'instruction_mix',
            rendererId: 'drilldown',
            title: 'Dynamic Analysis',
            description:
              'View the proportion of executed instructions in each category. Note that the percentages across categories might not sum to 100% because the values are derived from sampled and categorized data.',
            config: {
              ...noDataMessageConfig,
              data_source: {
                tables: {
                  drilldown: [
                    { renderer_id: 'drilldown', output: 'drilldown' },
                  ],
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
                },
              },
            },
          },
      isComparison
        ? {
            type: 'flat_functions_comparison',
            id: 'functions',
            rendererId: 'compare_flat',
            title: 'Dynamic Functions Comparison',
            description:
              'Compare how individual functions use different instruction types across runs, and how much CPU time different functions consume.',
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
                  deltas: [
                    { renderer_id: 'compare_flat', output: 'delta_flat' },
                  ],
                  measurements: [
                    { renderer_id: 'flat', output: 'measurements' },
                  ],
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
            title: 'Dynamic Functions',
            description:
              'View the CPU time and the proportion of instruction categories used for each function.',
            config: {
              ...noDataMessageConfig,
              data_source: {
                tables: {
                  flatFunctions: [{ renderer_id: 'flat', output: 'drilldown' }],
                  measurements: [
                    { renderer_id: 'flat', output: 'measurements' },
                  ],
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
  }

  if (renderStatic) {
    renderers.push({
      type: 'CSV',
      id: 'static_instruction_mix',
      config: {
        component: 'tool/instruction_mix/0/static_instruction_mix.csv',
      },
    });
    if (isComparison) {
      renderers.push({
        type: 'CompareFlatTable',
        id: 'static_instruction_mix_comparison',
        config: {
          data_source: {
            tables: {
              flat_tables: [
                {
                  renderer_id: 'static_instruction_mix',
                  output: 'csv',
                  content_index: 0,
                },
                {
                  renderer_id: 'static_instruction_mix',
                  output: 'csv',
                  content_index: 1,
                },
              ],
            },
          },
          join_columns: ['group'],
          fixed_columns: [],
          compare_columns: ['*'],
        },
      });
    }

    const staticAnalysisQuery =
      'SELECT "group" AS "Instruction group", percentage AS "% of total counts", instruction_count AS "Counts" FROM __table__;';
    const staticAnalysisComparisonQuery = `SELECT "group" AS "Instruction group", CASE WHEN percentage_delta IS NULL THEN printf('%.2f', CAST(percentage_2 AS DOUBLE)) ELSE printf('%.2f', CAST(percentage_2 AS DOUBLE)) || ' ' || printf('(%+.2f)', CAST(percentage_delta AS DOUBLE)) END AS "% of total counts", CASE WHEN instruction_count_delta IS NULL THEN CAST(instruction_count_2 AS VARCHAR) ELSE CAST(instruction_count_2 AS VARCHAR) || ' ' || printf('(%+d)', CAST(instruction_count_delta AS BIGINT)) END AS "Counts" FROM __table__;`;

    visualizations.push(
      isComparison
        ? {
            type: 'generic_grid',
            id: 'csv_static_instruction_mix',
            rendererId: 'static_instruction_mix_comparison',
            title: 'Static Disassembly Comparison',
            description:
              'Compare the proportion of instruction categories in each binary.',
            config: {
              autoSizeColumns: true,
              data_source: {
                tables: {
                  table: [
                    {
                      renderer_id: 'static_instruction_mix_comparison',
                      output: 'delta_flat_table',
                    },
                  ],
                },
              },
              customQuery: {
                query: staticAnalysisComparisonQuery,
                tableNamePlaceholder: '__table__',
              },
            },
          }
        : {
            type: 'generic_grid',
            id: 'csv_static_instruction_mix',
            rendererId: 'static_instruction_mix',
            title: 'Static Disassembly',
            description:
              'Investigate the proportion of instruction categories in each binary.',
            config: {
              autoSizeColumns: true,
              data_source: {
                tables: {
                  table: [
                    { renderer_id: 'static_instruction_mix', output: 'csv' },
                  ],
                },
              },
              customQuery: {
                query: staticAnalysisQuery,
                tableNamePlaceholder: '__table__',
              },
            },
          },
    );
  }

  return {
    renderers,
    ui: {
      visualizations,
      side_panel_filters:
        isComparison && renderDynamic && context.isRerenderingEnabled()
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
  id: 'instruction_mix',
  renderParameters: renderParameters,
  render: renderInstructionMix,
  enableFilterIfAvailable,
};
