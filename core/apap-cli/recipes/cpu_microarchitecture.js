// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// CPU Microarchitecture Recipe Definition

// @ts-check
let tool_name = 'neoprof';
let tool_version = '1.1.0';
const CORE_TYPE_RENDER_PARAMETER = 'filter_core_type';
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
    {
      id: CORE_TYPE_RENDER_PARAMETER,
      config: {
        type: 'string',
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
const softLockupRiskMessageCode =
  'recipes.cpu_microarchitecture.SOFT_LOCKUP_RISK';
const highCoreCountWarningThreshold = 100;

/**
 * @typedef {'none' | 'voluntary' | 'full' | 'lazy' | 'rt' | 'unknown'} PreemptionMode
 */

/**
 * @param {string} kernelConfig
 * @returns {PreemptionMode}
 */
function getConfiguredPreemptionMode(kernelConfig) {
  const enabled = (option) =>
    new RegExp(`^CONFIG_${option}=y$`, 'm').test(kernelConfig);

  if (enabled('PREEMPT_RT')) {
    return 'rt';
  }
  if (enabled('PREEMPT_NONE')) {
    return 'none';
  }
  if (enabled('PREEMPT_VOLUNTARY')) {
    return 'voluntary';
  }
  if (enabled('PREEMPT_LAZY')) {
    return 'lazy';
  }
  if (enabled('PREEMPT')) {
    return 'full';
  }
  return 'unknown';
}

/**
 * @param {string} kernelCmdline
 * @returns {PreemptionMode | undefined}
 */
function getPreemptionOverride(kernelCmdline) {
  /** @type {PreemptionMode | undefined} */
  let override;
  for (const argument of kernelCmdline.trim().split(/\s+/)) {
    const match = argument.match(/^preempt=(none|voluntary|full|lazy)$/);
    if (match) {
      override = /** @type {PreemptionMode} */ (match[1]);
    }
  }
  return override;
}

/**
 * @param {string} runtimePreemption
 * @returns {PreemptionMode | undefined}
 */
function getRuntimePreemptionMode(runtimePreemption) {
  const match = runtimePreemption.match(/\((none|voluntary|full|lazy)\)/);
  return match ? /** @type {PreemptionMode} */ (match[1]) : undefined;
}

/**
 * Determine the effective kernel preemption mode if possible
 *
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @returns {PreemptionMode}
 */
function detectEffectivePreemptionMode(context) {
  let configResult;
  try {
    configResult = context.runCommand({
      type: 'exec',
      cmd: "grep -E '^CONFIG_PREEMPT(_[A-Z0-9_]+)?=y$' /boot/config-$(uname -r)",
    });
  } catch (_) {
    return 'unknown';
  }

  if (configResult.ReturnCode !== 0) {
    return 'unknown';
  }

  const configuredMode = getConfiguredPreemptionMode(configResult.Stdout);
  if (!/^CONFIG_PREEMPT_DYNAMIC=y$/m.test(configResult.Stdout)) {
    return configuredMode;
  }

  // Dynamic preemption can be changed after boot. Prefer the current runtime
  // mode when debugfs is mounted, then fall back to the boot configuration.
  try {
    const debugfsResult = context.runCommand({
      type: 'exec',
      cmd: "grep -qsE '^[^ ]+ /sys/kernel/debug debugfs( |$)' /proc/mounts",
    });

    // If debugfs is mounted, then look for dynamic preemption using sudo
    if (debugfsResult.ReturnCode === 0) {
      const runtimeResult = context.runCommand({
        type: 'exec',
        cmd: 'cat /sys/kernel/debug/sched/preempt',
        runAsAdmin: true,
      });
      if (runtimeResult.ReturnCode === 0) {
        const runtimeMode = getRuntimePreemptionMode(runtimeResult.Stdout);
        if (runtimeMode) {
          return runtimeMode;
        }
      }
    }
  } catch (_) {
    // Fall back to the kernel command line and configured default.
  }

  let cmdlineResult;
  try {
    cmdlineResult = context.runCommand({
      type: 'exec',
      cmd: "if [ -r /proc/cmdline ]; then tr ' ' '\\n' < /proc/cmdline | sed -n '/^preempt=/p'; else exit 1; fi",
    });
  } catch (_) {
    return configuredMode;
  }

  if (cmdlineResult.ReturnCode !== 0) {
    return configuredMode;
  }

  return getPreemptionOverride(cmdlineResult.Stdout) ?? configuredMode;
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @param {import("./docs/jsdocs").Workload} workload
 * @returns {import("./docs/jsdocs").RecipeReadyAdvice | undefined}
 */
function getSoftLockupRiskAdvice(context, workload) {
  const targetInfo = context.targetInfo();
  const cpuCount = targetInfo.CPUs.length;
  if (
    targetInfo.Os.OSFamily.toLowerCase() !== 'linux' ||
    workload.Type !== 'launch' ||
    cpuCount < highCoreCountWarningThreshold
  ) {
    return undefined;
  }

  const preemptionMode = detectEffectivePreemptionMode(context);
  if (preemptionMode !== 'none') {
    return undefined;
  }

  return {
    ToolName: '',
    AdviceSeverity: 'warning',
    MessageCode: softLockupRiskMessageCode,
    Metadata: {},
    Cause: '',
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function getFirstSupportedCPUTelemetry(context) {
  const targetInfo = context.targetInfo();
  const cpus = [...targetInfo.CPUs].sort(
    (left, right) => left.CoreNumber - right.CoreNumber,
  );

  for (const cpu of cpus) {
    const telemetrySpecification = context.getTelemetrySpecification(cpu.Name);
    if (telemetrySpecification) {
      return {
        cpuName: cpu.Name,
        coreNumbers: cpus
          .filter((candidate) => candidate.Name === cpu.Name)
          .map((candidate) => candidate.CoreNumber),
        telemetrySpecification,
      };
    }
  }

  return {
    cpuName: targetInfo.PrimaryCPUName,
    coreNumbers: [],
    telemetrySpecification: undefined,
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function getPMUSpec(context) {
  const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);
  if (!firstSupportedCPUTelemetry.telemetrySpecification) {
    throw new Error(
      `Telemetry specification for ${firstSupportedCPUTelemetry.cpuName} is unavailable`,
    );
  }

  return JSON.parse(firstSupportedCPUTelemetry.telemetrySpecification);
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
  const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);
  if (!firstSupportedCPUTelemetry.telemetrySpecification) {
    return {
      status: 'error',
      advice: [
        {
          ToolName: tool_name,
          AdviceSeverity: 'error',
          MessageCode: telemetrySpecificationUnavailableMessageCode,
          Metadata: { cpuName: firstSupportedCPUTelemetry.cpuName },
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
    filter_core_numbers: firstSupportedCPUTelemetry.coreNumbers.join(','),
  };

  let tools = generateNeoprofConfig(workload, params);
  let toolResponses = context.probeTools(tools);

  let allAdvice = collectToolAdvice(tools, toolResponses);
  const softLockupRiskAdvice = getSoftLockupRiskAdvice(context, workload);
  if (softLockupRiskAdvice) {
    allAdvice.push(softLockupRiskAdvice);
  }

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function computeValidValues(context) {
  const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);
  if (!firstSupportedCPUTelemetry.telemetrySpecification) {
    return [];
  }
  const pmuSpec = JSON.parse(firstSupportedCPUTelemetry.telemetrySpecification);

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
  const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);
  let metrics_group = getMetricsGroup(context);
  let params = {
    mode: 'metrics',
    metrics_group: metrics_group,
    sampling_frequency: sampling_freq,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
    rich_data_capture: context.getParameter('rich_data_capture'),
    filter_core_numbers: firstSupportedCPUTelemetry.coreNumbers.join(','),
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
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 */
function renderCPUMicroarchitecture(context) {
  const isComparison = context.getRunDescriptions().length === 2;
  const runDescription = context.getRunDescriptions()[0];
  const supportsCoreTypeFiltering =
    context.isRerenderingEnabled() && !isComparison;
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
          description:
            'View a summary of frontend, backend, retiring, and bad speculation metrics.',
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
    renderers,
    ui: {
      visualizations,
      side_panel_filters: filters,
    },
  };
}
