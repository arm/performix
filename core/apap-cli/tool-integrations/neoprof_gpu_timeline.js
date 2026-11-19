// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const { ensureDeployed } = require('./utils');

const TOOL_GPU_LAYERS = {
  name: 'gpulayers',
  version: '9.8.0.ngp_b7193aa2_3',
};
const LAYER_LIBRARY = 'libVkLayerGPUTimeline.so';

/** @param {import('../recipes/docs/jsdocs').ToolContext} ctx */
function isEnabled(ctx) {
  return (
    ctx.params.collect_gpu_timeline === true &&
    ctx.workload.type === 'androidLaunch'
  );
}

/** @param {import('../recipes/docs/jsdocs').Engine} engine */
function libraryPath(engine) {
  return `${engine.toolsRoot()}/${TOOL_GPU_LAYERS.name}/${TOOL_GPU_LAYERS.version}/bin/${LAYER_LIBRARY}`;
}

/**
 * Gator activates the layer in the package, then restores Android settings after capture.
 * Its Android layer manager requires the library at this fixed staging path.
 * @param {import('../recipes/docs/jsdocs').Engine} engine
 * @param {import('../recipes/docs/jsdocs').ToolContext} ctx
 */
async function prepare(engine, ctx) {
  if (!isEnabled(ctx)) return;
  const source = libraryPath(engine);
  await ensureDeployed(engine, source, TOOL_GPU_LAYERS.name);
  const result = await engine.execCommand(
    ['cp', source, `/data/local/tmp/${LAYER_LIBRARY}`],
    { asPrivileged: false },
  );
  if (result.rc !== 0) {
    throw {
      code: 'tool_integrations.neoprof.NEOPROF_FAILED',
      metadata: { tool: 'cp', code: result.rc },
      cause: result.stderr,
    };
  }
}

module.exports = { TOOL_GPU_LAYERS, isEnabled, libraryPath, prepare };
