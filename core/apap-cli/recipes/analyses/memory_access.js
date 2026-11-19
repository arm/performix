// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

let tool_name = 'neoprof';

/**
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 */
function renderMemoryAccess(context) {
  let isComparison = context.getRunDescriptions().length === 2;

  const dataSourceSingle = {
    tables: {
      symbols: [{ renderer_id: 'streamline_symbols', output: 'symbols' }],
      images: [{ renderer_id: 'streamline_symbols', output: 'images' }],
    },
  };

  const latencyBreakdownDataSourceCompareDrilldown = {
    tables: {
      drilldown: [
        {
          renderer_id: 'latency_breakdown',
          output: 'latency_breakdown',
          content_index: 0,
        },
        {
          renderer_id: 'latency_breakdown',
          output: 'latency_breakdown',
          content_index: 1,
        },
      ],
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
    },
  };

  const tlbWalkDataSourceCompareDrilldown = {
    tables: {
      drilldown: [
        { renderer_id: 'tlb_walk', output: 'tlb_walk_score', content_index: 0 },
        { renderer_id: 'tlb_walk', output: 'tlb_walk_score', content_index: 1 },
      ],
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
    },
  };

  const dataSourceComparison = {
    tables: {
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
    },
  };

  const sourceFilesSingle = {
    tables: {
      source_files: [
        { renderer_id: 'streamline_symbols', output: 'source_files' },
      ],
    },
  };

  const sourceFilesComparison = {
    tables: {
      source_files: [
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 1,
        },
      ],
    },
  };

  const disassemblySingle = {
    tables: {
      source_files: [
        { renderer_id: 'streamline_symbols', output: 'source_files' },
      ],
      images: [{ renderer_id: 'streamline_symbols', output: 'images' }],
      symbols: [{ renderer_id: 'streamline_symbols', output: 'symbols' }],
    },
  };

  const disassemblyComparison = {
    tables: {
      source_files: [
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'source_files',
          content_index: 1,
        },
      ],
      images: [
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'images',
          content_index: 1,
        },
      ],
      symbols: [
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 0,
        },
        {
          renderer_id: 'streamline_symbols',
          output: 'symbols',
          content_index: 1,
        },
      ],
    },
  };

  const dataSource = isComparison ? dataSourceComparison : dataSourceSingle;
  const sourceFiles = isComparison ? sourceFilesComparison : sourceFilesSingle;
  const disassembly = isComparison ? disassemblyComparison : disassemblySingle;

  let renderSpec = {
    renderers: [
      {
        type: 'StreamlineAnalyzeSymbols',
        id: 'streamline_symbols',
        config: {
          entity: `tool/${tool_name}/0/`,
          symbols: 'symbols-spe.json',
          legacy_symbols: 'symbols.json',
        },
      },
      {
        type: 'ProcessesAndThreadsParser',
        id: 'processes_and_threads',
        config: { entity: `tool/${tool_name}/0/` },
      },
      {
        type: 'LatencyBreakdown',
        id: 'latency_breakdown',
        config: {
          component: 'functions-capture-spe.csv',
          data_source: dataSource,
          entity: `tool/${tool_name}/0/`,
        },
      },
      {
        type: 'TLBWalkScore',
        id: 'tlb_walk',
        config: {
          component: 'functions-capture-spe.csv',
          data_source: dataSource,
          entity: `tool/${tool_name}/0/`,
        },
      },
      {
        type: 'SourceCodeAttribution',
        id: 'source_code_attribution',
        config: {
          data_source: sourceFiles,
          entity: `tool/${tool_name}/0/`,
        },
      },
      {
        type: 'DisassemblyRenderer',
        id: 'disassembly',
        config: {
          entity: `tool/${tool_name}/0/`,
          data_source: disassembly,
        },
      },
    ],
    visualizations: [
      isComparison
        ? {
            type: 'flat_functions_comparison',
            id: 'compare_latency_breakdown',
            rendererId: 'compare_latency_breakdown',
            title: 'Latency Breakdown Comparison',
            description:
              'Compare how memory latency is distributed across memory levels for each function. Use this view to identify changes in memory behavior and execution time between runs.',
            config: {
              data_source: {
                tables: {
                  flatFunctionsCurrentRun: [
                    {
                      renderer_id: 'latency_breakdown',
                      output: 'latency_breakdown',
                      content_index: 1,
                    },
                  ],
                  flatFunctionsBaselineRun: [
                    {
                      renderer_id: 'latency_breakdown',
                      output: 'latency_breakdown',
                      content_index: 0,
                    },
                  ],
                  deltas: [
                    {
                      renderer_id: 'compare_latency_breakdown',
                      output: 'delta_flat',
                    },
                  ],
                  measurements: [
                    {
                      renderer_id: 'latency_breakdown',
                      output: 'measurements',
                    },
                  ],
                  symbols: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'symbols',
                      content_index: 1,
                    },
                  ],
                  images: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'images',
                      content_index: 1,
                    },
                  ],
                  source_files: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'source_files',
                      content_index: 1,
                    },
                  ],
                },
              },
            },
          }
        : {
            type: 'flat_functions',
            id: 'latency_breakdown',
            rendererId: 'latency_breakdown',
            title: 'Latency Breakdown',
            description:
              'View how the memory latency of each function is distributed across the memory hierarchy, including the average latency and total contribution to execution time.',
            config: {
              data_source: {
                tables: {
                  flatFunctions: [
                    {
                      renderer_id: 'latency_breakdown',
                      output: 'latency_breakdown',
                    },
                  ],
                  measurements: [
                    {
                      renderer_id: 'latency_breakdown',
                      output: 'measurements',
                    },
                  ],
                  symbols: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'symbols',
                    },
                  ],
                  images: [
                    { renderer_id: 'streamline_symbols', output: 'images' },
                  ],
                  source_files: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'source_files',
                    },
                  ],
                },
              },
            },
          },
      isComparison
        ? {
            type: 'flat_functions_comparison',
            id: 'compare_tlb',
            rendererId: 'compare_tlb_walk',
            title: 'TLB Walk Breakdown Comparison',
            description:
              'Compare TLB accesses, TLB walks, and the average walk cost for each function between runs. Use this view to identify changes in address translation behavior.',
            config: {
              data_source: {
                tables: {
                  flatFunctionsCurrentRun: [
                    {
                      renderer_id: 'tlb_walk',
                      output: 'tlb_walk_score',
                      content_index: 1,
                    },
                  ],
                  flatFunctionsBaselineRun: [
                    {
                      renderer_id: 'tlb_walk',
                      output: 'tlb_walk_score',
                      content_index: 0,
                    },
                  ],
                  deltas: [
                    {
                      renderer_id: 'compare_tlb_walk',
                      output: 'delta_flat',
                    },
                  ],
                  measurements: [
                    { renderer_id: 'tlb_walk', output: 'measurements' },
                  ],
                  symbols: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'symbols',
                      content_index: 1,
                    },
                  ],
                  images: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'images',
                      content_index: 1,
                    },
                  ],
                  source_files: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'source_files',
                    },
                  ],
                },
              },
            },
          }
        : {
            type: 'flat_functions',
            id: 'tlb',
            rendererId: 'tlb_walk',
            title: 'TLB Walk Breakdown',
            description:
              'View the number of TLB accesses and TLB walks for each function, and the average cost of TLB walks. Use this view to identify functions affected by address translation overhead.',
            config: {
              data_source: {
                tables: {
                  flatFunctions: [
                    { renderer_id: 'tlb_walk', output: 'tlb_walk_score' },
                  ],
                  measurements: [
                    { renderer_id: 'tlb_walk', output: 'measurements' },
                  ],
                  symbols: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'symbols',
                    },
                  ],
                  images: [
                    { renderer_id: 'streamline_symbols', output: 'images' },
                  ],
                  source_files: [
                    {
                      renderer_id: 'streamline_symbols',
                      output: 'source_files',
                    },
                  ],
                },
              },
            },
          },
    ],
  };

  if (isComparison) {
    renderSpec.renderers.push(
      {
        type: 'CompareDrilldownFlat',
        id: 'compare_latency_breakdown',
        config: { data_source: latencyBreakdownDataSourceCompareDrilldown },
      },
      {
        type: 'CompareDrilldownFlat',
        id: 'compare_tlb_walk',
        config: { data_source: tlbWalkDataSourceCompareDrilldown },
      },
    );
  }

  return renderSpec;
}

module.exports = {
  id: 'memory_access',
  renderParameters: [],
  render: renderMemoryAccess,
};
