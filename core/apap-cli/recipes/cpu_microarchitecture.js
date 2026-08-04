// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// CPU Microarchitecture Recipe Definition

// @ts-check
let tool_name = 'neoprof';
let tool_version = '1.1.0';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'cpu_microarchitecture',
  title: 'CPU Microarchitecture',
  version: '1.0',
  api_version: '1.0.2',
  status: 'stable',
  description:
    'The CPU Microarchitecture recipe helps you explore Arm CPU performance step by step. It gives you a broad, structured view of any bottlenecks and hotspots. You can then focus on the areas where optimization will have the most impact.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool',
          name: tool_name,
          version: tool_version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'metrics_group',
      required: false,
      label: 'Metrics Groups',
      description:
        'Select the metrics groups to use for the CPU Microarchitecture analysis. If not specified, all groups will be collected. Run recipe info to determine available groups on a target.',
      config: {
        type: 'multi_select',
        options: computeValidValues,
        defaultValue: [],
      },
    },
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
      id: 'collect_all',
      required: false,
      label: 'Collect all metrics',
      description:
        'If enabled, all metrics groups available on the target system will be collected. This is useful for broad analysis but may decrease individual group detail due to multiplexing.',
      config: {
        type: 'checkbox',
        defaultValue: false,
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
  readyStages: [
    {
      name: 'Checking recipe is ready',
      description:
        'Check that the target can run the cpu_microarchitecture recipe with the given metric groups',
      exec: readyCPUMicroarchitecture,
    },
  ],
  runStages: [
    {
      name: 'Collecting metric group(s)',
      description:
        'This stage collects metric group(s) on the target and processes the captured data',
      exec: runCPUMicroarchitecture,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderCPUMicroarchitecture,
    },
  ],
};

const telemetrySpecificationUnavailableMessageCode =
  'recipes.cpu_microarchitecture.TELEMETRY_SPECIFICATION_UNAVAILABLE';

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function getPrimaryCPUTelemetrySpecification(context) {
  const cpuName = context.targetInfo().PrimaryCPUName;
  return {
    cpuName,
    telemetrySpecification: context.getTelemetrySpecification(cpuName),
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function getPMUSpec(context) {
  const primaryCPUTelemetry = getPrimaryCPUTelemetrySpecification(context);
  if (!primaryCPUTelemetry.telemetrySpecification) {
    throw new Error(
      `Telemetry specification for ${primaryCPUTelemetry.cpuName} is unavailable`,
    );
  }

  return JSON.parse(primaryCPUTelemetry.telemetrySpecification);
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function getMetricsGroup(context) {
  let metricsGroup = context.getParameter('metrics_group').join(',');

  // If no metrics group is specified, or collect_all is enabled, collect all groups
  if (metricsGroup.length === 0 || context.getParameter('collect_all')) {
    const pmuSpec = getPMUSpec(context);
    let allMetricsGroups = Object.values(
      pmuSpec.methodologies.topdown_methodology.metric_grouping,
    ).flat();

    // sl-collect expects metrics groups to be lower case
    metricsGroup = allMetricsGroups.join(',');
  }
  metricsGroup = metricsGroup.toLowerCase();
  context.logInfo(`collecting metrics ${metricsGroup}`);

  return metricsGroup;
}

/**
 * generateNeoprofConfig generates a ToolConfigurationsArg for the
 * neoprof tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function generateNeoprofConfig(workload, params) {
  return {
    toolConfigs: [
      {
        name: tool_name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyCPUMicroarchitecture(context) {
  const primaryCPUTelemetry = getPrimaryCPUTelemetrySpecification(context);
  if (!primaryCPUTelemetry.telemetrySpecification) {
    return {
      status: 'error',
      advice: [
        {
          ToolName: tool_name,
          AdviceSeverity: 'error',
          MessageCode: telemetrySpecificationUnavailableMessageCode,
          Metadata: { cpuName: primaryCPUTelemetry.cpuName },
          Cause: '',
        },
      ],
    };
  }

  let workload = context.getWorkload();
  let samplingFreq = context.getParameter('sampling_freq');
  let metricsGroup = getMetricsGroup(context);
  let params = {
    mode: 'metrics',
    metrics_group: metricsGroup,
    sampling_frequency: samplingFreq,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
    rich_data_capture: context.getParameter('rich_data_capture'),
  };

  let tools = generateNeoprofConfig(workload, params);
  let toolResponses = context.probeTools(tools);

  let allAdvice = collectToolAdvice(tools, toolResponses);

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function computeValidValues(context) {
  const primaryCPUTelemetry = getPrimaryCPUTelemetrySpecification(context);
  if (!primaryCPUTelemetry.telemetrySpecification) {
    return [];
  }
  const pmuSpec = JSON.parse(primaryCPUTelemetry.telemetrySpecification);

  const valid_metrics_groups = new Set(
    Object.values(pmuSpec.methodologies.topdown_methodology.metric_grouping)
      .flat()
      .map((metric_group) => metric_group.toLowerCase()),
  );

  // Generate a list of unique metric groups from pmuSpec.methodologies.topdown_methodology.decision_tree.metrics
  // Include metrics.group and all items in metrics.next_items, as the main group is usually Topdown_L1,
  // which is also a valid choice.
  // Not using pmuSpec.methodologies.topdown_methodology.metric_grouping as it includes groups not in the decision tree
  // which do not display in the GUI summary view.
  let all_metrics_groups = new Set();
  for (let item of pmuSpec.methodologies.topdown_methodology.decision_tree
    .metrics) {
    all_metrics_groups.add(item.group);
    for (let next_item of item.next_items) {
      if (valid_metrics_groups.has(next_item.toLowerCase())) {
        all_metrics_groups.add(next_item);
      }
    }
  }
  all_metrics_groups = Array.from(all_metrics_groups);
  all_metrics_groups.sort();

  // sl-collect expects metrics groups to be lower case.
  return all_metrics_groups.map((metric_group) => ({
    value: metric_group.toLowerCase(),
    label: metric_group.toLowerCase(),
  }));
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runCPUMicroarchitecture(context) {
  const sampling_freq = context.getParameter('sampling_freq');
  const workload = context.getWorkload();
  let metrics_group = getMetricsGroup(context);
  let params = {
    mode: 'metrics',
    metrics_group: metrics_group,
    sampling_frequency: sampling_freq,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
    rich_data_capture: context.getParameter('rich_data_capture'),
  };
  context.runTools(generateNeoprofConfig(workload, params));
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
function renderCPUMicroarchitecture(context) {
  const isComparison = context.getRunDescriptions().length === 2;
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

  const topBarFilters = [];
  if (context.isRerenderingEnabled() && !isComparison) {
    const filterPid = getRenderParameterIfExists(context, 'filter_pid');
    const filterTid = getRenderParameterIfExists(context, 'filter_tid');
    const slAnalyzeConfig = { entity: `tool/${tool_name}/0/` };
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
        config: { entity: `tool/${tool_name}/0/` },
      },
      {
        type: 'TimeRangeParser',
        id: 'time_range',
        config: { entity: `tool/${tool_name}/0/` },
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
          'Time-range filtering is unavailable for this run. Re-run the recipe with "Collect rich data" enabled.',
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
      config: { entity: `tool/${tool_name}/0/` },
    },
    {
      type: 'TargetInfoRenderer',
      id: 'target_info',
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
        data_source: dataSource,
        entity: `tool/${tool_name}/0/`,
      },
    },
    {
      type: 'StreamlineAnalyzeFunctionProfileRenderer2',
      id: 'drilldown',
      config: {
        data_source: dataSource,
        entity: `tool/${tool_name}/0/`,
      },
    },
    {
      type: 'SourceCodeAttribution',
      id: 'source_code_attribution',
      config: {
        data_source: sourceFiles,
        entity: `tool/${tool_name}/0/`,
      },
    },
    {
      type: 'DisassemblyRenderer',
      id: 'disassembly',
      config: {
        entity: `tool/${tool_name}/0/`,
        data_source: disassembly,
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
          description:
            'View a summary of frontend, backend, retiring, and bad speculation metrics.',
          config: {
            ...(timeRangeNoDataMessage
              ? { noDataMessage: timeRangeNoDataMessage }
              : {}),
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
            ...(timeRangeNoDataMessage
              ? { noDataMessage: timeRangeNoDataMessage }
              : {}),
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
            ...(timeRangeNoDataMessage
              ? { noDataMessage: timeRangeNoDataMessage }
              : {}),
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
