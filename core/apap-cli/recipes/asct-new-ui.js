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
  name: 'asct-new-ui',
  title: 'System Characterization',
  version: '1.0',
  api_version: '1.0.0',
  status: 'experimental',
  description:
    'EXPERIMENTAL This *preview* recipe runs the Arm System Characterization Tool (ASCT) to collect system information and microbenchmark results. It measures memory latency/bandwidth behavior to help with platform bring-up, tuning, and architectural comparisons.',
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
 * Convert ASCT's current column-oriented ubench JSON shape into primitive rows
 * that generic table visualizations and later chart queries can control.
 * @param {string} relativePath
 */
function buildASCTJSONTableSQL(relativePath) {
  return `
    WITH source AS (
      SELECT row_to_json(raw_row) AS document
      FROM read_json_auto({{path:${relativePath}}}) AS raw_row
    ),
    metrics AS (
      SELECT
        metric.key AS metric,
        metric.value AS rows_by_key
      FROM source
      CROSS JOIN json_each(document) AS metric
      WHERE json_type(metric.value) = 'OBJECT'
    ),
    metric_values_by_key AS (
      SELECT
        value_by_key.key AS row_key,
        metric,
        value_by_key.value AS raw_value,
        json_type(value_by_key.value) AS value_type
      FROM metrics
      CROSS JOIN json_each(rows_by_key) AS value_by_key
    )
    SELECT
      row_key,
      metric,
      CASE
        WHEN value_type = 'VARCHAR' THEN json_extract_string(raw_value, '$')
        ELSE raw_value::VARCHAR
      END AS value,
      value_type
    FROM metric_values_by_key
    ORDER BY try_cast(row_key AS INTEGER), row_key, metric
  `;
}

const ASCT_C2C_LATENCY_RENDERER_ID = 'asct_csv_benchmark_c2c_latency';

/**
 * @param {string} rendererId
 * @param {string} output
 */
function makeRendererDataSource(rendererId, output) {
  return [{ renderer_id: rendererId, output }];
}

const ASCT_C2C_LATENCY_DATA_SOURCE = makeRendererDataSource(
  ASCT_C2C_LATENCY_RENDERER_ID,
  'csv',
);

const ASCT_CSV_ANALYSIS_VISUALIZATIONS = [
  {
    key: 'peakBandwidthGrid',
    benchmarkName: 'peak-bandwidth',
    config: {
      customQuery: {
        tableNamePlaceholder: '__table__',
        query:
          'SELECT "% of Peak Theoretical", "Peak BW [GB/s]", "Traffic type" ' +
          'FROM __table__',
      },
      noDataMessage: 'Peak bandwidth data is unavailable for this run.',
      noDataStatus: 'info',
      showColumnFilters: false,
      showColumnMenu: false,
    },
  },
  {
    key: 'coreToCoreLatencyHeatmap',
    benchmarkName: 'c2c-latency',
    config: {
      xColumn: 'source_core',
      yColumn: 'target_core',
      valueColumn: 'latency_ns',
      xLabel: 'Source core',
      yLabel: 'Target core',
      valueLabel: 'Latency',
      valueUnit: 'ns',
      data_source: {
        tables: {
          table: ASCT_C2C_LATENCY_DATA_SOURCE,
        },
      },
      customQuery: {
        tableNamePlaceholder: '{table}',
        query:
          'SELECT ' +
          'try_cast("CPUA" AS VARCHAR) AS "source_core", ' +
          'try_cast("CPUB" AS VARCHAR) AS "target_core", ' +
          'try_cast("Latency" AS DOUBLE) AS "latency_ns" ' +
          'FROM {table} ' +
          'WHERE "CPUA" IS NOT NULL AND "CPUB" IS NOT NULL AND "Latency" IS NOT NULL ' +
          'ORDER BY try_cast("CPUA" AS INTEGER), try_cast("CPUB" AS INTEGER)',
      },
    },
  },
];

const ASCT_JSON_ANALYSIS_VISUALIZATIONS = [
  {
    key: 'latencySweepLineChart',
    benchmarkName: 'latency-sweep',
    fileName: 'latency-sweep.ubench.json',
    config: {
      xAxisTitle: 'Message size (bytes)',
      xAxisType: 'log',
      xAxisLogBase: 2,
      xAxisMin: 'dataMin',
      yAxisTitle: 'Latency (ns)',
      yAxisType: 'log',
      yAxisLogBase: 2,
      yAxisMin: 'dataMin',
      pointSize: 7,
      enableZoom: false,
      series: [
        {
          type: 'single',
          name: 'Average latency',
          xColumn: 'size_bytes',
          yColumn: 'average_latency_ns',
        },
      ],
      customQuery: {
        tableNamePlaceholder: '__table__',
        query: `
          WITH chart_values AS (
            SELECT
              row_key,
              max(
                CASE WHEN metric = 'sizes' THEN try_cast(value AS DOUBLE) END
              ) AS size_bytes,
              max(
                CASE
                  WHEN metric = 'average_latency_ns'
                  THEN try_cast(value AS DOUBLE)
                END
              ) AS average_latency_ns
            FROM __table__
            WHERE metric IN ('sizes', 'average_latency_ns')
            GROUP BY row_key
          )
          SELECT size_bytes, average_latency_ns
          FROM chart_values
          WHERE size_bytes IS NOT NULL
            AND average_latency_ns IS NOT NULL
          ORDER BY size_bytes
        `,
      },
    },
  },
  {
    key: 'latencySweepGrid',
    benchmarkName: 'latency-sweep',
    fileName: 'latency-sweep.ubench.json',
    config: {
      customQuery: {
        tableNamePlaceholder: '__table__',
        query: `
          WITH pivoted AS (
            PIVOT __table__
            ON metric
            USING first(value)
            GROUP BY row_key
          )
          SELECT * EXCLUDE (row_key)
          FROM pivoted
          ORDER BY try_cast(row_key AS INTEGER), row_key
        `,
      },
      noDataMessage: 'Latency sweep data is unavailable for this run.',
      noDataStatus: 'info',
      showColumnFilters: false,
      showColumnMenu: false,
    },
  },
  {
    key: 'bandwidthSweepLineChart',
    benchmarkName: 'bandwidth-sweep',
    fileName: 'bandwidth-sweep.ubench.json',
    config: {
      xAxisTitle: 'Message size (bytes)',
      xAxisType: 'log',
      xAxisLogBase: 2,
      xAxisMin: 'dataMin',
      yAxisTitle: 'Bandwidth (Mbps)',
      yAxisType: 'log',
      yAxisLogBase: 2,
      yAxisMin: 'dataMin',
      pointSize: 7,
      enableZoom: false,
      series: [
        {
          type: 'single',
          name: 'Total bandwidth',
          xColumn: 'sizes',
          yColumn: 'total_bandwidth_mbps',
        },
      ],
      customQuery: {
        tableNamePlaceholder: '__table__',
        query: `
          WITH chart_values AS (
            SELECT
              row_key,
              max(
                CASE WHEN metric = 'sizes' THEN try_cast(value AS DOUBLE) END
              ) AS sizes,
              max(
                CASE
                  WHEN metric = 'total_bandwidth_mbps'
                  THEN try_cast(value AS DOUBLE)
                END
              ) AS total_bandwidth_mbps
            FROM __table__
            WHERE metric IN ('sizes', 'total_bandwidth_mbps')
            GROUP BY row_key
          )
          SELECT sizes, total_bandwidth_mbps
          FROM chart_values
          WHERE sizes IS NOT NULL
            AND total_bandwidth_mbps IS NOT NULL
          ORDER BY sizes
        `,
      },
    },
  },
  {
    key: 'bandwidthSweepGrid',
    benchmarkName: 'bandwidth-sweep',
    fileName: 'bandwidth-sweep.ubench.json',
    config: {
      customQuery: {
        tableNamePlaceholder: '__table__',
        query: `
          WITH pivoted AS (
            PIVOT __table__
            ON metric
            USING first(value)
            GROUP BY row_key
          )
          SELECT * EXCLUDE (row_key)
          FROM pivoted
          ORDER BY try_cast(row_key AS INTEGER), row_key
        `,
      },
      noDataMessage: 'Bandwidth sweep data is unavailable for this run.',
      noDataStatus: 'info',
      showColumnFilters: false,
      showColumnMenu: false,
    },
  },
];

/**
 * @param {Record<string, any>} analysisConfig
 * @param {string} key
 * @param {any} visualizationConfig
 * @param {{renderer_id: string, output: string}[]} dataSource
 */
function addAsctAnalysisVisualization(
  analysisConfig,
  key,
  visualizationConfig,
  dataSource,
) {
  analysisConfig.data_source = analysisConfig.data_source || { tables: {} };
  analysisConfig.data_source.tables[key] = dataSource;
  analysisConfig[key] = {
    ...visualizationConfig,
    data_source: {
      tables: {
        table: dataSource,
      },
    },
  };
}

/**
 * @param {{entity: string, selectedBenchmarks: { id: string, name: string, label: string, defaultSelected: boolean, description: string }[]}} args
 */
function buildAsctAnalysisConfig({ entity, selectedBenchmarks }) {
  const analysisConfig = /** @type {Record<string, any>} */ ({
    data_source: { tables: {} },
  });
  let renderersToAdd = /** @type {any[]} */ ([]);

  // Change this to a map with the selected benchmark as the value
  const selectedBenchmarkNames = new Map(
    selectedBenchmarks.map((benchmark) => [benchmark.name, benchmark]),
  );

  ASCT_CSV_ANALYSIS_VISUALIZATIONS.forEach((v) => {
    const selectedBenchmark = selectedBenchmarkNames.get(v.benchmarkName);
    if (selectedBenchmark === undefined) {
      return;
    }

    const rendererId = `asct_csv_${selectedBenchmark.id}`;
    renderersToAdd.push({
      type: 'CSV',
      id: rendererId,
      config: {
        component: `${entity}/${`${selectedBenchmark.name}.csv`}`,
      },
    });

    addAsctAnalysisVisualization(
      analysisConfig,
      v.key,
      v.config,
      makeRendererDataSource(rendererId, 'csv'),
    );
  });

  ASCT_JSON_ANALYSIS_VISUALIZATIONS.forEach((v) => {
    const selectedBenchmark = selectedBenchmarkNames.get(v.benchmarkName);
    if (selectedBenchmark === undefined) {
      return;
    }
    const rendererId = `asct_json_${selectedBenchmark.id}_${v.key}`;

    renderersToAdd.push({
      type: 'SQL',
      id: rendererId,
      config: {
        sql: buildASCTJSONTableSQL(`${entity}/${v.fileName}`),
        output: {
          name: 'table',
          cardinality: 'one',
          component_type: {
            name: 'flat_table',
            schema_version: '1.0',
          },
        },
      },
    });

    addAsctAnalysisVisualization(
      analysisConfig,
      v.key,
      v.config,
      makeRendererDataSource(rendererId, 'table'),
    );
  });

  return { analysisConfig, renderersToAdd };
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 */
function renderAsct(context) {
  const runDescriptions = context.getRunDescriptions();
  const renderers = /** @type {any[]} */ ([]);
  const visualizations = [];

  const entity = 'tool/asct/0/output';
  const createdCsvFiles = getCreatedCsvFiles(context, entity).files;

  const params =
    runDescriptions.length > 0 ? runDescriptions[0].Parameters : {};

  // Apply the shared selection rules so rendering matches run/ready behavior.
  const { selectedBenchmarks } = resolveBenchmarkSelection((id) => params[id]);

  const openDirectoryRendererId = 'asct_open_directory_renderer';
  renderers.push({
    type: 'DummyRenderer',
    id: openDirectoryRendererId,
    config: {
      schema: [],
      content: [],
    },
  });

  const asctAnalysisConfig = buildAsctAnalysisConfig({
    entity,
    selectedBenchmarks,
  });

  renderers.push(...asctAnalysisConfig.renderersToAdd);

  visualizations.push({
    type: 'asct_analysis',
    rendererId: openDirectoryRendererId,
    id: 'asct_analysis',
    title: 'Analysis',
    description: '',
    config: asctAnalysisConfig.analysisConfig,
  });

  if (createdCsvFiles.has('system-info.csv')) {
    const rendererId = 'asct_key_value_system_info';

    renderers.push({
      type: 'SQL',
      id: rendererId,
      config: {
        sql: `
          SELECT *
          FROM read_csv(
            {{path:${entity}/system-info.csv}},
            header = false,
            columns = {'key': 'VARCHAR', 'value': 'VARCHAR'}
          )
        `,
        output: {
          name: 'table',
          component_type: {
            name: 'flat_table',
            schema_version: '1.0',
          },
        },
      },
    });

    visualizations.push({
      type: 'asct_system_information',
      id: 'asct_system_info_table',
      rendererId: rendererId,
      title: 'System Information',
      description:
        'System information collected by ASCT, including CPU, memory, and storage details.',
      config: {
        data_source: {
          tables: {
            systemInformation: [{ renderer_id: rendererId, output: 'table' }],
          },
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
        .listRunComponents(0, `${entity}/**/*.csv`)
        .map((component) => component.fileName),
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
