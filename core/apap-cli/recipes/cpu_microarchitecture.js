// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// CPU Microarchitecture Recipe Definition

// @ts-check
const {
  render: renderCPUMicroarchitecture,
  renderParameters,
} = require('./analyses/cpu_microarchitecture');

const TOOL_WPERF = { name: 'wperf', version: '1.0.1' };
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

let tool_name = 'neoprof';
let tool_version = '1.1.0';
const {
  collectToolAdvice,
  toolStatusToRecipeStatus,
  getPartialCPUSupportAdvice,
} = recipeUtils;

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
    'Analyze CPU execution efficiency to identify bottlenecks, for example, pipeline stalls and branch mispredictions.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Windows' }],
      dependencies: [
        { type: 'tool', ...TOOL_WPERF, requiredWhen: { type: 'always' } },
      ],
    },
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
  renderParameters: renderParameters,
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
  const windowsTarget = isWindowsTarget(context.targetInfo());
  const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);
  if (!firstSupportedCPUTelemetry.telemetrySpecification) {
    return {
      status: 'error',
      advice: [
        {
          ToolName: windowsTarget ? TOOL_WPERF.name : tool_name,
          AdviceSeverity: 'error',
          MessageCode: telemetrySpecificationUnavailableMessageCode,
          Metadata: { cpuName: firstSupportedCPUTelemetry.cpuName },
          Cause: '',
        },
      ],
    };
  }

  if (windowsTarget) return readyWindowsMicroarchitecture(context);

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
  const partialCPUSupportAdvice = getPartialCPUSupportAdvice(
    context,
    'recipes.cpu_microarchitecture.PARTIAL_CPU_SUPPORT',
  );
  if (partialCPUSupportAdvice) {
    allAdvice.push(partialCPUSupportAdvice);
  }
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
  if (isWindowsTarget(context.targetInfo())) {
    const unsupported = unsupportedWindowsOptions(context);
    if (unsupported.length)
      throw {
        code: readinessMessageCode,
        metadata: { message: unsupported.join(' ') },
      };
    context.runTools(generateWindowsConfig(context));
    return;
  }
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

function isWindowsTarget(targetInfo) {
  return targetInfo.Os?.OSFamily?.toLowerCase() === 'windows';
}

function generateWindowsConfig(context) {
  const groups = context.getParameter('collect_all')
    ? ''
    : context.getParameter('metrics_group').join(',').toLowerCase();
  return {
    toolConfigs: [
      {
        name: TOOL_WPERF.name,
        workload: context.getWorkload(),
        env: {},
        params: {
          mode: 'metrics',
          metrics_group: groups,
          sampling_frequency: context.getParameter('sampling_freq'),
        },
      },
    ],
  };
}

function unsupportedWindowsOptions(context) {
  const messages = [];
  if (
    context.getParameter('collect_java_stacks') ||
    context.getParameter('collect_dotnet_stacks')
  ) {
    messages.push(
      'Managed-code stack collection is not supported by Windows CPU Microarchitecture.',
    );
  }
  if (context.getParameter('rich_data_capture')) {
    messages.push(
      'Rich capture and process, thread or time-range filtering are not supported by Windows CPU Microarchitecture.',
    );
  }
  return messages;
}

function readyWindowsMicroarchitecture(context) {
  const unsupported = unsupportedWindowsOptions(context);
  const tools = generateWindowsConfig(context);
  const responses = context.probeTools(tools);
  const advice = collectToolAdvice(tools, responses);
  for (const message of unsupported)
    advice.push({
      ToolName: TOOL_WPERF.name,
      AdviceSeverity: 'error',
      MessageCode: readinessMessageCode,
      Metadata: { message },
      Cause: '',
    });
  advice.push({
    ToolName: TOOL_WPERF.name,
    AdviceSeverity: 'warning',
    MessageCode: readinessMessageCode,
    Metadata: {
      message:
        'Windows CPU metrics are estimates from samples across all cores. Missing events stay unavailable. Multiplexing uses nominal exposure and sample loss is not corrected.',
    },
    Cause: '',
  });
  return { status: toolStatusToRecipeStatus(advice), advice };
}
