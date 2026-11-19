// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Instruction Mix Recipe Definition

// @ts-check
const {
  render: renderInstructionMix,
  renderParameters,
} = require('./analyses/instruction_mix');

let tool_neoprof_name = 'neoprof';

const PY_IM_VERSION = '0.4.5';
let tool_imix_name = 'instruction_mix';
let tool_neoprof_version = '1.1.0';
let tool_imix_version = '1.1.0';

// Temporary fix until we support recipes defining whether they are custom or not
let customRecipe = false;
const {
  collectToolAdvice,
  toolStatusToRecipeStatus,
  getPartialCPUSupportAdvice,
} = recipeUtils;
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
    'See the types and proportions of instructions in your code, including vector instructions.',
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
  renderParameters: renderParameters,
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
    telemetrySpecification: context.getTelemetrySpecification(
      targetInfo.PrimaryCPUName,
    ),
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @param {import("./docs/jsdocs").RecipeReadyAdvice[]} advice
 */
function addTelemetrySpecificationAdvice(context, advice) {
  const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);
  if (!firstSupportedCPUTelemetry.telemetrySpecification) {
    advice.push({
      ToolName: '',
      AdviceSeverity: 'error',
      MessageCode: telemetrySpecificationUnavailableMessageCode,
      Metadata: { cpuName: firstSupportedCPUTelemetry.cpuName },
      Cause: '',
    });
  } else {
    const partialCPUSupportAdvice = getPartialCPUSupportAdvice(
      context,
      'recipes.instruction_mix.PARTIAL_CPU_SUPPORT',
    );
    if (partialCPUSupportAdvice) {
      advice.push(partialCPUSupportAdvice);
    }
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
    const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);

    let params = {
      mode: 'metrics',
      metrics_group: 'operation_mix',
      sampling_frequency: samplingFreq,
      get_ipc_metric_name: true,
      collect_java_stacks: context.getParameter('collect_java_stacks'),
      collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
      rich_data_capture: context.getParameter('rich_data_capture'),
      filter_core_numbers: firstSupportedCPUTelemetry.coreNumbers.join(','),
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
    addTelemetrySpecificationAdvice(context, allAdvice);
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
    const firstSupportedCPUTelemetry = getFirstSupportedCPUTelemetry(context);

    let params = {
      mode: 'metrics',
      metrics_group: 'operation_mix',
      sampling_frequency: samplingFreq,
      get_ipc_metric_name: true,
      collect_java_stacks: context.getParameter('collect_java_stacks'),
      collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
      rich_data_capture: context.getParameter('rich_data_capture'),
      filter_core_numbers: firstSupportedCPUTelemetry.coreNumbers.join(','),
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
