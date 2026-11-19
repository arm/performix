// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// ASCT Recipe Definition

// @ts-check

const {
  render: renderAsct,
  resolveBenchmarkSelection,
  parseBool,
  ASCT_BENCHMARKS,
  ASCT_DEFAULT_BENCHMARKS,
  TOOL_ASCT_NAME,
} = require('./analyses/asct');

const ASCT_VERSION = '0.7.1';
const TOOL_ASCT_VERSION = ASCT_VERSION;
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'asct',
  title: 'System Characterization',
  version: '1.0',
  api_version: '1.0.0',
  status: 'stable',
  description:
    'Collect system information and benchmark memory, storage, and core-to-core performance to characterize your target.',
  mcp_guidance:
    'ASCT runs can take several minutes, especially with Loaded latency. Select System information only for a quick hardware check, or select individual benchmarks for a shorter run.',
  parameters: [
    {
      id: 'system_info_only',
      required: false,
      label: 'System info only',
      description: 'Collect system information without running benchmarks.',
      config: /** @type {any} */ ({
        type: 'checkbox',
        defaultValue: false,
      }),
    },
    {
      id: 'default_benchmarks',
      required: false,
      label: 'Default benchmarks',
      description:
        'Run the default latency, bandwidth, NUMA, and core-to-core benchmarks. Loaded latency and storage benchmarks are not included and can extend the run.',
      config: /** @type {any} */ ({
        type: 'checkbox',
        defaultValue: false,
      }),
    },
    ...ASCT_BENCHMARKS.map((benchmark) => ({
      id: benchmark.id,
      required: false,
      label: benchmark.label,
      description: benchmark.description,
      config: /** @type {any} */ ({
        type: 'checkbox',
        defaultValue: false,
      }),
    })),
  ],
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_ASCT_NAME,
          version: TOOL_ASCT_VERSION,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  readyStages: [
    {
      name: 'Checking ASCT is ready',
      description:
        'Check that the target has the necessary dependencies to run the ASCT tool (in agent mode)',
      exec: readyAsct,
    },
  ],
  runStages: [
    {
      name: 'Validating recipe parameters',
      description:
        'This stage validates the user has specified appropriate parameter values',
      exec: validateRecipeParameters,
    },
    {
      name: 'Setting Up and Running ASCT',
      description:
        'This stage sets up and runs ASCT; then retrieves the necessary files',
      exec: runAsct,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderAsct,
    },
  ],
};

/**
 * generateAsctToolConfig generates a ToolConfiguration for the
 * ASCT tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfiguration}
 */
function generateAsctToolConfig(workload, params) {
  return /** @type {any} */ ({
    name: TOOL_ASCT_NAME,
    params,
    workload,
    env: {},
  });
}

/**
 * Serialize the selected benchmark names into the tool parameter format.
 *
 * @param {{name: string}[]} benchmarks
 * @returns {string}
 */
function serializeBenchmarks(benchmarks) {
  return benchmarks.map((benchmark) => benchmark.name).join(',');
}

/**
 * Build ASCT tool params from recipe checkboxes.
 * - If system_info_only=true, benchmark selections are ignored.
 * - If default_benchmarks=true, the recipe-defined default benchmark set is
 *   passed explicitly to the tool integration.
 * - If no benchmark checkboxes are selected, use the same default behavior.
 * @param {any} context
 * @returns {Object.<string, any>}
 */
function buildAsctParams(context) {
  const { systemInfoOnly, defaultBenchmarks, selectedBenchmarks } =
    resolveBenchmarkSelection((id) => context.getParameter(id));

  /** @type {Object.<string, any>} */
  const params = {};

  params.systemInfoOnly = systemInfoOnly;
  params.defaultBenchmarks = defaultBenchmarks;
  params.benchmarks = serializeBenchmarks(selectedBenchmarks);

  return params;
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyAsct(context) {
  let workload = context.getWorkload();

  /** @type {import("./docs/jsdocs").ToolConfigurationsArg} */
  let tools = { toolConfigs: [] };

  const { systemInfoOnly, defaultBenchmarks, selectedBenchmarks } =
    resolveBenchmarkSelection((id) => context.getParameter(id));

  const params = {
    benchmarks: systemInfoOnly ? '' : serializeBenchmarks(selectedBenchmarks),
    defaultBenchmarks: defaultBenchmarks,
    systemInfoOnly: systemInfoOnly,
  };

  tools.toolConfigs.push(generateAsctToolConfig(workload, params));

  const toolResponses = context.probeTools(tools);
  const responseList = Array.isArray(toolResponses) ? toolResponses : [];

  const allAdvice = collectToolAdvice(tools, responseList);

  return {
    Status: toolStatusToRecipeStatus(allAdvice),
    Advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function validateRecipeParameters(context) {
  const { systemInfoOnly, defaultBenchmarks, selectedBenchmarks } =
    resolveBenchmarkSelection((id) => context.getParameter(id));
  const explicitlyRequestedDefaultBenchmarks = parseBool(
    context.getParameter('default_benchmarks'),
  );
  const explicitlySelectedBenchmarks = ASCT_BENCHMARKS.filter((benchmark) =>
    parseBool(context.getParameter(benchmark.id)),
  );

  if (systemInfoOnly) {
    const ignoredLabels = [];
    if (explicitlyRequestedDefaultBenchmarks) {
      ignoredLabels.push('Default benchmarks');
    }
    ignoredLabels.push(
      ...explicitlySelectedBenchmarks.map((benchmark) => benchmark.label),
    );
    if (ignoredLabels.length > 0) {
      context.writeUserMessage(
        'warn',
        `System information only is selected, so the following benchmark selections are ignored: ${ignoredLabels.join(', ')}`,
      );
    }
  } else if (defaultBenchmarks) {
    if (explicitlySelectedBenchmarks.length > 0) {
      context.writeUserMessage(
        'warn',
        `Default benchmarks are selected, so the following individual selections are ignored: ${explicitlySelectedBenchmarks.map((benchmark) => benchmark.label).join(', ')}`,
      );
    }
    context.writeUserMessage(
      'info',
      `Running the default ASCT benchmarks: ${selectedBenchmarks.map((benchmark) => benchmark.label).join(', ')}.`,
    );
  } else {
    if (explicitlySelectedBenchmarks.length === 0) {
      context.writeUserMessage(
        'info',
        `No benchmarks selected. Running the default ASCT benchmarks: ${selectedBenchmarks.map((benchmark) => benchmark.label).join(', ')}.`,
      );
    }
  }
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runAsct(context) {
  let workload = context.getWorkload();

  /** @type {import("./docs/jsdocs").ToolConfigurationsArg} */
  let tools = { toolConfigs: [] };

  let params = buildAsctParams(context);

  tools.toolConfigs.push(generateAsctToolConfig(workload, params));

  context.runTools(tools);
}
