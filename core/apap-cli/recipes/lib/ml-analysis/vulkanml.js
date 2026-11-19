// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const TOOL_NEOPROF = { name: 'neoprof', version: '1.1.0' };
const {
  TOOL_GPU_LAYERS,
} = require('../../../tool-integrations/neoprof_gpu_timeline');
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;
const { buildGpuRenderParts } = require('../gpu-metrics/timeline');

const requiredWhen = {
  type: 'param_is_set',
  parameters: [{ flavor: 'vulkanml' }],
};
function getDeploymentDependencies(os) {
  return [
    { type: 'tool', ...TOOL_NEOPROF, requiredWhen },
    ...(os === 'Android'
      ? [{ type: 'tool_bundle', ...TOOL_GPU_LAYERS, requiredWhen }]
      : []),
  ];
}
const parameters = [];

function buildTools(workload) {
  return {
    toolConfigs: [
      {
        name: TOOL_NEOPROF.name,
        params: {
          reformat_on_host: true,
          download_images: false,
          collect_call_chains: false,
          rich_data_capture: true,
          collect_gpu_timeline: true,
          requested_gpu_metrics: JSON.stringify(['*']),
        },
        workload,
        env: {},
      },
    ],
  };
}

const unsupportedWorkloadCode =
  'recipes.ml_analysis.VULKANML_REQUIRES_ANDROID_LAUNCH';

function ready(context) {
  const workload = context.getWorkload();
  if (workload.Type !== 'androidLaunch') {
    return {
      status: 'error',
      advice: [
        {
          ToolName: TOOL_NEOPROF.name,
          AdviceSeverity: 'error',
          MessageCode: unsupportedWorkloadCode,
          Metadata: {},
          Cause: '',
        },
      ],
    };
  }
  const tools = buildTools(workload);
  const responses = context.probeTools(tools);
  const advice = collectToolAdvice(tools, responses);
  return { status: toolStatusToRecipeStatus(advice), advice };
}

function run(context) {
  const workload = context.getWorkload();
  if (workload.Type !== 'androidLaunch') {
    throw { code: unsupportedWorkloadCode };
  }
  context.runTools(buildTools(workload));
}

function render(context, runs) {
  const renderers = [];
  const visualizations = [];
  const gpu = runs[0].ToolsUsed.includes(TOOL_NEOPROF.name)
    ? buildGpuRenderParts(context, ['*'], { optional: true })
    : { renderers: [], groups: {}, tables: {} };
  if (Object.keys(gpu.groups).length > 0) {
    renderers.push(
      ...gpu.renderers.map((renderer) => ({
        ...renderer,
        config: { ...renderer.config, materialized: true },
      })),
    );
    visualizations.push({
      type: 'timeline',
      id: 'gpu_timeline',
      rendererId: 'gpu_metrics',
      title: 'GPU Timeline',
      description:
        'GPU metrics grouped by semantic layout in 100 ms bins, with a separate line per metric and device.',
      config: {
        xAxisUnit: 's',
        data_source: { tables: gpu.tables },
        groups: gpu.groups,
      },
    });
  }
  return { renderers, visualizations };
}

module.exports = { getDeploymentDependencies, parameters, ready, run, render };
