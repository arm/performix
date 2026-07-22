// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Custom Tool Recipe Definition

// @ts-check
let tool_name = 'neoprof';
let tool_version = '1.1.0';

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'custom_tool_recipe',
  title: 'Custom Tool',
  version: '1.0',
  api_version: '1.0.2',
  description: 'User recipe for testing purposes',
  deployments: [
    {
      dependencies: [
        {
          type: 'tool',
          name: tool_name,
          version: tool_version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool',
          name: `jitdump-jvm`,
          version: `0.1.1`,
          requiredWhen: {
            type: 'param_is_set',
            parameters: [{ collect_java_stacks: true }],
          },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'sampling_freq',
      required: false,
      label: 'Sampling Frequency',
      description:
        "Select the sampling frequency for the CPU microarchitecture analysis. The 'normal' frequency is suitable for most workloads, while 'high' provides more detailed information at the cost of increased overhead.",
      config: {
        type: 'single_select',
        options: [
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
        'Enable collection of Java stack traces when profiling JVM workloads.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
  ],
  readyStages: [
    {
      name: 'Recipe Ready',
      description: '',
      exec: readyRecipe,
    },
  ],
  runStages: [
    {
      name: 'Run the recipe',
      description: '',
      exec: runRecipe,
    },
  ],
  renderStages: [
    {
      name: 'Create Render',
      description: 'Create the renderer specs',
      exec: renderRecipe,
    },
  ],
  parameterValidation: validateParameters,
};

/**
 * @param {string} samplingFreq
 * @param {import("./docs/jsdocs").Workload} workload
 */
function getToolsArg(samplingFreq, workload) {
  let toolsArg = {
    tools: [
      {
        name: tool_name,
        args: ['-r', samplingFreq, '--capture-log'],
      },
    ],
    workload: workload,
  };

  // Only use "-I poll" if not system-wide profiling
  // Alternatives are (-S yes), or no inherit (-I no).
  if (workload.Type != 'systemWide') {
    toolsArg.tools[0].args.push('-I', 'poll');
  }
  return toolsArg;
}

/**
 * toolStatusToRecipeStatus converts recipe ready advice to a single
 * recipe ready status. The most severe is returned.
 * @param {import("./docs/jsdocs").RecipeReadyAdvice[]} advice
 * @returns {string} readyStatus
 */
function toolStatusToRecipeStatus(advice) {
  const rank = { ready: 0, unknown: 1, warning: 2, error: 3 };
  return advice.reduce(
    (worst, { AdviceSeverity }) =>
      rank[AdviceSeverity] > rank[worst] ? AdviceSeverity : worst,
    'ready',
  );
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
function readyRecipe(context) {
  let workload = context.getWorkload();
  let params = {
    mode: 'samples',
    sampling_frequency: context.getParameter('sampling_freq'),
    collect_java_stacks: context.getParameter('collect_java_stacks'),
  };
  let tools = generateNeoprofConfig(workload, params);
  let toolResponses = context.probeTools(tools);

  let allAdvice = toolResponses.flatMap((tr, i) =>
    (tr.advice ?? []).map((a) => ({
      ToolName: tools.toolConfigs[i].name,
      AdviceSeverity: a.level,
      MessageCode: a.messageCode,
      Metadata: a.metadata,
      Cause: a.cause,
    })),
  );

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runRecipe(context) {
  let samplingFreq = context.getParameter('sampling_freq');
  let workload = context.getWorkload();
  let params = {
    mode: 'samples',
    sampling_frequency: samplingFreq,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
  };
  context.runTools(generateNeoprofConfig(workload, params));
}

function renderRecipe(context) {}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @returns {import("./docs/jsdocs").ValidationResult}
 */
function validateParameters(context) {
  let samplingFreq = context.getParameter('sampling_freq');
  // This is a contrived example just to demonstrate a validateParameters function which has stricter validation rules
  // than the automatic validation against allowed options done by the engine.
  if (samplingFreq !== 'normal') {
    return {
      errors: [
        {
          parameterId: 'sampling_freq',
          value: samplingFreq,
          messageCode: 'tool_integrations.neoprof.PID_NOT_EXIST',
          cause: "Only 'normal' is allowed.",
        },
      ],
    };
  }
  return { errors: [] };
}
