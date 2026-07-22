// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// CMN Analysis Recipe Definition

//@ts-check

let tool_cmn_name = 'cmn_analysis';
let tool_cmn_version = '0.1.0';
const DEFAULT_SAMPLING_FREQ = 'normal';
const DEFAULT_SAMPLE_DURATION_MS = 3000;

const SAMPLING_INTERVAL_MS = {
  low: 10000,
  normal: 5000,
  high: 3500,
};
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'cmn_analysis',
  title: 'CMN Analysis',
  version: '1.0',
  api_version: '1.0.2',
  status: 'experimental',
  description:
    'The CMN Analysis recipe shows a breakdown of topology and activity of the CMN interconnect on your target.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool',
          name: tool_cmn_name,
          version: tool_cmn_version,
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
      id: 'sample_duration',
      required: false,
      label: 'Sample Duration (milliseconds)',
      description:
        'Specify the duration (in milliseconds) for which the Topdown tool should sample CMN data. Longer durations may provide more comprehensive insights but can also increase the time taken for analysis. This must be shorter than the sampling interval determined by the sampling frequency, and at least 3000ms.',
      config: {
        type: 'input',
        defaultValue: `${DEFAULT_SAMPLE_DURATION_MS}`,
        custom: {},
      },
    },
  ],
  readyStages: [
    {
      name: 'Checking CMN Analysis is ready',
      description: 'Checking that all dependencies for CMN Analysis are met',
      exec: readyCMN,
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
      name: 'Running CMN Analysis',
      description:
        'This stage runs the CMN Analysis using the CMN Discover tool and the Topdown Tool, and retrieves the results',
      exec: runCMNAnalysis,
    },
  ],
  renderStages: [
    {
      name: 'Rendering CMN averages',
      description:
        'Generate CMN per-device average tables from collected CSV data',
      exec: renderCMNAnalysis,
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyCMN(context) {
  let workload = context.getWorkload();
  let params = {
    samplingFreq: context.getParameter('sampling_freq'),
    toolDuration: context.getParameter('sample_duration'),
  };

  let tools = {
    toolConfigs: [generateCmnToolConfig(workload, params)],
  };

  let toolResponse = context.probeTools(tools);

  let allAdvice = collectToolAdvice(tools, toolResponse);

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

const MIN_SAMPLE_DURATION = 1000;

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function validateRecipeParameters(context) {
  let samplingFreqParam = context.getParameter('sampling_freq');
  let samplingFreq =
    typeof samplingFreqParam === 'string' && samplingFreqParam.length > 0
      ? samplingFreqParam
      : DEFAULT_SAMPLING_FREQ;
  let samplingIntervalMs = resolveRecipeSamplingInterval(samplingFreq);

  let sampleDurationParam = context.getParameter('sample_duration');
  let parsedSampleDuration = Number(sampleDurationParam);
  if (!Number.isFinite(parsedSampleDuration) || parsedSampleDuration <= 0) {
    parsedSampleDuration = DEFAULT_SAMPLE_DURATION_MS;
  }

  if (parsedSampleDuration < MIN_SAMPLE_DURATION) {
    throw {
      code: 'recipes.cmn_analysis.SAMPLE_DURATION_TOO_SHORT',
      metadata: {
        sampleDuration: parsedSampleDuration,
        minSampleDuration: MIN_SAMPLE_DURATION,
      },
    };
  }

  if (parsedSampleDuration >= samplingIntervalMs) {
    throw {
      code: 'recipes.cmn_analysis.INVALID_SAMPLE_DURATION',
      metadata: {
        samplingFreq: samplingFreq,
        sampleDurationMs: parsedSampleDuration,
        samplingIntervalMs: samplingIntervalMs,
      },
    };
  }
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runCMNAnalysis(context) {
  let workload = context.getWorkload();
  let params = {
    samplingFreq: context.getParameter('sampling_freq'),
    toolDuration: context.getParameter('sample_duration'),
  };

  let tools = {
    toolConfigs: [generateCmnToolConfig(workload, params)],
  };

  context.runTools(tools);
}

/**
 * generateCmnToolConfig generates a ToolConfiguration for the
 * cmn_analysis tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfiguration}
 */
function generateCmnToolConfig(workload, params) {
  return {
    name: tool_cmn_name,
    params: params,
    workload: workload,
    env: {},
  };
}

function resolveRecipeSamplingInterval(freq) {
  if (typeof freq !== 'string' || !freq.length) {
    return SAMPLING_INTERVAL_MS[DEFAULT_SAMPLING_FREQ];
  }
  return (
    SAMPLING_INTERVAL_MS[freq] ?? SAMPLING_INTERVAL_MS[DEFAULT_SAMPLING_FREQ]
  );
}

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 */
function renderCMNAnalysis(context) {
  const entity = `tool/${tool_cmn_name}/0/`;
  const isComparison = context.getRunDescriptions().length === 2;
  const renderers = [];
  const visualizations = [];

  renderers.push({
    type: 'CmnCsvAverage',
    id: 'cmn_csv_average',
    config: {
      entity,
    },
  });

  if (isComparison) {
    renderers.push({
      type: 'CompareFlatTable',
      id: 'cmn_csv_compare',
      config: {
        data_source: {
          tables: {
            flat_tables: [
              {
                renderer_id: 'cmn_csv_average',
                output: 'cmn_average_table',
                content_index: 0,
              },
              {
                renderer_id: 'cmn_csv_average',
                output: 'cmn_average_table',
                content_index: 1,
              },
            ],
          },
        },
        join_columns: ['mesh_id', 'group', 'metric', 'units'],
        fixed_columns: [],
        compare_columns: ['*'],
        input_component_type: 'cmn_metrics',
        output_component_type: 'cmn_metrics_delta',
      },
    });
  }

  visualizations.push(
    isComparison
      ? {
          type: 'generic_grid',
          id: 'cmn_average_table_comparison',
          rendererId: 'cmn_csv_compare',
          title: 'CMN Per-Device Averages (Comparison)',
          description: '',
          config: {
            autoSizeColumns: false,
            data_source: {
              tables: {
                table: [
                  {
                    renderer_id: 'cmn_csv_compare',
                    output: 'delta_flat_table',
                  },
                ],
              },
            },
          },
        }
      : {
          type: 'generic_grid',
          id: 'cmn_average_table',
          rendererId: 'cmn_csv_average',
          title: 'CMN Per-Device Averages',
          description: '',
          config: {
            autoSizeColumns: false,
            data_source: {
              tables: {
                table: [
                  {
                    renderer_id: 'cmn_csv_average',
                    output: 'cmn_average_table',
                  },
                ],
              },
            },
          },
        },
  );

  const meshContentIndex = isComparison ? 1 : 0;
  const meshSvgComponents = context
    .listRunComponents(meshContentIndex, `${entity}cmn-mesh-svg`)
    .filter((component) => component.componentType.name === 'cmn-mesh-svg');

  for (const component of meshSvgComponents) {
    // Mesh images are expected to be named like "mesh0.svg"
    if (
      !component.fileName.startsWith('mesh') ||
      !component.fileName.endsWith('.svg')
    ) {
      throw new Error(`unexpected CMN mesh filename: ${component.fileName}`);
    }
    const meshIndex = component.fileName.slice('mesh'.length, -'.svg'.length);
    const titleIndex = meshSvgComponents.length > 1 ? ` ${meshIndex}` : '';
    const titleSuffix = isComparison ? ' (Current Run)' : '';

    visualizations.push({
      type: 'image',
      id: `cmn_mesh${meshIndex}`,
      rendererId: 'cmn_csv_average', // This visualization doesn't use a renderer, but a rendererId is required by the engine. Pass cmn_csv_average as a placeholder.
      title: `CMN Mesh Topology${titleIndex}${titleSuffix}`,
      description: '',
      config: {
        data_source: {
          file: {
            relative_path: component.relativePath,
          },
        },
      },
    });
  }

  return {
    renderers,
    visualizations,
  };
}
