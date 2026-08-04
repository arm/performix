// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Instruction Mix Recipe Definition

// @ts-check

const PY_IM_VERSION = '0.4.5';

let tool_neoprof_name = 'neoprof';
let tool_imix_name = 'instruction_mix';
let tool_neoprof_version = '1.1.0';
let tool_imix_version = '1.1.0';

// Temporary fix until we support recipes defining whether they are custom or not
let customRecipe = false;
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;
const telemetrySpecificationUnavailableMessageCode =
  'recipes.instruction_mix.TELEMETRY_SPECIFICATION_UNAVAILABLE';

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'instruction_mix',
  title: 'Instruction Mix',
  version: '1.0',
  api_version: '1.0.2',
  status: 'stable',
  description:
    'The Instruction Mix recipe shows a breakdown of the types and proportions of instructions in your code. It helps you confirm that your code takes full advantage of Arm-specific features and is especially useful for checking compiler output when porting from x86.',
  parameters: [
    {
      id: 'sampling_freq',
      required: false,
      label: 'Sampling Frequency',
      description:
        "Select the sampling frequency. The 'normal' frequency is suitable for most workloads, while 'high' provides more detailed information at the cost of increased overhead.",
      config: {
        type: 'single_select',
        options: [
          { value: 'low', label: 'Low' },
          { value: 'normal', label: 'Normal' },
          { value: 'high', label: 'High' },
        ],
        defaultValue: 'normal',
      },
    },
    {
      id: 'mode',
      required: false,
      label: 'Mode',
      description:
        "Select the mode for the instruction mix analysis. 'Dynamic' runs dynamic sampling analysis, 'static' runs the static binary analysis, and 'both' runs both tools.",
      config: {
        type: 'radio',
        options: [
          { value: 'dynamic', label: 'Dynamic' },
          { value: 'static', label: 'Static' },
          { value: 'both', label: 'Both' },
        ],
        defaultValue: 'both',
      },
    },
    {
      id: 'collect_java_stacks',
      required: false,
      label: 'Collect Java stacks',
      description:
        'Enable collection of Java stack traces when profiling JVM workloads.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'collect_dotnet_stacks',
      required: false,
      label: 'Collect .NET stacks',
      description:
        'Enable collection of .NET stack traces when profiling .NET workloads.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'rich_data_capture',
      required: false,
      label: 'Collect rich data',
      description: `Enables the collection of rich data from the target, which enables advanced filtering functionality after the run completes. This can significantly increase host storage usage and transfer time.`,
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
  ],
  renderParameters: [
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
  ],
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool',
          name: tool_imix_name,
          version: tool_imix_version,
          requiredWhen: {
            type: 'param_is_not_set',
            parameters: [{ mode: 'dynamic' }],
          },
        },
        {
          type: 'tool',
          name: tool_neoprof_name,
          version: tool_neoprof_version,
          requiredWhen: {
            type: 'param_is_not_set',
            parameters: [{ mode: 'static' }],
          },
        },
      ],
    },
  ],
  readyStages: [
    {
      name: 'Checking Instruction Mix is ready',
      description:
        'Check that the target has the necessary dependencies to run the instruction mix tool (in agent mode)',
      exec: readyInstructionMix,
    },
  ],
  runStages: [
    {
      name: 'Validating recipe parameters',
      description:
        'This stage validates the user has specified appropriate parameter values',
      exec: validateRecipeParameters,
    },
    {
      name: 'Running instruction mix',
      description:
        'This stage runs dynamic and static instruction mix, and retrieves the files',
      exec: runInstructionMix,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderInstructionMix,
    },
  ],
};

/**
 * generateNeoprofToolConfig generates a ToolConfiguration for the
 * neoprof tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfiguration}
 */
function generateNeoprofToolConfig(workload, params) {
  return {
    name: tool_neoprof_name,
    params: params,
    workload: workload,
    env: {},
  };
}

/**
 * generateIMixToolConfig generates a ToolConfiguration for the
 * instruction_mix tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfiguration}
 */
function generateIMixToolConfig(workload, params) {
  return {
    name: tool_imix_name,
    params: params,
    workload: workload,
    env: {},
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @param {import("./docs/jsdocs").RecipeReadyAdvice[]} advice
 */
function addTelemetrySpecificationWarning(context, advice) {
  const cpuName = context.targetInfo().PrimaryCPUName;
  if (!context.getTelemetrySpecification(cpuName)) {
    advice.push({
      ToolName: '',
      AdviceSeverity: 'warning',
      MessageCode: telemetrySpecificationUnavailableMessageCode,
      Metadata: { cpuName },
      Cause: '',
    });
  }
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyInstructionMix(context) {
  let mode = context.getParameter('mode');
  let workload = context.getWorkload();

  /** @type {import("./docs/jsdocs").ToolConfigurationsArg} */
  let tools = { toolConfigs: [] };

  let runDynamic = mode === 'dynamic' || mode === 'both';
  if (runDynamic) {
    let samplingFreq = context.getParameter('sampling_freq');

    let params = {
      mode: 'metrics',
      metrics_group: 'operation_mix',
      sampling_frequency: samplingFreq,
      get_ipc_metric_name: true,
      collect_java_stacks: context.getParameter('collect_java_stacks'),
      collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
      rich_data_capture: context.getParameter('rich_data_capture'),
    };

    tools.toolConfigs.push(generateNeoprofToolConfig(workload, params));
  }

  let runStatic = mode === 'static' || mode === 'both';
  if (runStatic) {
    let params = {
      customRecipe: customRecipe,
    };

    tools.toolConfigs.push(generateIMixToolConfig(workload, params));
  }

  let toolResponses = context.probeTools(tools);

  let allAdvice = collectToolAdvice(tools, toolResponses);
  if (runDynamic) {
    addTelemetrySpecificationWarning(context, allAdvice);
  }

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function validateRecipeParameters(context) {
  let mode = context.getParameter('mode');
  let workload = context.getWorkload();

  if (mode === 'static' && context.getParameter('collect_java_stacks')) {
    throw {
      code: 'recipes.instruction_mix.JAVA_COLLECTION_STATIC_MODE',
      metadata: { mode: mode },
    };
  }

  if (mode !== 'dynamic') {
    if (workload.Type === 'systemWide') {
      throw {
        code: 'recipes.instruction_mix.NO_WORKLOAD_STATIC',
        metadata: { workloadType: 'system-wide' },
      };
    }

    if (workload.Type === 'attach') {
      throw {
        code: 'recipes.instruction_mix.NO_WORKLOAD_STATIC',
        metadata: { workloadType: 'PID attach' },
      };
    }
  }
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runInstructionMix(context) {
  let mode = context.getParameter('mode');
  let workload = context.getWorkload();

  /** @type {import("./docs/jsdocs").ToolConfigurationsArg} */
  let tools = { toolConfigs: [] };

  let runDynamic = mode === 'dynamic' || mode === 'both';
  if (runDynamic) {
    let samplingFreq = context.getParameter('sampling_freq');

    let params = {
      mode: 'metrics',
      metrics_group: 'operation_mix',
      sampling_frequency: samplingFreq,
      get_ipc_metric_name: true,
      collect_java_stacks: context.getParameter('collect_java_stacks'),
      collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
      rich_data_capture: context.getParameter('rich_data_capture'),
    };
    tools.toolConfigs.push(generateNeoprofToolConfig(workload, params));
  }

  let runStatic = mode === 'static' || mode === 'both';
  if (runStatic) {
    let params = {
      customRecipe: customRecipe,
    };

    tools.toolConfigs.push(generateIMixToolConfig(workload, params));
  }

  context.runTools(tools);
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
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

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
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
  const timeRangeNoDataMessage =
    filterStartTimeNs !== null || filterEndTimeNs !== null
      ? 'No samples match the selected time range. Try widening or clearing the time range filter.'
      : null;

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
  let renderers = [];
  let visualizations = [];
  const topBarFilters = [];

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
    if (context.isRerenderingEnabled() && !isComparison) {
      const slAnalyzeConfig = { entity: `tool/${tool_neoprof_name}/0/` };
      if (filterPid !== null && Number.isFinite(filterPid) && filterPid > 0) {
        slAnalyzeConfig.filter_pid = filterPid;
      }
      if (filterTid !== null && Number.isFinite(filterTid) && filterTid > 0) {
        slAnalyzeConfig.filter_tid = filterTid;
      }
      if (
        filterStartTimeNs !== null &&
        Number.isFinite(filterStartTimeNs) &&
        filterStartTimeNs >= 0
      ) {
        slAnalyzeConfig.filter_start_time_ns = Math.round(filterStartTimeNs);
      }
      if (
        filterEndTimeNs !== null &&
        Number.isFinite(filterEndTimeNs) &&
        filterEndTimeNs >= 0
      ) {
        slAnalyzeConfig.filter_end_time_ns = Math.round(filterEndTimeNs);
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
      );
      const runDescription = context.getRunDescriptions()[0];
      const renderTimeRangeFilter = { ...timeRangeFilter };

      // Treat a missing parameter as disabled; only an explicit true enables time-range filtering.
      const richDataCaptureEnabled =
        runDescription.Parameters.rich_data_capture === true;

      if (!richDataCaptureEnabled) {
        renderTimeRangeFilter.disabled = {
          reason:
            'Time-range filtering is unavailable for this run. re-run the Recipe with "Collect rich data" enabled.',
        };
      } else if (!runDescription.IsRunPhaseTwoComplete) {
        renderTimeRangeFilter.disabled = {
          reason: runDescription.IsRunInProgress
            ? 'Unavailable until all capture data has been retrieved from the target.'
            : 'Unavailable because the run ended before all capture data was retrieved from the target.',
        };
      }
      topBarFilters.push(renderTimeRangeFilter);
    }

    renderers.push(
      {
        type: 'StreamlineAnalyzeSymbols',
        id: 'streamline_symbols',
        config: { config: { entity: `tool/${tool_neoprof_name}/0/` } },
      },

      { type: 'TargetInfoRenderer', id: 'target_info' },
      {
        type: 'StreamlineAnalyzeFlatFunctions2',
        id: 'flat',
        config: {
          data_source: dataSource,
          entity: `tool/${tool_neoprof_name}/0/`,
        },
      },
      {
        type: 'StreamlineAnalyzeFunctionProfileRenderer2',
        id: 'drilldown',
        config: {
          data_source: dataSource,
          entity: `tool/${tool_neoprof_name}/0/`,
        },
      },
      {
        type: 'SourceCodeAttribution',
        id: 'source_code_attribution',
        config: {
          data_source: sourceFiles,
          entity: `tool/${tool_neoprof_name}/0/`,
        },
      },
      {
        type: 'DisassemblyRenderer',
        id: 'disassembly',
        config: {
          entity: `tool/${tool_neoprof_name}/0/`,
          data_source: disassembly,
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
              ...(timeRangeNoDataMessage
                ? { noDataMessage: timeRangeNoDataMessage }
                : {}),
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
              ...(timeRangeNoDataMessage
                ? { noDataMessage: timeRangeNoDataMessage }
                : {}),
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

  if (topBarFilters.length > 0) {
    return {
      renderers,
      ui: {
        visualizations,
        top_bar_filters: topBarFilters,
      },
    };
  }
  return { renderers, visualizations };
}
