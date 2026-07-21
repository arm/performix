// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// ASCT Recipe Definition

// @ts-check

const ASCT_VERSION = '0.6.0';
const TOOL_ASCT_NAME = 'asct';
const TOOL_ASCT_VERSION = ASCT_VERSION;
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

// Centralize benchmark metadata here so the recipe owns the exposed benchmark set.
const ASCT_BENCHMARKS = [
  {
    id: 'benchmark_idle_latency',
    name: 'idle-latency',
    label: 'Idle latency',
    defaultSelected: true,
    description: 'Report a matrix of idle memory latency across NUMA nodes.',
  },
  {
    id: 'benchmark_peak_bandwidth',
    name: 'peak-bandwidth',
    label: 'Peak bandwidth',
    defaultSelected: true,
    description: 'Report peak memory bandwidth.',
  },
  {
    id: 'benchmark_cross_numa_bandwidth',
    name: 'cross-numa-bandwidth',
    label: 'Cross-NUMA bandwidth',
    defaultSelected: true,
    description: 'Report cross-NUMA node memory bandwidth.',
  },
  {
    id: 'benchmark_latency_sweep',
    name: 'latency-sweep',
    label: 'Latency sweep',
    defaultSelected: true,
    description:
      'Sweep latency by datasize to map cache hierarchy and find optimal datasize for other benchmarks.',
  },
  {
    id: 'benchmark_bandwidth_sweep',
    name: 'bandwidth-sweep',
    label: 'Bandwidth sweep',
    defaultSelected: true,
    description: 'Sweep bandwidth by datasize to map cache hierarchy.',
  },
  {
    id: 'benchmark_loaded_latency',
    name: 'loaded-latency',
    label: 'Loaded latency',
    defaultSelected: false,
    description:
      'Report loaded memory latency with background memory activity. This benchmark can take several minutes to complete.',
  },
  {
    id: 'benchmark_c2c_latency',
    name: 'c2c-latency',
    label: 'Core-to-core latency',
    defaultSelected: true,
    description: 'Report core to core latency.',
  },
];

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'asct',
  title: 'System Characterization',
  version: '1.0',
  api_version: '1.0.0',
  status: 'preview',
  description:
    'This *preview* recipe runs the Arm System Characterization Tool (ASCT) to collect system information and microbenchmark results. It measures memory latency/bandwidth behavior to help with platform bring-up, tuning, and architectural comparisons.',
  mcp_guidance:
    'Default benchmark runs can take several minutes; use timeout 600 when running the default benchmarks. Use system_info_only for a short system information check.',
  parameters: [
    {
      id: 'system_info_only',
      required: false,
      label: 'System info only',
      description: 'Run only system-info (no benchmarks).',
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
        'Run the ASCT default benchmark set (same behavior as passing no benchmark arguments).',
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

/** @param {unknown} value */
function parseBool(value) {
  return String(value).toLowerCase() === 'true';
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

const ASCT_DEFAULT_BENCHMARKS = ASCT_BENCHMARKS.filter(
  (benchmark) => benchmark.defaultSelected === true,
);

/**
 * Resolve benchmark selection rules:
 * 1) system_info_only=true => no benchmarks selected.
 * 2) default_benchmarks=true => ASCT default benchmarks selected.
 * 3) Some benchmarks selected => only selected benchmarks.
 * 4) No benchmark params selected => ASCT default benchmarks selected.
 * @param {(id: string) => unknown} getParam
 */
function resolveBenchmarkSelection(getParam) {
  const systemInfoOnly = parseBool(getParam('system_info_only'));
  const defaultBenchmarks = parseBool(getParam('default_benchmarks'));
  const explicitlySelectedBenchmarks = ASCT_BENCHMARKS.filter((benchmark) =>
    parseBool(getParam(benchmark.id)),
  );

  if (systemInfoOnly) {
    return {
      systemInfoOnly: true,
      defaultBenchmarks: false,
      selectedBenchmarks: [],
    };
  }

  if (defaultBenchmarks) {
    return {
      systemInfoOnly: false,
      defaultBenchmarks: true,
      selectedBenchmarks: ASCT_DEFAULT_BENCHMARKS,
    };
  }

  if (explicitlySelectedBenchmarks.length > 0) {
    return {
      systemInfoOnly: false,
      defaultBenchmarks: false,
      selectedBenchmarks: explicitlySelectedBenchmarks,
    };
  }

  return {
    systemInfoOnly: false,
    defaultBenchmarks: true,
    selectedBenchmarks: ASCT_DEFAULT_BENCHMARKS,
  };
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
        `System info only is enabled, so benchmark selections are ignored: ${ignoredLabels.join(', ')}`,
      );
    }
  } else if (defaultBenchmarks) {
    if (explicitlySelectedBenchmarks.length > 0) {
      context.writeUserMessage(
        'warn',
        `Default benchmarks is enabled, so explicit benchmark selections are ignored: ${explicitlySelectedBenchmarks.map((benchmark) => benchmark.label).join(', ')}`,
      );
    }
    context.writeUserMessage(
      'info',
      `Using the ASCT default benchmark set: ${selectedBenchmarks.map((benchmark) => benchmark.label).join(', ')}.`,
    );
  } else {
    if (explicitlySelectedBenchmarks.length === 0) {
      context.writeUserMessage(
        'info',
        `No benchmarks explicitly selected; defaulting to ${selectedBenchmarks.map((benchmark) => benchmark.label).join(', ')}.`,
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

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 */
function renderAsct(context) {
  const renderers = /** @type {any[]} */ ([]);
  const visualizations = [];

  const entity = 'tool/asct/0/output';
  const csvFileDiscovery = getCreatedCsvFiles(context, entity);
  const createdCsvFiles = csvFileDiscovery.files;

  const addCsvRenderer = (rendererId, file) => {
    renderers.push({
      type: 'CSV',
      id: rendererId,
      config: {
        component: `${entity}/${file}`,
      },
    });
  };

  const openDirectoryRendererId = 'asct_open_directory_renderer';
  renderers.push({
    type: 'DummyRenderer',
    id: openDirectoryRendererId,
    config: {
      schema: [],
      content: [],
    },
  });

  visualizations.push(
    /** @type {any} */ ({
      type: 'open_directory',
      rendererId: openDirectoryRendererId,
      id: 'asct_open_directory',
      title: 'Summary',
      description: '',
      config: {
        tool_name: TOOL_ASCT_NAME,
        title: 'System Characterization report generated',
        description: getSummaryDescription(csvFileDiscovery),
        caption:
          'Opens the folder on your machine containing the ASCT output files for this run.',
      },
    }),
  );

  // CSVs with tabular benchmark data rendered as generic grids.
  const dataCsvFiles = new Set([
    'idle-latency.csv',
    'peak-bandwidth.csv',
    'cross-numa-bandwidth.csv',
    'latency-sweep.csv',
    'bandwidth-sweep.csv',
    'loaded-latency.csv',
    'c2c-latency.csv',
  ]);

  const entries = ASCT_BENCHMARKS.map((benchmark) => ({
    id: benchmark.id,
    title: benchmark.label,
    description: benchmark.description,
    file: `${benchmark.name}.csv`,
  })).filter(
    (entry) => dataCsvFiles.has(entry.file) && createdCsvFiles.has(entry.file),
  );

  for (const entry of entries) {
    const rendererId = `asct_csv_${entry.id}`;
    addCsvRenderer(rendererId, entry.file);

    visualizations.push({
      type: 'generic_grid',
      id: `asct_${entry.id}_table`,
      rendererId: rendererId,
      title: entry.title,
      description: entry.description,
      config: {
        autoSizeColumns: true,
        data_source: {
          tables: {
            table: [{ renderer_id: rendererId, output: 'csv' }],
          },
        },
        customQuery: {
          tableNamePlaceholder: '__table__',
          query:
            "SELECT COLUMNS(c -> c = 'index' OR c = 'column0' OR c = '') AS \"-\", " +
            "COLUMNS(c -> c != 'index' AND c != 'column0' AND c != '') FROM __table__",
        },
      },
    });
  }

  // CSVs with key-value style content rendered as generic grids.
  const keyValueCsvFiles = new Set(['system-info.csv']);

  const keyValueEntries = [
    {
      id: 'system_info',
      title: 'System Information',
      description:
        'System information collected by ASCT, including CPU, memory, and storage details.',
      file: 'system-info.csv',
    },
    ...ASCT_BENCHMARKS.map((benchmark) => ({
      id: benchmark.id,
      title: benchmark.label,
      description: benchmark.description,
      file: `${benchmark.name}.csv`,
    })),
  ].filter(
    (entry) =>
      keyValueCsvFiles.has(entry.file) && createdCsvFiles.has(entry.file),
  );

  for (const entry of keyValueEntries) {
    const rendererId = `asct_key_value_${entry.id}`;

    addCsvRenderer(rendererId, entry.file);

    visualizations.push({
      type: 'generic_grid',
      id: `asct_${entry.id}_table`,
      rendererId: rendererId,
      title: entry.title,
      description: entry.description,
      config: {
        autoSizeColumns: true,
        data_source: {
          tables: {
            table: [{ renderer_id: rendererId, output: 'csv' }],
          },
        },
        customQuery: {
          tableNamePlaceholder: '__table__',
          query:
            'SELECT * FROM (' +
            'SELECT \'\' AS "Name", \'\' AS "Value" WHERE FALSE ' +
            'UNION ALL ' +
            'SELECT * FROM __table__' +
            ') t',
        },
      },
    });
  }

  return { renderers, visualizations };
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 * @param {string} entity
 * @returns {{ files: Set<string>, warning: string | undefined }}
 */
function getCreatedCsvFiles(context, entity) {
  try {
    const files = new Set(
      context
        .listRunComponents(0, entity)
        .map((component) => component.fileName)
        .filter((fileName) => fileName.endsWith('.csv')),
    );

    if (files.size === 0) {
      const warning = 'APX did not find any ASCT CSV output files.';
      context.logWarn(warning);
      return { files, warning };
    }

    return { files, warning: undefined };
  } catch (err) {
    const warning = `Unable to list ASCT output files: ${String(err)}`;
    context.logWarn(warning);
    return { files: new Set(), warning };
  }
}

/**
 * @param {{ files: Set<string>, warning: string | undefined }} csvFileDiscovery
 */
function getSummaryDescription(csvFileDiscovery) {
  if (csvFileDiscovery.warning !== undefined) {
    return `${csvFileDiscovery.warning} The raw output files might still be available in the run directory.`;
  }

  return 'This run generated system information and microbenchmark results using the Arm System Characterization Tool (ASCT). The full report and raw output files are available in the run directory.';
}
