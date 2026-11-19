// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const REQUESTED_GPU_METRICS = ['*'];

const { buildGpuRenderParts } = require('../lib/gpu-metrics/timeline');

/** @param {import("../docs/jsdocs").RenderExecutionContext} context */
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

const renderParameters = [];

module.exports = {
  id: 'graphics',
  renderParameters: renderParameters,
  render: renderGPU,
};
