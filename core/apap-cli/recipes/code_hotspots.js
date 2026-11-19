// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Code Hotspots Recipe Definition

// @ts-check
const {
  render: renderHotspots,
  renderParameters,
  enableFilterIfAvailable,
} = require('./analyses/code_hotspots');

const TOOL_NEOPROF = { name: 'neoprof', version: '1.1.0' };
const TOOL_WPERF = { name: 'wperf', version: '1.0.1' };

const {
  CPU_CYCLES,
  COUNTER_CAPABILITY_COMPONENT_TYPE,
  NATIVE_CPU_COUNTER_CAPABILITY_COMPONENT_TYPE,
  NATIVE_CPU_COUNTER_ID_PREFIX,
} = require('../tool-integrations/tool_capabilities');
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const telemetrySpecificationUnavailableMessageCode =
  'recipes.code_hotspots.TELEMETRY_SPECIFICATION_UNAVAILABLE';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;
const performixGlobal = /** @type {import("./docs/jsdocs").PerformixGlobal} */ (
  globalThis['performix']
);

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'code_hotspots',
  title: 'Code Hotspots',
  version: '1.0',
  api_version: '1.0.2',
  status: 'stable',
  description:
    'Find the functions and source lines that consume the most CPU time.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Windows' }],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_WPERF.name,
          version: TOOL_WPERF.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Android' },
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_NEOPROF.name,
          version: TOOL_NEOPROF.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    ...(performixGlobal.customPMUCaptureEnabled
      ? [
          {
            id: 'cpu_event',
            required: false,
            label: 'CPU Events',
            description:
              'Select additional CPU PMU events available on the target.',
            config: {
              type: 'multi_select',
              options: cpuEventOptions,
              defaultValue: [],
              requiresTools: true,
            },
          },
        ]
      : []),
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
      id: 'collect_java_stacks',
      required: false,
      label: 'Collect Java stacks',
      description:
        'Collect Java stack traces for JVM workloads. Java Flight Recorder data is also collected when available.',
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
      id: 'reformat_on_host',
      required: false,
      label: 'Reformat on host',
      description:
        'Run analysis on the host instead of the target. This is always enabled for Android targets but can be optionally enabled for Linux targets. collect_java_stacks and collect_dotnet_stacks are not currently supported when reformat_on_host is enabled.',
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
    {
      id: 'collect_off_cpu_time',
      required: false,
      label: 'Collect off-CPU time',
      description: 'Collect time spent blocked or waiting off CPU.',
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
      description: 'Check that the target can run the Code Hotspots recipe',
      exec: readyHotspots,
    },
  ],
  runStages: [
    {
      name: 'Collecting hotspots data',
      description:
        'This stage collects hotspot samples on the target and processes the captured data',
      exec: runHotspots,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderHotspots,
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @returns {import("./docs/jsdocs").ParameterOptions[]}
 */
function cpuEventOptions(context) {
  const targetInfo = context.targetInfo();
  if (!isLinuxTarget(targetInfo) && !isAndroidTarget(targetInfo)) return [];

  const tools = generateNeoprofConfig(
    { Type: 'systemWide', Data: '' },
    { compute_cpu_pmu_events: true },
  );
  const [probe] = context.probeTools(tools);

  const options = [];
  for (const [id, capability] of Object.entries(probe.capabilities)) {
    const isNativeCPUCounter =
      capability.componentType?.name ===
      NATIVE_CPU_COUNTER_CAPABILITY_COMPONENT_TYPE.name;
    if (!isNativeCPUCounter || capability.state !== 'available') continue;
    const { payload } = capability;
    options.push({
      value: id.slice(NATIVE_CPU_COUNTER_ID_PREFIX.length),
      label: payload.title,
      description: payload.description,
      metadata: {
        pmuId: payload.pmu.id,
        pmuName: payload.pmu.name,
        eventTitle: payload.title,
        eventCode: payload.eventCode,
        category: payload.category ?? '',
      },
    });
  }
  return options.sort(
    (a, b) =>
      a.metadata.pmuId.localeCompare(b.metadata.pmuId) ||
      a.label.localeCompare(b.label) ||
      a.value.localeCompare(b.value),
  );
}

/**
 * @param {string} samplingFreq
 * @param {import("./docs/jsdocs").Workload} workload
 */
function getToolsArg(samplingFreq, workload) {
  return {
    tools: [
      {
        name: TOOL_NEOPROF.name,
        args: ['-r', samplingFreq],
      },
    ],
    workload: workload,
  };
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
        name: TOOL_NEOPROF.name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * generateWperfConfig generates a ToolConfigurationsArg for the
 * wperf tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function generateWperfConfig(workload, params) {
  return {
    toolConfigs: [
      {
        name: TOOL_WPERF.name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * Build the neoprof parameter set used by agent-mode invocations.
 * @param {import("./docs/jsdocs").RunExecutionContext|import("./docs/jsdocs").ReadyExecutionContext} context
 * @param {string} samplingFreq
 * @returns {Object.<string, any>}
 */
function buildNeoprofParams(context, samplingFreq) {
  const androidTarget = isAndroidTarget(context.targetInfo());
  const params = {
    mode: 'samples',
    export_timeline: true,
    sampling_frequency: samplingFreq,
    rich_data_capture: context.getParameter('rich_data_capture'),
    collect_off_cpu_time: context.getParameter('collect_off_cpu_time'),
    reformat_on_host: androidTarget || context.getParameter('reformat_on_host'),
  };

  if (!androidTarget) {
    const enableJavaAnalysis = context.getParameter('collect_java_stacks');
    params.collect_java_stacks = enableJavaAnalysis;
    params.collect_jfr = enableJavaAnalysis && context.isJfrCaptureEnabled();
    params.collect_dotnet_stacks = context.getParameter(
      'collect_dotnet_stacks',
    );
  }

  return params;
}

/**
 * Windows keeps sample-only collection until its CPU counter support is delivered.
 * @param {import("./docs/jsdocs").RunExecutionContext|import("./docs/jsdocs").ReadyExecutionContext} context
 * @returns {import("./docs/jsdocs").CapabilityRequest|undefined}
 */
function getCPUCapabilityRequest(context) {
  if (isWindowsTarget(context.targetInfo())) return undefined;
  const required = [CPU_CYCLES];
  if (performixGlobal.customPMUCaptureEnabled) {
    const events = context.getParameter('cpu_event') || [];
    required.push(...events.map((id) => NATIVE_CPU_COUNTER_ID_PREFIX + id));
  }
  return { required, optional: [] };
}

/**
 * Use the same invocation configuration and capability request in readiness and capture.
 * @param {import("./docs/jsdocs").RunExecutionContext|import("./docs/jsdocs").ReadyExecutionContext} context
 * @returns {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function buildHotspotsToolConfig(context) {
  const workload = context.getWorkload();
  const samplingFreq = context.getParameter('sampling_freq');
  const toolConfigurations = isWindowsTarget(context.targetInfo())
    ? generateWperfConfig(workload, buildWperfParams(samplingFreq))
    : generateNeoprofConfig(
        workload,
        buildNeoprofParams(context, samplingFreq),
      );
  const capabilityRequest = getCPUCapabilityRequest(context);
  if (capabilityRequest !== undefined) {
    toolConfigurations.toolConfigs[0].capabilityRequest = capabilityRequest;
  }
  return toolConfigurations;
}

/**
 * Build the wperf parameter set used by agent-mode invocations.
 * @param {string} samplingFreq
 * @returns {Object.<string, any>}
 */
function buildWperfParams(samplingFreq) {
  return {
    mode: 'samples',
    sampling_frequency: samplingFreq ? samplingFreq : 'normal',
  };
}

/**
 * Determine whether the current target OS is Windows.
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @returns {boolean}
 */
function isWindowsTarget(targetInfo) {
  const family = targetInfo?.Os?.OSFamily ?? '';
  return family.toLowerCase() === 'windows';
}

/**
 * Determine whether the current target OS is Android.
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @returns {boolean}
 */
function isAndroidTarget(targetInfo) {
  const family = targetInfo?.Os?.OSFamily ?? '';
  return family.toLowerCase() === 'android';
}

/**
 * Determine whether the current target OS is Linux.
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @returns {boolean}
 */
function isLinuxTarget(targetInfo) {
  const family = targetInfo?.Os?.OSFamily ?? '';
  return family.toLowerCase() === 'linux';
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
function readyHotspots(context) {
  const targetInfo = context.targetInfo();
  const hostReformatEnabled = context.getParameter('reformat_on_host');
  const collectJavaStacks = context.getParameter('collect_java_stacks');
  const enableJavaAnalysis = collectJavaStacks && context.isJfrCaptureEnabled();
  const collectJitDumpsEnabled =
    collectJavaStacks || context.getParameter('collect_dotnet_stacks');
  const richDataCaptureEnabled = context.getParameter('rich_data_capture');

  if (isWindowsTarget(targetInfo)) {
    const toolConfigurations = buildHotspotsToolConfig(context);
    const toolResponses = context.probeTools(toolConfigurations);
    const allAdvice = collectToolAdvice(toolConfigurations, toolResponses);
    if (collectJitDumpsEnabled) {
      allAdvice.push({
        ToolName: TOOL_WPERF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'JIT dump collection is not supported for Windows targets. Jitted symbols will not be available.',
        },
        Cause: '',
      });
    }
    if (enableJavaAnalysis) {
      allAdvice.push(javaAnalysisUnsupportedAdvice(TOOL_WPERF.name, 'Windows'));
    }
    if (hostReformatEnabled) {
      allAdvice.push({
        ToolName: TOOL_WPERF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'Reformatting on the host is not supported for Windows targets. Reformatting will be done on the target.',
        },
        Cause: '',
      });
    }
    if (richDataCaptureEnabled) {
      allAdvice.push({
        ToolName: TOOL_WPERF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'Rich data capture is not supported for Windows targets. Advanced filtering functionality will not be available.',
        },
        Cause: '',
      });
    }
    addTelemetrySpecificationWarning(context, allAdvice);
    return {
      status: toolStatusToRecipeStatus(allAdvice),
      advice: allAdvice,
    };
  }

  const toolConfigurations = buildHotspotsToolConfig(context);
  const neoprofConfig = toolConfigurations.toolConfigs[0];
  // The GUI probes readiness before a workload has necessarily been selected.
  // Defer Java-specific tool checks until the workload type is known.
  if (enableJavaAnalysis && !neoprofConfig.workload.Type) {
    neoprofConfig.params.collect_java_stacks = false;
    neoprofConfig.params.collect_jfr = false;
  }
  const toolResponses = context.probeTools(toolConfigurations);
  const allAdvice = collectToolAdvice(toolConfigurations, toolResponses);
  if (enableJavaAnalysis) {
    if (isAndroidTarget(targetInfo)) {
      allAdvice.push(
        javaAnalysisUnsupportedAdvice(TOOL_NEOPROF.name, 'Android'),
      );
    } else if (hostReformatEnabled) {
      allAdvice.push(
        javaAnalysisUnsupportedAdvice(
          TOOL_NEOPROF.name,
          'host-side reformatting',
        ),
      );
    }
  }
  if (collectJitDumpsEnabled) {
    if (isAndroidTarget(targetInfo)) {
      allAdvice.push({
        ToolName: TOOL_NEOPROF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'JIT dump collection is not supported for Android targets. Jitted symbols will not be available.',
        },
        Cause: '',
      });
    } else if (hostReformatEnabled) {
      allAdvice.push({
        ToolName: TOOL_NEOPROF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'JIT dump collection is not supported when reformatting on the host. Jitted symbols will not be available.',
        },
        Cause: '',
      });
    }
  }
  addTelemetrySpecificationWarning(context, allAdvice);
  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

function javaAnalysisUnsupportedAdvice(toolName, mode) {
  return {
    ToolName: toolName,
    AdviceSeverity: 'error',
    MessageCode: readinessMessageCode,
    Metadata: {
      message: `Java analysis is not supported with ${mode}. Use a supported Linux target with target-side reformatting.`,
    },
    Cause: '',
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runHotspots(context) {
  context.runTools(buildHotspotsToolConfig(context));
}
