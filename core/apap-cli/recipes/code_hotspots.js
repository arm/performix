// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Code Hotspots Recipe Definition

// @ts-check
const {
  findTimelineCounterBindings,
  buildTimelineSQLRendererBundle,
} = require('./lib/timeline_sql_renderer');
const { buildNeoprofTimelineVisualization } = require('./lib/timeline_config');
const { buildJavaAnalysisRender } = require('./lib/java_analysis_render');
const {
  NEOPROF_TIMELINE_BIN_DURATIONS_NS,
} = require('../tool-integrations/neoprof_timeline');

const TOOL_NEOPROF = { name: 'neoprof', version: '1.1.0' };
const TOOL_WPERF = { name: 'wperf', version: '1.0.1' };
const CORE_TYPE_RENDER_PARAMETER = 'filter_core_type';
const NEOPROF_TIMELINE_COUNTER_PARQUET_PATTERN =
  'tool/neoprof/0/output/parquet/timeline/key_type=*/series_id=*/bin_duration=*/counter.parquet';
const NEOPROF_TIMELINE_CAPTURE_METADATA_PATTERN =
  'tool/neoprof/0/output/parquet/metadata/capture_metadata.json';
const COUNTER_CAPABILITY_COMPONENT_TYPE = {
  name: 'tool_capabilities/counter',
  version: '1.0',
};
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const telemetrySpecificationUnavailableMessageCode =
  'recipes.code_hotspots.TELEMETRY_SPECIFICATION_UNAVAILABLE';
const timelineDataIncompleteMessageCode =
  'recipes.code_hotspots.TIMELINE_DATA_INCOMPLETE';
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
        'Collect Java stack traces for JVM workloads. Java Flight Recorder data is also collected when available.',
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
    {
      id: CORE_TYPE_RENDER_PARAMETER,
      config: {
        type: 'string',
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
  const params = {
    mode: 'samples',
    sampling_frequency: samplingFreq,
    rich_data_capture: context.getParameter('rich_data_capture'),
    reformat_on_host: androidTarget || context.getParameter('reformat_on_host'),
  };

  if (!androidTarget) {
    const enableJavaAnalysis = context.getParameter('collect_java_stacks');
    params.collect_java_stacks = enableJavaAnalysis;
    params.collect_jfr = enableJavaAnalysis && context.isJfrCaptureEnabled();
    params.collect_dotnet_stacks = context.getParameter(
      'collect_dotnet_stacks',
    );
  }

  return params;
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
  const collectJavaStacks = context.getParameter('collect_java_stacks');
  const enableJavaAnalysis = collectJavaStacks && context.isJfrCaptureEnabled();
  const collectJitDumpsEnabled =
    collectJavaStacks || context.getParameter('collect_dotnet_stacks');
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
    if (enableJavaAnalysis) {
      allAdvice.push(javaAnalysisUnsupportedAdvice(TOOL_WPERF.name, 'Windows'));
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
  // The GUI probes readiness before a workload has necessarily been selected.
  // Defer Java-specific tool checks until the workload type is known.
  if (enableJavaAnalysis && !workload.Type) {
    params.collect_java_stacks = false;
    params.collect_jfr = false;
  }
  const tools = generateNeoprofConfig(workload, params);
  const toolResponses = context.probeTools(tools);
  const allAdvice = collectToolAdvice(tools, toolResponses);
  if (enableJavaAnalysis) {
    if (isAndroidTarget(targetInfo)) {
      allAdvice.push(
        javaAnalysisUnsupportedAdvice(TOOL_NEOPROF.name, 'Android'),
      );
    } else if (hostReformatEnabled) {
      allAdvice.push(
        javaAnalysisUnsupportedAdvice(
          TOOL_NEOPROF.name,
          'host-side reformatting',
        ),
      );
    }
  }
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

function javaAnalysisUnsupportedAdvice(toolName, mode) {
  return {
    ToolName: toolName,
    AdviceSeverity: 'error',
    MessageCode: readinessMessageCode,
    Metadata: {
      message: `Java analysis is not supported with ${mode}. Use a supported Linux target with target-side reformatting.`,
    },
    Cause: '',
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
    context.runTools(
      generateWperfConfig(workload, buildWperfParams(samplingFreq)),
    );
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
 * Retrieve and validate the counter metadata recorded for the neoprof tool
 * invocation that produced the timeline components.
 *
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 * @returns {Map<string, {title: string, description: string, units: string}>}
 */
function getTimelineCounterMetadata(context) {
  const capabilities = context
    .getToolCapabilities(0, {
      toolName: TOOL_NEOPROF.name,
      invocationIndex: 0,
    })
    .list();
  const metadataBySeriesKey = new Map();

  for (const capability of Object.values(capabilities)) {
    if (
      capability?.componentType?.name !==
        COUNTER_CAPABILITY_COMPONENT_TYPE.name ||
      capability?.componentType?.version !==
        COUNTER_CAPABILITY_COMPONENT_TYPE.version ||
      capability?.state !== 'collected'
    ) {
      continue;
    }

    const payload = capability.payload;
    if (!payload || typeof payload !== 'object') {
      throw new Error('Timeline counter capability payload must be an object');
    }
    if (!Number.isSafeInteger(payload.series_id) || payload.series_id < 0) {
      throw new Error(
        'Timeline counter capability series_id must be a non-negative safe integer',
      );
    }
    if (!Number.isSafeInteger(payload.key_type) || payload.key_type < 0) {
      throw new Error(
        'Timeline counter capability key_type must be a non-negative safe integer',
      );
    }
    if (typeof payload.title !== 'string' || payload.title.length === 0) {
      throw new Error('Timeline counter capability title is required');
    }
    if (typeof payload.description !== 'string') {
      throw new Error(
        'Timeline counter capability description must be a string',
      );
    }
    if (typeof payload.units !== 'string') {
      throw new Error('Timeline counter capability units must be a string');
    }
    const seriesKey = `key_${payload.key_type}_series_${payload.series_id}`;
    if (metadataBySeriesKey.has(seriesKey)) {
      throw new Error(`Duplicate timeline counter metadata for ${seriesKey}`);
    }

    metadataBySeriesKey.set(seriesKey, {
      title: payload.title,
      description: payload.description,
      units: payload.units,
    });
  }

  return metadataBySeriesKey;
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

const processFilter = {
  id: 'process',
  type: 'process_filter',
  title: 'Processes',
  rendererId: 'processes_and_threads',
  description: 'Include data from a selected process.',
  parameterBindings: {
    pid: 'filter_pid',
    // changing pid should clear tid
    tid: 'filter_tid',
  },
  config: {
    data_source: {
      tables: {
        processes: [
          { renderer_id: 'processes_and_threads', output: 'processes' },
        ],
      },
    },
    optionsQuery: {
      dataSource: 'processes',
      query:
        'SELECT CAST(pid AS INTEGER) AS pid, name FROM __table__ ORDER BY pid',
      tableNamePlaceholder: '__table__',
    },
  },
};

const threadFilter = {
  id: 'thread',
  type: 'thread_filter',
  title: 'Threads',
  rendererId: 'processes_and_threads',
  description: 'Include data from a selected thread.',
  parameterBindings: {
    // the process selected by `processFilter` is an input to thread filtering
    // as only a thread from that process can be selected
    pid: 'filter_pid',
    tid: 'filter_tid',
  },
  config: {
    data_source: {
      tables: {
        threads: [{ renderer_id: 'processes_and_threads', output: 'threads' }],
      },
    },
    optionsQuery: {
      dataSource: 'threads',
      query:
        'SELECT CAST(pid AS INTEGER) AS pid, CAST(tid AS INTEGER) AS tid, name FROM __table__ ORDER BY pid, tid',
      tableNamePlaceholder: '__table__',
    },
  },
};

const coreTypeFilter = {
  id: 'core_type',
  type: 'single_selection_list_filter',
  title: 'Core type',
  rendererId: 'supported_core_types',
  description: 'Include data from a selected CPU core type.',
  parameterBindings: {
    value: CORE_TYPE_RENDER_PARAMETER,
  },
  config: {
    data_source: {
      tables: {
        coreTypes: [
          {
            renderer_id: 'supported_core_types',
            output: 'supported_core_types',
          },
        ],
      },
    },
    optionsQuery: {
      dataSource: 'coreTypes',
      query:
        'SELECT name AS value, name AS label FROM __table__ GROUP BY name ORDER BY MIN(core_number), name',
      tableNamePlaceholder: '__table__',
    },
    allowNone: true,
    emptyMessage: 'No supported core types are available for this run.',
  },
};

function enableFilterIfAvailable(filter, runDescription, hasRichCapture) {
  // Treat missing parameters as disabled; only an explicit true enables global filtering.
  const richDataCaptureEnabled =
    runDescription.Parameters.rich_data_capture === true ||
    runDescription.Parameters.include_raw_data === true;

  if (!richDataCaptureEnabled) {
    return {
      ...filter,
      disabled: {
        reason:
          'Global filtering is unavailable for this run. Re-run the recipe with "Collect rich data" enabled.',
      },
    };
  }
  if (!runDescription.IsRunPhaseTwoComplete) {
    return {
      ...filter,
      disabled: {
        reason: runDescription.IsRunInProgress
          ? 'Unavailable until all capture data has been retrieved from the target.'
          : 'Unavailable because the run ended before all capture data was retrieved from the target.',
      },
    };
  }
  if (!hasRichCapture) {
    return {
      ...filter,
      disabled: {
        reason:
          'Global filtering is unavailable because this run has no retained rich capture data.',
      },
    };
  }
  return filter;
}

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
  const supportsFiltering =
    context.isRerenderingEnabled() &&
    tool.name === TOOL_NEOPROF.name &&
    !isComparison;
  const hasRichCapture =
    supportsFiltering &&
    context
      .listRunComponents(0, `tool/${tool.name}/0/**`)
      .some((component) => component.componentType.name === 'capture_apc');
  const coreTypeFilterIsAvailable =
    supportsFiltering &&
    (runDescription.Parameters.rich_data_capture === true ||
      runDescription.Parameters.include_raw_data === true) &&
    runDescription.IsRunPhaseTwoComplete &&
    hasRichCapture;
  const filterCoreType = coreTypeFilterIsAvailable
    ? (context.getRenderParameter(CORE_TYPE_RENDER_PARAMETER) ?? null)
    : null;
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

  let noDataMessageConfig = {};

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
  const slAnalyzeRerenderDependency = [{ renderer_id: 'sl_analyze' }];
  let isSlAnalyzeRerendering = false;

  function withSlAnalyzeRerenderDependency(dataSource) {
    return isSlAnalyzeRerendering
      ? { ...dataSource, renderers: slAnalyzeRerenderDependency }
      : dataSource;
  }

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
  const filters = [];
  renderers.push({
    type: 'TargetInfoRenderer',
    id: 'target_info',
    config: { entity: `tool/${tool.name}/0/` },
  });
  // SlAnalyzeRenderer and ProcessesAndThreadsParser are only applicable to neoprof since wperf does not capture an apc dir.
  if (supportsFiltering) {
    const slAnalyzeConfig = { entity: `tool/${tool.name}/0/` };
    let isFiltering = false;
    if (filterTid !== null && Number.isFinite(filterTid)) {
      slAnalyzeConfig.filter_tid = filterTid;
      isFiltering = true;
    } else if (filterPid !== null && Number.isFinite(filterPid)) {
      slAnalyzeConfig.filter_pid = filterPid;
      isFiltering = true;
    }
    if (
      filterStartTimeNs !== null &&
      Number.isFinite(filterStartTimeNs) &&
      filterStartTimeNs >= 0
    ) {
      slAnalyzeConfig.filter_start_time_ns = Math.round(filterStartTimeNs);
      isFiltering = true;
    }
    if (
      filterEndTimeNs !== null &&
      Number.isFinite(filterEndTimeNs) &&
      filterEndTimeNs >= 0
    ) {
      slAnalyzeConfig.filter_end_time_ns = Math.round(filterEndTimeNs);
      isFiltering = true;
    }
    if (filterCoreType !== null) {
      slAnalyzeConfig.filter_core_type = filterCoreType;
      slAnalyzeConfig.data_source = {
        tables: {
          target_info_cpus: [
            { renderer_id: 'target_info', output: 'target_info_cpus' },
          ],
        },
      };
      isFiltering = true;
    }

    if (isFiltering) {
      noDataMessageConfig = {
        noDataMessage:
          'No samples match the selected filter values. Try loosening or clearing filters.',
      };
    }

    renderers.push({
      type: 'SlAnalyzeRenderer',
      id: 'sl_analyze',
      config: slAnalyzeConfig,
    });
    isSlAnalyzeRerendering = true;
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
    renderers.push({
      type: 'SupportedCoreTypes',
      id: 'supported_core_types',
      config: {
        data_source: {
          tables: {
            target_info_cpus: [
              { renderer_id: 'target_info', output: 'target_info_cpus' },
            ],
          },
        },
      },
    });

    filters.push(
      enableFilterIfAvailable(coreTypeFilter, runDescription, hasRichCapture),
    );
    filters.push(
      enableFilterIfAvailable(timeRangeFilter, runDescription, hasRichCapture),
    );
    filters.push(
      enableFilterIfAvailable(processFilter, runDescription, hasRichCapture),
    );
    filters.push(
      enableFilterIfAvailable(threadFilter, runDescription, hasRichCapture),
    );
  }
  renderers.push(
    {
      type: 'StreamlineAnalyzeSymbols',
      id: 'streamline_symbols',
      config: {
        entity: `tool/${tool.name}/0/`,
        data_source: withSlAnalyzeRerenderDependency({}),
      },
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
        data_source: withSlAnalyzeRerenderDependency(dataSource),
        entity: `tool/${tool.name}/0/`,
        ...(filterCoreType === null ? {} : { cpu_name: filterCoreType }),
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
        data_source: withSlAnalyzeRerenderDependency(dataSource),
        ...(filterCoreType === null ? {} : { cpu_name: filterCoreType }),
      },
    },
    {
      type: 'SourceCodeAttribution',
      id: 'source_code_attribution',
      config: {
        entity: `tool/${tool.name}/0/`,
        data_source: withSlAnalyzeRerenderDependency(sourceFiles),
      },
    },
    {
      type: 'DisassemblyRenderer',
      id: 'disassembly',
      config: {
        entity: `tool/${tool.name}/0/`,
        data_source: withSlAnalyzeRerenderDependency(disassembly),
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
            ...noDataMessageConfig,
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
            ...noDataMessageConfig,
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
            ...noDataMessageConfig,
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
        const captureMetadataComponents = context.listRunComponents(
          0,
          NEOPROF_TIMELINE_CAPTURE_METADATA_PATTERN,
        );
        if (captureMetadataComponents.length > 1) {
          throw new Error(
            'Code Hotspots supports only one NeoProf timeline capture metadata component',
          );
        }
        if (captureMetadataComponents.length === 1) {
          const captureMetadata = JSON.parse(
            context.readRunComponent(
              0,
              captureMetadataComponents[0].relativePath,
            ),
          );
          const timelineVisualization = buildNeoprofTimelineVisualization({
            timelineSources: timelineSourceBundle.timelineSources,
            captureMetadata,
            expectedBinDurations: NEOPROF_TIMELINE_BIN_DURATIONS_NS,
            incompleteCatalogueMessageCode: timelineDataIncompleteMessageCode,
            metadataBySeriesKey: getTimelineCounterMetadata(context),
          });

          renderers.push(...timelineSourceBundle.renderers);
          visualizations.push(...timelineVisualization.visualizations);
        }
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

  const javaRender = buildJavaAnalysisRender(
    context,
    'tool/neoprof/0/java/parquet',
    visualizations,
  );

  return {
    renderers: [...renderers, ...javaRender.renderers],
    ui: {
      visualizations: javaRender.ui.visualizations,
      side_panel_filters: [...filters, ...javaRender.ui.side_panel_filters],
    },
  };
}
