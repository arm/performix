// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

let tool_name = 'linux_perf';

/**
 * @param {import("../docs/jsdocs").RunExecutionContext} context
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

module.exports = {
  id: 'cache_sharing',
  renderParameters: [],
  render: renderCacheSharing,
};
