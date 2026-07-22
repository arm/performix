// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Memory sharing / Cache-to-cache analysis

// @ts-check

const PY_CACHE_SHARING_VERSION = '1.0.0';

let tool_name = 'linux_perf';
let tool_version = '1.0.0';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'cache_sharing',
  title: 'Cache Sharing Analysis',
  version: '1.0.0',
  api_version: '1.0.0',
  status: 'experimental',
  description:
    'Used to spot cache lines with heavy sharing and decide if they’re true sharing (multiple threads writing the same line) or false sharing (threads touching different offsets of the same line). It surfaces per-line and per-function counts of accesses, writers, and sharing offsets so you can pinpoint where contention or false sharing is hurting performance',
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
      id: 'user_only',
      required: false,
      label: 'User-space only',
      description:
        'Configure all events so they only sample when the CPU is executing user-space code.',
      config: { type: 'checkbox', defaultValue: true },
    },
  ],
  readyStages: [
    {
      name: 'Check perf availability',
      description: 'Verify that `perf --version` runs cleanly.',
      exec: readyCacheSharing,
    },
    {
      name: 'Checking SPE is ready',
      description: 'Check that the target is configured for SPE profiling',
      exec: readySPE,
    },
  ],

  runStages: [
    {
      name: 'Recording cache-to-cache samples',
      description: 'Invoke `perf c2c record.',
      exec: runCacheSharing,
    },
  ],

  renderStages: [
    {
      name: 'Creating renderers',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderCacheSharing,
    },
  ],
};

/**
 * ready stage: delegate to the linux_perf integration probe.
 *
 * @param {import("./docs/jsdocs.js").ReadyExecutionContext} context
 */
function readyCacheSharing(context) {
  const wl = context.getWorkload();
  const tools = {
    toolConfigs: [{ name: tool_name, params: {}, workload: wl, env: {} }],
  };

  const toolResponses = context.probeTools(tools);

  const advice = collectToolAdvice(tools, toolResponses);

  return {
    status: toolStatusToRecipeStatus(advice),
    advice,
  };
}

/**
 * Checks if the Linux target has Arm SPE configured using the sysfs intraface.
 * Returns a readiness status. Mirror of `readySPE` memory_access.
 * TODO: Move this into a more central location; APAP-4360.
 *
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readySPE(context) {
  const speReady = context.runCommand({
    type: 'exec',
    cmd: 'ls -d /sys/bus/event_source/devices/arm_spe_* 2>/dev/null',
  });
  if (speReady.ReturnCode !== 0) {
    return {
      status: 'error',
      advice: [
        {
          ToolName: tool_name,
          AdviceSeverity: 'error',
          MessageCode: 'tool_integrations.common.SPE_NOT_CONFIGURED',
          Metadata: {},
          Cause: '',
        },
      ],
    };
  }

  return { status: 'ready', advice: [] };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runCacheSharing(context) {
  const wl = context.getWorkload();

  // Default to c2c record. Optional -u for user space only sampling
  let perfArgs = 'c2c record';
  if (context.getParameter('user_only')) {
    perfArgs = `${perfArgs} -u`;
  }

  return context.runTools({
    toolConfigs: [
      {
        name: tool_name,
        params: { perfArgs },
        workload: wl,
        env: {},
      },
    ],
  });
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function renderCacheSharing(context) {
  const renderSpec = {
    renderers: [
      {
        type: 'StreamlineAnalyzeSymbols',
        id: 'perf_symbols',
        config: {
          entity: `tool/${tool_name}/0/`,
          symbols: 'perf_c2c_output_symbols.json',
        },
      },
      {
        type: 'CacheSharing',
        id: 'cache_sharing',
        config: {
          entity: `tool/${tool_name}/0/`,
          component: 'perf-c2c-output',
          data_source: {
            tables: {
              symbols: [
                {
                  renderer_id: 'perf_symbols',
                  output: 'symbols',
                },
              ],
              images: [
                {
                  renderer_id: 'perf_symbols',
                  output: 'images',
                },
              ],
              source_files: [
                {
                  renderer_id: 'perf_symbols',
                  output: 'source_files',
                },
              ],
            },
          },
        },
      },
      {
        type: 'SourceCodeAttribution',
        id: 'source_code_attribution',
        config: {
          data_source: {
            tables: {
              source_files: [
                { renderer_id: 'perf_symbols', output: 'source_files' },
              ],
            },
          },
          entity: `tool/${tool_name}/0/`,
        },
      },
    ],
    visualizations: [
      {
        type: 'generic_grid',
        id: 'cachelines',
        rendererId: 'cache_sharing',
        title: 'Cachelines',
        description:
          'Cachelines flagged as TRUE/FALSE sharing issues with sample counts and sharing metadata.',
        config: {
          data_source: {
            tables: {
              table: [
                {
                  renderer_id: 'cache_sharing',
                  output: 'cachelines_flat',
                },
              ],
            },
          },
        },
      },
      {
        type: 'flat_functions',
        id: 'accesses',
        rendererId: 'cache_sharing',
        title: 'Accesses',
        description:
          'Function-level cache-to-cache accesses with sample, coherence, and store counts.',
        config: {
          data_source: {
            tables: {
              flatFunctions: [
                {
                  renderer_id: 'cache_sharing',
                  output: 'drilldown',
                },
              ],
              measurements: [
                {
                  renderer_id: 'cache_sharing',
                  output: 'measurements',
                },
              ],
              symbols: [
                {
                  renderer_id: 'perf_symbols',
                  output: 'symbols',
                },
              ],
              images: [
                {
                  renderer_id: 'perf_symbols',
                  output: 'images',
                },
              ],
              source_files: [
                {
                  renderer_id: 'perf_symbols',
                  output: 'source_files',
                },
              ],
            },
          },
        },
      },
    ],
  };

  return renderSpec;
}
