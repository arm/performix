// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Memory Access Recipe Definition

// @ts-check
let tool_name = 'neoprof';
let tool_version = '1.1.0';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'memory_access',
  title: 'Memory Access',
  version: '1.0',
  api_version: '1.0.0',
  status: 'stable',
  description:
    'The Memory Access recipe analyzes how your software interacts with the memory system and identifies where latency occurs. It uses low-overhead profiling to reveal inefficient data access patterns under typical workload conditions, without significant overhead.',
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
      id: 'min_latency_cycles',
      required: false,
      label: 'Minimum Latency Cycles',
      description:
        'Set the minimum latency cycles. This parameter allows you to filter out memory accesses that are below a certain latency threshold, focusing on more significant memory operations.',
      config: {
        type: 'input',
        defaultValue: '',
      },
    },
    {
      id: 'spe_sample_rate',
      required: false,
      label: 'SPE Sampling Rate',
      description:
        'Set the Arm SPE periodic sampling rate (operations between each sample). Must be a non-zero positive integer; hardware minimums apply.',
      config: {
        type: 'input',
        defaultValue: '',
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
  ],
  readyStages: [
    {
      name: 'Checking Neoprof is ready',
      description: 'Check that the target can run the Neoprof collector',
      exec: readyNeoprof,
    },
    {
      name: 'Checking SPE is ready',
      description: 'Check that the target is configured for SPE profiling',
      exec: readySPE,
    },
  ],
  runStages: [
    {
      name: 'Collecting SPE samples',
      description:
        'Collect SPE samples on the target and processes the captured data',
      exec: runMemoryAccess,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderMemoryAccess,
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {string} workflow
 */
function getToolsArg(workload, workflow) {
  let toolsArg = {
    tools: [
      {
        name: tool_name,
        args: ['-X', workflow],
      },
    ],
    workload: workload,
  };
  return toolsArg;
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
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function computeSpeWorkflow(context) {
  let workflow = 'workflow_spe';
  let minLatency = context.getParameter('min_latency_cycles');
  if (minLatency) {
    workflow = `workflow_spe:min_latency=${minLatency}`;
  }
  return workflow;
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyNeoprof(context) {
  let workload = context.getWorkload();
  const speSampleRate = context.getParameter('spe_sample_rate');

  let params = {
    mode: 'spe',
    spe_sample_rate: speSampleRate,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
  };
  let tools = generateNeoprofConfig(workload, params);
  let toolResponses = context.probeTools(tools);

  let allAdvice = collectToolAdvice(tools, toolResponses);

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readySPE(context) {
  /* This is a temporary check until the profiler supports a recipe readiness
   * probe for this. It would be better to create a trial SPE event but that
   * requires perf to be installed, so just check sysfs instead for now. */
  let readySPE = context.runCommand({
    type: 'exec',
    cmd: 'ls -d /sys/bus/event_source/devices/arm_spe_* 2>/dev/null',
  });
  if (readySPE.ReturnCode != 0) {
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
function runMemoryAccess(context) {
  let workload = context.getWorkload();
  let workflow = computeSpeWorkflow(context);
  const speSampleRate = context.getParameter('spe_sample_rate');
  let params = {
    mode: 'spe',
    spe_workflow: workflow,
    spe_sample_rate: speSampleRate,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
  };
  context.runTools(generateNeoprofConfig(workload, params));
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
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
