// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Code Hotspots Recipe Definition

// @ts-check
const {
  findTimelineCounterBindings,
  buildTimelineSQLRendererBundle,
} = require('./lib/timeline_sql_renderer');

const TOOL_NEOPROF = { name: 'neoprof', version: '1.1.0' };
const TOOL_WPERF = { name: 'wperf', version: '1.0.1' };
const NEOPROF_TIMELINE_COUNTER_PARQUET_PATTERN =
  'tool/neoprof/0/output/parquet/timeline/series_id=*/bin_duration=*/counter.parquet';
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const telemetrySpecificationUnavailableMessageCode =
  'recipes.code_hotspots.TELEMETRY_SPECIFICATION_UNAVAILABLE';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'code_hotspots',
  title: 'Code Hotspots',
  version: '1.0',
  api_version: '1.0.2',
  status: 'stable',
  description:
    'The Code Hotspots recipe shows which parts of your code consume the most CPU time. It helps you quickly find and fix performance bottlenecks by identifying the functions and lines where optimization will have the most impact.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Windows' }],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_WPERF.name,
          version: TOOL_WPERF.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Android' },
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_NEOPROF.name,
          version: TOOL_NEOPROF.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'sampling_freq',
      required: false,
      label: 'Sampling Frequency',
      description:
        "Select the sampling frequency. The 'normal' frequency is suitable for most workloads, while 'high' provides more detailed information at the cost of increased overhead.",
      config: {
        type: 'single_select',
        options: [
          { value: 'low', label: 'Low' },
          { value: 'normal', label: 'Normal' },
          { value: 'high', label: 'High' },
        ],
        defaultValue: 'normal',
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
    {
      id: 'reformat_on_host',
      required: false,
      label: 'Reformat on host',
      description:
        'Run analysis on the host instead of the target. This is always enabled for Android targets but can be optionally enabled for Linux targets. collect_java_stacks and collect_dotnet_stacks are not currently supported when reformat_on_host is enabled.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'rich_data_capture',
      required: false,
      label: 'Collect rich data',
      description: `Enables the collection of rich data from the target, which enables advanced filtering functionality after the run completes. This can significantly increase host storage usage and transfer time.`,
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
  ],
  renderParameters: [
    {
      id: 'filter_pid',
      config: {
        type: 'number',
      },
    },
    {
      id: 'filter_tid',
      config: {
        type: 'number',
      },
    },
    {
      id: 'filter_start_time_ns',
      config: {
        type: 'number',
      },
    },
    {
      id: 'filter_end_time_ns',
      config: {
        type: 'number',
      },
    },
  ],
  readyStages: [
    {
      name: 'Checking recipe is ready',
      description: 'Check that the target can run the Code Hotspots recipe',
      exec: readyHotspots,
    },
  ],
  runStages: [
    {
      name: 'Collecting hotspots data',
      description:
        'This stage collects hotspot samples on the target and processes the captured data',
      exec: runHotspots,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderHotspots,
    },
  ],
};

/**
 * @param {string} samplingFreq
 * @param {import("./docs/jsdocs").Workload} workload
 */
function getToolsArg(samplingFreq, workload) {
  return {
    tools: [
      {
        name: TOOL_NEOPROF.name,
        args: ['-r', samplingFreq],
      },
    ],
    workload: workload,
  };
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
        name: TOOL_NEOPROF.name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * generateWperfConfig generates a ToolConfigurationsArg for the
 * wperf tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function generateWperfConfig(workload, params) {
  return {
    toolConfigs: [
      {
        name: TOOL_WPERF.name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * Build the neoprof parameter set used by agent-mode invocations.
 * @param {import("./docs/jsdocs").RunExecutionContext|import("./docs/jsdocs").ReadyExecutionContext} context
 * @param {string} samplingFreq
 * @returns {Object.<string, any>}
 */
function buildNeoprofParams(context, samplingFreq) {
  const androidTarget = isAndroidTarget(context.targetInfo());
  return {
    mode: 'samples',
    sampling_frequency: samplingFreq,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
    rich_data_capture: androidTarget
      ? false
      : context.getParameter('rich_data_capture'),
    reformat_on_host: androidTarget || context.getParameter('reformat_on_host'),
  };
}

/**
 * Build the wperf parameter set used by agent-mode invocations.
 * @param {string} samplingFreq
 * @returns {Object.<string, any>}
 */
function buildWperfParams(samplingFreq) {
  return {
    mode: 'samples',
    sampling_frequency: samplingFreq ? samplingFreq : 'normal',
  };
}

/**
 * Determine whether the current target OS is Windows.
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @returns {boolean}
 */
function isWindowsTarget(targetInfo) {
  const family = targetInfo?.Os?.OSFamily ?? '';
  return family.toLowerCase() === 'windows';
}

/**
 * Determine whether the current target OS is Android.
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @returns {boolean}
 */
function isAndroidTarget(targetInfo) {
  const family = targetInfo?.Os?.OSFamily ?? '';
  return family.toLowerCase() === 'android';
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @param {import("./docs/jsdocs").RecipeReadyAdvice[]} advice
 */
function addTelemetrySpecificationWarning(context, advice) {
  const cpuName = context.targetInfo().PrimaryCPUName;
  if (!context.getTelemetrySpecification(cpuName)) {
    advice.push({
      ToolName: '',
      AdviceSeverity: 'warning',
      MessageCode: telemetrySpecificationUnavailableMessageCode,
      Metadata: { cpuName },
      Cause: '',
    });
  }
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyHotspots(context) {
  const workload = context.getWorkload();
  const samplingFreq = context.getParameter('sampling_freq');
  const targetInfo = context.targetInfo();
  const hostReformatEnabled = context.getParameter('reformat_on_host');
  const collectJitDumpsEnabled =
    context.getParameter('collect_java_stacks') ||
    context.getParameter('collect_dotnet_stacks');
  const richDataCaptureEnabled = context.getParameter('rich_data_capture');

  if (isWindowsTarget(targetInfo)) {
    const tools = generateWperfConfig(workload, buildWperfParams(samplingFreq));
    const toolResponses = context.probeTools(tools);
    const allAdvice = collectToolAdvice(tools, toolResponses);
    if (collectJitDumpsEnabled) {
      allAdvice.push({
        ToolName: TOOL_WPERF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'JIT dump collection is not supported for Windows targets. Jitted symbols will not be available.',
        },
        Cause: '',
      });
    }
    if (hostReformatEnabled) {
      allAdvice.push({
        ToolName: TOOL_WPERF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'Reformatting on the host is not supported for Windows targets. Reformatting will be done on the target.',
        },
        Cause: '',
      });
    }
    if (richDataCaptureEnabled) {
      allAdvice.push({
        ToolName: TOOL_WPERF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'Rich data capture is not supported for Windows targets. Advanced filtering functionality will not be available.',
        },
        Cause: '',
      });
    }
    addTelemetrySpecificationWarning(context, allAdvice);
    return {
      status: toolStatusToRecipeStatus(allAdvice),
      advice: allAdvice,
    };
  }

  const params = buildNeoprofParams(context, samplingFreq);
  const tools = generateNeoprofConfig(workload, params);
  const toolResponses = context.probeTools(tools);
  const allAdvice = collectToolAdvice(tools, toolResponses);
  if (collectJitDumpsEnabled) {
    if (isAndroidTarget(targetInfo)) {
      allAdvice.push({
        ToolName: TOOL_NEOPROF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'JIT dump collection is not supported for Android targets. Jitted symbols will not be available.',
        },
        Cause: '',
      });
    } else if (hostReformatEnabled) {
      allAdvice.push({
        ToolName: TOOL_NEOPROF.name,
        AdviceSeverity: 'warning',
        MessageCode: readinessMessageCode,
        Metadata: {
          message:
            'JIT dump collection is not supported when reformatting on the host. Jitted symbols will not be available.',
        },
        Cause: '',
      });
    }
  }
  addTelemetrySpecificationWarning(context, allAdvice);
  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runHotspots(context) {
  const samplingFreq = context.getParameter('sampling_freq');
  const workload = context.getWorkload();
  const targetInfo = context.targetInfo();
  const windowsTarget = isWindowsTarget(targetInfo);

  if (windowsTarget) {
    context.runTools(generateWperfConfig(workload, buildWperfParams()));
    return;
  }

  context.runTools(
    generateNeoprofConfig(workload, buildNeoprofParams(context, samplingFreq)),
  );
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 * @return {{ tool: { name: string, version: string } | null, errorDescription: string | null }}
 */
function getRenderRunTool(context) {
  let result = { tool: null, errorDescription: null };
  const runDescriptions = context.getRunDescriptions();

  for (const runDesc of runDescriptions) {
    // If this is a legacy run where ToolsUsed is null - assume neoprof, else get the tool used.
    let currToolDescr = TOOL_NEOPROF;
    for (const toolName of runDesc.ToolsUsed ?? []) {
      if (toolName === TOOL_NEOPROF.name) {
        currToolDescr = TOOL_NEOPROF;
      } else if (toolName === TOOL_WPERF.name) {
        currToolDescr = TOOL_WPERF;
      } else {
        return {
          tool: null,
          errorDescription: `Unsupported tool "${toolName}" used in run - cannot render.`,
        };
      }
    }

    if (!result.tool) {
      result = { tool: currToolDescr, errorDescription: null };
    } else if (result.tool.name !== currToolDescr.name) {
      // Mixed tools in comparison - cannot render.
      return {
        tool: null,
        errorDescription: 'Mixed tools in comparison - cannot render.',
      };
    }
  }

  return result.tool ? result : { tool: TOOL_NEOPROF, errorDescription: null };
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 * @param {string} parameterId
 * @returns {number | null}
 */
function getRenderParameterIfExists(context, parameterId) {
  const param = context.getRenderParameter(parameterId);
  return param === null || param === undefined ? null : Number(param);
}

/**
 * @param {number} binDuration
 * @returns {string}
 */
function buildProvisionalTimelineSystemWideQuery(binDuration) {
  return `
    -- Presentation-layer query for provisional timeline charts.
    --
    -- The upstream timeline SQL bundle keeps source loading and interval
    -- expansion faithful to the parquet inputs, so gaps remain absent there.
    -- This final chart query sums values across all discovered device/thread
    -- pairs, then zero-fills uncovered bins. Treat this as display-only policy
    -- for now.
    WITH aggregated_series_points AS (
      SELECT
        x_start,
        SUM(value) AS value
      FROM {table}
      GROUP BY x_start
    ),
    x_bounds AS (
      SELECT
        MIN(x_start) AS min_x_start,
        MAX(x_start) AS max_x_start
      FROM aggregated_series_points
    ),
    x_domain AS (
      SELECT generated.x_start
      FROM x_bounds
      CROSS JOIN generate_series(
        CAST(x_bounds.min_x_start AS BIGINT),
        CAST(x_bounds.max_x_start AS BIGINT),
        ${binDuration}
      ) AS generated(x_start)
    )
    SELECT
      x_domain.x_start,
      COALESCE(aggregated_series_points.value, 0.0) AS value
    FROM x_domain
    LEFT JOIN aggregated_series_points
      ON aggregated_series_points.x_start = x_domain.x_start
    ORDER BY x_domain.x_start
  `.trim();
}

/**
 * @param {Array<{
 *   rawSeriesKey: string,
 *   rendererId: string,
 *   output: string,
 *   seriesId: number,
 *   binDuration: number,
 * }>} timelineSources
 * @returns {{ visualizations: any[] }}
 */
function buildProvisionalTimelineVisualization(timelineSources) {
  if (timelineSources.length === 0) {
    return { visualizations: [] };
  }

  const tables = {};
  const groups = {};
  const rendererId = timelineSources[0].rendererId;

  for (const [groupIndex, source] of timelineSources.entries()) {
    const groupKey = `${source.rawSeriesKey}_${source.binDuration}`;
    const binDuration = source.binDuration;
    const seriesLabel = `Series ${source.seriesId}`;
    tables[groupKey] = [
      {
        renderer_id: source.rendererId,
        output: source.output,
      },
    ];
    groups[groupKey] = {
      title: `${seriesLabel} (${binDuration} ns)`,
      type: 'line',
      index: groupIndex,
      description: `Provisional timeline series ${seriesLabel} at ${binDuration} ns resolution.`,
      config: {
        xAxisTitle: 'Time (ns)',
        yAxisTitle: 'Value',
        customQuery: {
          tableNamePlaceholder: '{table}',
          query: buildProvisionalTimelineSystemWideQuery(binDuration),
        },
        series: [
          {
            type: 'single',
            name: 'Total',
            xColumn: 'x_start',
            yColumn: 'value',
          },
        ],
      },
    };
  }

  return {
    visualizations: [
      {
        type: 'timeline',
        id: 'timeline',
        rendererId,
        title: 'Timeline',
        description: 'Preview provisional timeline data for hotspots analysis.',
        config: {
          xAxisUnit: 'ns',
          data_source: {
            tables,
          },
          groups,
        },
      },
    ],
  };
}

const timeRangeFilter = {
  id: 'time_range',
  type: 'time_range_filter',
  title: 'Time range',
  rendererId: 'time_range',
  description: 'Select the time region of interest',
  parameterBindings: {
    filter_start_time: 'filter_start_time_ns',
    filter_end_time: 'filter_end_time_ns',
  },
  config: {
    data_source: {
      tables: {
        timeLimits: [{ renderer_id: 'time_range', output: 'time_limits' }],
      },
    },
    rangeQuery: {
      dataSource: 'timeLimits',
      query:
        'SELECT CAST(MIN(start_time_ns) AS DOUBLE) AS start_time, CAST(MAX(end_time_ns) AS DOUBLE) AS end_time FROM __table__',
      tableNamePlaceholder: '__table__',
    },
    initialValuesQuery: {
      dataSource: 'timeLimits',
      query:
        'SELECT CAST(MIN(start_time_ns) AS DOUBLE) AS filter_start_time, CAST(MAX(end_time_ns) AS DOUBLE) AS filter_end_time FROM __table__',
      tableNamePlaceholder: '__table__',
    },
    unit: 'ns',
  },
};

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 */
function renderHotspots(context) {
  const isComparison = context.getRunDescriptions().length === 2;
  const runToolInfo = getRenderRunTool(context);

  if (runToolInfo.errorDescription) {
    throw {
      code: 'cli.cmd.run.render.RENDERER_FAILED',
      metadata: { failures: runToolInfo.errorDescription },
    };
  }

  const tool = runToolInfo.tool;
  const runDescription = context.getRunDescriptions()[0];
  const filterPid = getRenderParameterIfExists(context, 'filter_pid');
  const filterTid = getRenderParameterIfExists(context, 'filter_tid');
  const filterStartTimeNs = getRenderParameterIfExists(
    context,
    'filter_start_time_ns',
  );
  const filterEndTimeNs = getRenderParameterIfExists(
    context,
    'filter_end_time_ns',
  );
  const timeRangeNoDataMessage =
    filterStartTimeNs !== null || filterEndTimeNs !== null
      ? 'No samples match the selected time range. Try widening or clearing the time range filter.'
      : null;

  const dataSourceSingle = {
    tables: {
      symbols: [{ renderer_id: 'streamline_symbols', output: 'symbols' }],
      images: [{ renderer_id: 'streamline_symbols', output: 'images' }],
      target_info_cpus: [
        { renderer_id: 'target_info', output: 'target_info_cpus' },
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
      target_info_cpus: [
        {
          renderer_id: 'target_info',
          output: 'target_info_cpus',
          content_index: 0,
        },
        {
          renderer_id: 'target_info',
          output: 'target_info_cpus',
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

  const dataSourceCompareDrilldownStacks = {
    tables: {
      drilldown: [
        { renderer_id: 'drilldown', output: 'drilldown', content_index: 0 },
        { renderer_id: 'drilldown', output: 'drilldown', content_index: 1 },
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

  const dataSourceCompareDrilldownFlat = {
    tables: {
      drilldown: [
        { renderer_id: 'flat', output: 'drilldown', content_index: 0 },
        { renderer_id: 'flat', output: 'drilldown', content_index: 1 },
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

  let renderers = [];
  const topBarFilters = [];
  // SlAnalyzeRenderer and ProcessesAndThreadsParser are only applicable to neoprof since wperf does not capture an apc dir.
  if (
    context.isRerenderingEnabled() &&
    tool.name === TOOL_NEOPROF.name &&
    !isComparison
  ) {
    const slAnalyzeConfig = { entity: `tool/${tool.name}/0/` };
    if (filterPid !== null && Number.isFinite(filterPid) && filterPid > 0) {
      slAnalyzeConfig.filter_pid = filterPid;
    }
    if (filterTid !== null && Number.isFinite(filterTid) && filterTid > 0) {
      slAnalyzeConfig.filter_tid = filterTid;
    }
    if (
      filterStartTimeNs !== null &&
      Number.isFinite(filterStartTimeNs) &&
      filterStartTimeNs >= 0
    ) {
      slAnalyzeConfig.filter_start_time_ns = Math.round(filterStartTimeNs);
    }
    if (
      filterEndTimeNs !== null &&
      Number.isFinite(filterEndTimeNs) &&
      filterEndTimeNs >= 0
    ) {
      slAnalyzeConfig.filter_end_time_ns = Math.round(filterEndTimeNs);
    }
    renderers.push({
      type: 'SlAnalyzeRenderer',
      id: 'sl_analyze',
      config: slAnalyzeConfig,
    });
    renderers.push({
      type: 'ProcessesAndThreadsParser',
      id: 'processes_and_threads',
      config: { entity: `tool/${tool.name}/0/` },
    });
    renderers.push({
      type: 'TimeRangeParser',
      id: 'time_range',
      config: { entity: `tool/${tool.name}/0/` },
    });
    const renderTimeRangeFilter = { ...timeRangeFilter };

    // Treat a missing parameter as disabled; only an explicit true enables time-range filtering.
    const richDataCaptureEnabled =
      runDescription.Parameters.rich_data_capture === true;

    if (!richDataCaptureEnabled) {
      renderTimeRangeFilter.disabled = {
        reason:
          'Time-range filtering is unavailable for this run. Re-run the recipe with "Collect rich data" enabled.',
      };
    } else if (!runDescription.IsRunPhaseTwoComplete) {
      renderTimeRangeFilter.disabled = {
        reason: runDescription.IsRunInProgress
          ? 'Unavailable until all capture data has been retrieved from the target.'
          : 'Unavailable because the run ended before all capture data was retrieved from the target.',
      };
    }
    topBarFilters.push(renderTimeRangeFilter);
  }
  renderers.push(
    {
      type: 'StreamlineAnalyzeSymbols',
      id: 'streamline_symbols',
      config: { entity: `tool/${tool.name}/0/` },
    },
    {
      type: 'TargetInfoRenderer',
      id: 'target_info',
      config: { entity: `tool/${tool.name}/0/` },
    },
    {
      type: 'StreamlineAnalyzeFlatFunctions2',
      id: 'flat',
      config: {
        component: 'functions-capture-periodic_sampling.csv',
        'compute-metrics': [
          {
            type: 'percentage',
            'total-from': 'Periodic Samples (self)',
            columns: ['Periodic Samples (self)'],
            'relative-order-priority': 'higher',
          },
        ],
        data_source: dataSource,
        entity: `tool/${tool.name}/0/`,
      },
    },
    {
      type: 'StreamlineAnalyzeFunctionProfileRenderer2',
      id: 'drilldown',
      config: {
        'call-tree': 'call_tree_samples.json',
        entity: `tool/${tool.name}/0/`,
        measurements: [
          {
            component: 'callpath_self_samples.json',
            'column-suffix': 'self',
          },
          {
            component: 'callpath_total_samples.json',
            'column-suffix': 'total',
          },
        ],
        'compute-metrics': [
          {
            type: 'percentage',
            'total-from': 'Periodic Samples (self)',
            columns: ['Periodic Samples (self)', 'Periodic Samples (total)'],
            'relative-order-priority': 'higher',
          },
        ],
        data_source: dataSource,
      },
    },
    {
      type: 'SourceCodeAttribution',
      id: 'source_code_attribution',
      config: {
        entity: `tool/${tool.name}/0/`,
        data_source: sourceFiles,
      },
    },
    {
      type: 'DisassemblyRenderer',
      id: 'disassembly',
      config: {
        entity: `tool/${tool.name}/0/`,
        data_source: disassembly,
      },
    },
  );

  const visualizations = [
    isComparison
      ? {
          type: 'flame_graph_comparison',
          id: 'flame_graph',
          rendererId: 'compare_drilldown',
          title: 'Flame Graph Comparison',
          description:
            'Compare sampled stack traces between the current run and the baseline. The leaf function appears at the top of the graph, and its callers appear below it. The box width represents the number of samples. Red indicates functions sampled more often in the current run. Blue indicates functions sampled more often in the baseline. Use this view to identify call paths whose performance changed between runs.',
          config: {
            data_source: {
              tables: {
                deltas: [{ renderer_id: 'compare_drilldown', output: 'delta' }],
                measurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                measurementOrder: [
                  {
                    renderer_id: 'drilldown',
                    output: 'measurement_order',
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
              },
            },
          },
        }
      : {
          type: 'flame_graph',
          id: 'flame_graph',
          rendererId: 'drilldown',
          title: 'Flame Graph',
          description:
            'View sampled stack traces and identify hot code paths. The leaf function call appears at the top of the graph, and its callers appear below it. The box width represents how often a function appears in the samples.',
          config: {
            ...(timeRangeNoDataMessage
              ? { noDataMessage: timeRangeNoDataMessage }
              : {}),
            data_source: {
              tables: {
                callstack: [{ renderer_id: 'drilldown', output: 'drilldown' }],
                callstackMeasurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                callstackMeasurementOrder: [
                  {
                    renderer_id: 'drilldown',
                    output: 'measurement_order',
                  },
                ],
                flatFunctions: [{ renderer_id: 'flat', output: 'drilldown' }],
                flatFunctionsMeasurementOrder: [
                  {
                    renderer_id: 'flat',
                    output: 'measurement_order',
                  },
                ],
                flatFunctionsMeasurements: [
                  { renderer_id: 'flat', output: 'measurements' },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
                  },
                ],
              },
            },
          },
        },
    isComparison
      ? {
          type: 'flat_functions_comparison',
          id: 'functions',
          rendererId: 'compare_flat',
          title: 'Functions Comparison',
          description:
            'Compare per-function CPU time between runs to identify performance changes.',
          config: {
            data_source: {
              tables: {
                flatFunctionsCurrentRun: [
                  {
                    renderer_id: 'compare_flat',
                    output: 'aggregated_drilldown',
                    content_index: 1,
                  },
                ],
                flatFunctionsBaselineRun: [
                  {
                    renderer_id: 'compare_flat',
                    output: 'aggregated_drilldown',
                    content_index: 0,
                  },
                ],
                deltas: [{ renderer_id: 'compare_flat', output: 'delta_flat' }],
                measurements: [{ renderer_id: 'flat', output: 'measurements' }],
                measurementOrder: [
                  { renderer_id: 'flat', output: 'measurement_order' },
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
          id: 'functions',
          rendererId: 'flat',
          title: 'Functions',
          description: 'Identify functions that consume the most CPU time.',
          config: {
            ...(timeRangeNoDataMessage
              ? { noDataMessage: timeRangeNoDataMessage }
              : {}),
            data_source: {
              tables: {
                flatFunctions: [{ renderer_id: 'flat', output: 'drilldown' }],
                measurements: [{ renderer_id: 'flat', output: 'measurements' }],
                measurementOrder: [
                  { renderer_id: 'flat', output: 'measurement_order' },
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
          type: 'call_stack_comparison',
          id: 'call_stack',
          rendererId: 'compare_drilldown',
          title: 'Call Stack Comparison',
          description:
            'Compare CPU time performance metrics between runs, grouped by call path. Identify where execution cost shifted between runs. This view includes the function’s own execution time (self) and the time of the function and all the functions that called it (total).',
          config: {
            data_source: {
              tables: {
                deltas: [{ renderer_id: 'compare_drilldown', output: 'delta' }],
                measurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                measurementOrder: [
                  {
                    renderer_id: 'drilldown',
                    output: 'measurement_order',
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
          type: 'call_stack',
          id: 'call_stack',
          rendererId: 'drilldown',
          title: 'Call Stack',
          description:
            'View CPU time information for each function grouped by call path. This view includes the function’s own execution time (self) and the time of the function and all the functions that called it (total). Use this view to determine whether execution cost originates in a function or in its call chain.',
          config: {
            ...(timeRangeNoDataMessage
              ? { noDataMessage: timeRangeNoDataMessage }
              : {}),
            data_source: {
              tables: {
                drilldown: [{ renderer_id: 'drilldown', output: 'drilldown' }],
                measurements: [
                  { renderer_id: 'drilldown', output: 'measurements' },
                ],
                measurementOrder: [
                  {
                    renderer_id: 'drilldown',
                    output: 'measurement_order',
                  },
                ],
                symbols: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'symbols',
                  },
                ],
                images: [
                  {
                    renderer_id: 'streamline_symbols',
                    output: 'images',
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
        },
  ];

  if (context.isNeoprofTimelineEnabled() && !isComparison) {
    const timelineBindings = findTimelineCounterBindings(
      context,
      0,
      NEOPROF_TIMELINE_COUNTER_PARQUET_PATTERN,
    );
    if (timelineBindings.length > 0) {
      const timelineSourceBundle = buildTimelineSQLRendererBundle({
        bindings: timelineBindings,
      });
      if (timelineSourceBundle.renderers.length > 0) {
        const timelineVisualization = buildProvisionalTimelineVisualization(
          timelineSourceBundle.timelineSources,
        );

        renderers.push(...timelineSourceBundle.renderers);
        visualizations.push(...timelineVisualization.visualizations);
      }
    }
  }

  if (isComparison) {
    renderers.push({
      type: 'CompareDrilldownCallStacks',
      id: 'compare_drilldown',
      config: { data_source: dataSourceCompareDrilldownStacks },
    });
    renderers.push({
      type: 'CompareDrilldownFlat',
      id: 'compare_flat',
      config: {
        data_source: dataSourceCompareDrilldownFlat,
        aggregate_duplicate_symbols: true,
      },
    });
  }

  return topBarFilters.length > 0
    ? { renderers, ui: { visualizations, top_bar_filters: topBarFilters } }
    : { renderers, visualizations };
}
