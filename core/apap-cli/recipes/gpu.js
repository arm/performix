// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// GPU Recipe Definition

// @ts-check

const TOOL_NEOPROF = { name: 'neoprof', version: '1.1.0' };
const READINESS_MESSAGE_CODE =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'gpu',
  title: 'GPU',
  version: '1.0',
  api_version: '1.0.2',
  status: 'experimental',
  description: 'The GPU recipe.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Android' }],
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
  parameters: [],
  renderParameters: [],
  readyStages: [
    {
      name: 'Checking GPU information is available',
      description: 'Check that Neoprof can query GPU information',
      exec: readyGPU,
    },
  ],
  runStages: [],
  renderStages: [],
};

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyGPU(context) {
  const tools = getGpuToolConfiguration(context);
  const toolResponses = context.probeTools(tools);
  const capabilities = toolResponses[0]?.capabilities ?? {};
  const gpuName = capabilities.platform?.payload.gpu_name;
  const gpuCounters = Object.values(capabilities).filter(
    (capability) =>
      capability?.componentType?.name === 'tool_capabilities/counter' &&
      capability.state === 'available',
  );
  const advice = collectToolAdvice(tools, toolResponses);

  if (!gpuName) {
    addReadinessMessage(
      advice,
      'error',
      'The target GPU could not be identified.',
    );
  }

  const status = toolStatusToRecipeStatus(advice);
  if (gpuName && gpuCounters?.length) {
    addReadinessMessage(
      advice,
      'message',
      `Detected GPU: ${gpuName}. Found ${gpuCounters.length} GPU counters.`,
    );
  }

  return {
    status,
    advice,
  };
}

/**
 * @param {Array} advice
 * @param {'error'|'message'} severity
 * @param {string} message
 */
function addReadinessMessage(advice, severity, message) {
  advice.push({
    ToolName: TOOL_NEOPROF.name,
    AdviceSeverity: severity,
    MessageCode: READINESS_MESSAGE_CODE,
    Metadata: { message },
    Cause: '',
  });
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function getGpuToolConfiguration(context) {
  return {
    toolConfigs: [
      {
        name: TOOL_NEOPROF.name,
        params: {
          mode: 'gpu',
        },
        workload: context.getWorkload(),
        env: {},
      },
    ],
  };
}
