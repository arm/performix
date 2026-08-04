// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// ASCT Recipe Definition

// @ts-check

const ASCT_VERSION = '0.6.1';
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
    description: 'Report idle memory latency across NUMA nodes.',
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
    description: 'Report memory bandwidth between NUMA nodes.',
  },
  {
    id: 'benchmark_latency_sweep',
    name: 'latency-sweep',
    label: 'Latency sweep',
    defaultSelected: true,
    description:
      'Measure latency across data sizes to reveal the cache hierarchy and help size other benchmarks.',
  },
  {
    id: 'benchmark_bandwidth_sweep',
    name: 'bandwidth-sweep',
    label: 'Bandwidth sweep',
    defaultSelected: true,
    description:
      'Measure bandwidth across data sizes to reveal the cache hierarchy.',
  },
  {
    id: 'benchmark_loaded_latency',
    name: 'loaded-latency',
    label: 'Loaded latency',
    defaultSelected: false,
    description:
      'Measure memory latency while background activity generates memory traffic. This benchmark can take several minutes to complete.',
  },
  {
    id: 'benchmark_c2c_latency',
    name: 'c2c-latency',
    label: 'Core-to-core latency',
    defaultSelected: true,
    description: 'Report latency between CPU cores.',
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
        'Run the default latency, bandwidth, NUMA, and core-to-core benchmarks. Loaded latency is not included and can extend the run.',
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
const ASCT_SYSTEM_INFO_RENDERER_ID = 'asct_key_value_system_info';

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
    key: 'numaLatencyMatrix',
    benchmarkName: 'idle-latency',
    config: {},
  },
  {
    key: 'numaBandwidthMatrix',
    benchmarkName: 'cross-numa-bandwidth',
    config: {},
  },
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
  {
    key: 'loadedLatencyLineChart',
    benchmarkName: 'loaded-latency',
    config: {
      xAxisTitle: 'Peak theoretical bandwidth',
      xAxisType: 'value',
      xAxisMin: 0,
      xAxisValueFormat: 'percent',
      yAxisTitle: 'Loaded latency',
      yAxisType: 'value',
      yAxisMin: 0,
      yAxisValueFormat: 'seconds',
      pointSize: 7,
      enableZoom: false,
      enableTooltip: true,
      series: [
        {
          type: 'single',
          name: 'Loaded latency',
          xColumn: 'peak_bandwidth_percent',
          yColumn: 'latency_seconds',
        },
      ],
      customQuery: {
        tableNamePlaceholder: '__table__',
        query: `
          -- Normalize ASCT's nanosecond output to seconds so the renderer can
          -- choose an appropriate display unit.
          SELECT
            try_cast("% of Peak Theoretical BW" AS DOUBLE) AS peak_bandwidth_percent,
            try_cast("Loaded latency [ns]" AS DOUBLE) / 1000000000 AS latency_seconds
          FROM __table__
          WHERE "% of Peak Theoretical BW" IS NOT NULL
            AND "Loaded latency [ns]" IS NOT NULL
          ORDER BY peak_bandwidth_percent
        `,
      },
    },
  },
  {
    key: 'loadedLatencyGrid',
    benchmarkName: 'loaded-latency',
    config: {
      // The CSV renderer exposes its row ordinal as `column0`; it is not ASCT data.
      customQuery: {
        tableNamePlaceholder: '__table__',
        query: "SELECT COLUMNS(c -> c != 'column0') FROM __table__",
      },
      noDataMessage: 'Loaded latency data is unavailable for this run.',
      noDataStatus: 'info',
      showColumnFilters: false,
      showColumnMenu: false,
    },
  },
];

const ASCT_JSON_ANALYSIS_VISUALIZATIONS = [
  {
    key: 'latencySweepLineChart',
    benchmarkName: 'latency-sweep',
    fileName: 'latency-sweep.ubench.json',
    config: {
      xAxisTitle: 'Message size',
      xAxisType: 'log',
      xAxisLogBase: 2,
      xAxisMin: 'dataMin',
      xAxisValueFormat: 'iec-bytes',
      yAxisTitle: 'Latency',
      yAxisType: 'log',
      yAxisLogBase: 2,
      yAxisMin: 'dataMin',
      yAxisValueFormat: 'seconds',
      pointSize: 7,
      enableZoom: false,
      enableTooltip: true,
      series: [
        {
          type: 'single',
          name: 'Average latency',
          xColumn: 'size_bytes',
          yColumn: 'average_latency_seconds',
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
          -- Normalize ASCT's nanosecond output to seconds so the renderer can
          -- choose an appropriate display unit.
          SELECT
            size_bytes,
            average_latency_ns / 1000000000 AS average_latency_seconds
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
      xAxisTitle: 'Message size',
      xAxisType: 'log',
      xAxisLogBase: 2,
      xAxisMin: 'dataMin',
      xAxisValueFormat: 'iec-bytes',
      yAxisTitle: 'Bandwidth',
      yAxisType: 'log',
      yAxisLogBase: 2,
      yAxisMin: 'dataMin',
      yAxisValueFormat: 'iec-bytes-per-second',
      pointSize: 7,
      enableZoom: false,
      enableTooltip: true,
      series: [
        {
          type: 'single',
          name: 'Total bandwidth',
          xColumn: 'size_bytes',
          yColumn: 'total_bandwidth_bytes_per_second',
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
                  WHEN metric = 'total_bandwidth_mbps'
                  THEN try_cast(value AS DOUBLE)
                END
              ) AS total_bandwidth_mbps
            FROM __table__
            WHERE metric IN ('sizes', 'total_bandwidth_mbps')
            GROUP BY row_key
          )
          -- ASCT reports decimal MB/s; normalize to bytes/s so the renderer
          -- can choose an appropriate IEC display unit.
          SELECT
            size_bytes,
            total_bandwidth_mbps * 1000000 AS total_bandwidth_bytes_per_second
          FROM chart_values
          WHERE size_bytes IS NOT NULL
            AND total_bandwidth_mbps IS NOT NULL
          ORDER BY size_bytes
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
 * @param {{
 *   dataSourceKey: string,
 *   configKey: string,
 *   config: any,
 *   dataSource: {renderer_id: string, output: string}[],
 * }} entry
 */
function addAsctAnalysisConfigEntry(
  analysisConfig,
  { dataSourceKey, configKey, config, dataSource },
) {
  analysisConfig.data_source = analysisConfig.data_source || { tables: {} };
  analysisConfig.data_source.tables[dataSourceKey] = dataSource;
  analysisConfig[configKey] = {
    ...config,
    data_source: {
      tables: {
        table: dataSource,
      },
    },
  };
}

const ASCT_CACHE_SIZES_TABLE_KEY = 'cacheSizes';
const ASCT_CACHE_SIZE_MARKERS_CONFIG_KEY = 'cacheSizeMarkers';
const ASCT_CACHE_SIZES_DATA_SOURCE = makeRendererDataSource(
  ASCT_SYSTEM_INFO_RENDERER_ID,
  'table',
);

// system-info.csv names cache capacities using K/M/G/T. Convert them to
// bytes so the marker values align with the sweep x-axis data.
const ASCT_CACHE_SIZE_MARKERS_CONFIG = {
  data_source: {
    tables: {
      table: ASCT_CACHE_SIZES_DATA_SOURCE,
    },
  },
  customQuery: {
    tableNamePlaceholder: '__table__',
    query: `
      WITH cache_sizes AS (
        SELECT
          regexp_extract(
            "key",
            '^sys_hw\\.caches\\.(L1D|L2U|L3U)\\s+([0-9]+(?:\\.[0-9]+)?[KkMmGgTt])(?:\\s+|$)',
            1
          ) AS level,
          regexp_extract(
            "key",
            '^sys_hw\\.caches\\.(L1D|L2U|L3U)\\s+([0-9]+(?:\\.[0-9]+)?[KkMmGgTt])(?:\\s+|$)',
            2
          ) AS size
        FROM __table__
        WHERE regexp_matches(
          "key",
          '^sys_hw\\.caches\\.(L1D|L2U|L3U)\\s+[0-9]+(?:\\.[0-9]+)?[KkMmGgTt](?:\\s+|$)'
        )
      )
      SELECT
        level || ' cache' AS name,
        try_cast(left(size, length(size) - 1) AS DOUBLE) *
          CASE upper(right(size, 1))
            WHEN 'K' THEN 1024
            WHEN 'M' THEN 1024 * 1024
            WHEN 'G' THEN 1024 * 1024 * 1024
            WHEN 'T' THEN 1024 * 1024 * 1024 * 1024
          END AS x
      FROM cache_sizes
      WHERE try_cast(left(size, length(size) - 1) AS DOUBLE) > 0
      ORDER BY x
    `,
  },
};

/**
 * @param {{entity: string, selectedBenchmarks: { id: string, name: string, label: string, defaultSelected: boolean, description: string }[], includeCacheSizeMarkers: boolean}} args
 */
function buildAsctAnalysisConfig({
  entity,
  selectedBenchmarks,
  includeCacheSizeMarkers,
}) {
  const analysisConfig = /** @type {Record<string, any>} */ ({
    data_source: { tables: {} },
  });
  if (includeCacheSizeMarkers) {
    addAsctAnalysisConfigEntry(analysisConfig, {
      dataSourceKey: ASCT_CACHE_SIZES_TABLE_KEY,
      configKey: ASCT_CACHE_SIZE_MARKERS_CONFIG_KEY,
      config: ASCT_CACHE_SIZE_MARKERS_CONFIG,
      dataSource: ASCT_CACHE_SIZES_DATA_SOURCE,
    });
  }
  let renderersToAdd = /** @type {any[]} */ ([]);
  const csvRendererIds = new Set();

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
    if (!csvRendererIds.has(rendererId)) {
      renderersToAdd.push({
        type: 'CSV',
        id: rendererId,
        config: {
          component: `${entity}/${`${selectedBenchmark.name}.csv`}`,
        },
      });
      csvRendererIds.add(rendererId);
    }

    addAsctAnalysisConfigEntry(analysisConfig, {
      dataSourceKey: v.key,
      configKey: v.key,
      config: v.config,
      dataSource: makeRendererDataSource(rendererId, 'csv'),
    });
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

    addAsctAnalysisConfigEntry(analysisConfig, {
      dataSourceKey: v.key,
      configKey: v.key,
      config: v.config,
      dataSource: makeRendererDataSource(rendererId, 'table'),
    });
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
  const hasSystemInformation = createdCsvFiles.has('system-info.csv');

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
    includeCacheSizeMarkers: hasSystemInformation,
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

  if (hasSystemInformation) {
    const rendererId = ASCT_SYSTEM_INFO_RENDERER_ID;

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
