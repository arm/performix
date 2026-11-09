// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Graphics Recipe Definition

// @ts-check

const TOOL_NEOPROF = { name: 'neoprof', version: '1.1.0' };
const TOOL_LWI = { name: 'lwi', version: '1.0.1' };
const TOOL_GPU_LAYERS = {
  name: 'gpulayers',
  version: '9.8.0.ngp_b7193aa2_3',
};
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;
const { buildGpuRenderParts } = require('./lib/gpu-metrics/timeline');
const REQUESTED_GPU_METRICS = ['*'];

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'graphics',
  title: 'Graphics',
  version: '1.0',
  api_version: '1.0.2',
  status: 'experimental',
  description: 'Analyze GPU activity and performance counters over time.',
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
        {
          type: 'tool_bundle',
          name: TOOL_LWI.name,
          version: TOOL_LWI.version,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool_bundle',
          name: TOOL_GPU_LAYERS.name,
          version: TOOL_GPU_LAYERS.version,
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
  runStages: [
    {
      name: 'Collecting GPU counters',
      description: 'Collect the available GPU counters with NeoProf',
      exec: runGPU,
    },
  ],
  renderStages: [
    {
      name: 'Building GPU metrics',
      description: 'Build the requested GPU metric groups',
      exec: renderGPU,
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyGPU(context) {
  const tools = getGpuToolConfiguration(context);
  const toolResponses = context.probeTools(tools);
  const advice = collectToolAdvice(tools, toolResponses);

  const status = toolStatusToRecipeStatus(advice);
  return {
    status,
    advice,
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function getGpuToolConfiguration(context) {
  return {
    toolConfigs: [
      {
        name: TOOL_NEOPROF.name,
        params: {
          reformat_on_host: true,
          requested_gpu_metrics: JSON.stringify(REQUESTED_GPU_METRICS),
        },
        workload: context.getWorkload(),
        env: {},
      },
    ],
  };
}

/** @param {import("./docs/jsdocs").RunExecutionContext} context */
function runGPU(context) {
  const tools = getGpuToolConfiguration(context);
  context.runTools(tools);
}

/** @param {import("./docs/jsdocs").RenderExecutionContext} context */
function renderGPU(context) {
  const { renderers, tables, groups } = buildGpuRenderParts(
    context,
    REQUESTED_GPU_METRICS,
  );
  if (Object.keys(groups).length === 0) {
    return { renderers, visualizations: [] };
  }
  return {
    // Prepare metric tables once for all timeline groups.
    renderers: renderers.map((renderer) => ({
      ...renderer,
      config: { ...renderer.config, materialized: true },
    })),
    visualizations: [
      {
        type: 'timeline',
        id: 'gpu_timeline',
        rendererId: 'gpu_metrics',
        title: 'GPU Timeline',
        description:
          'GPU metrics grouped by semantic layout in 100 ms bins, with a separate line per metric and device.',
        config: {
          xAxisUnit: 's',
          data_source: { tables },
          groups,
        },
      },
    ],
  };
}
