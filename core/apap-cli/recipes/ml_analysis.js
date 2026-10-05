// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const TOOL_PYTORCH_COLLECT = {
  name: 'pytorch-collect',
  version: '1.0.0',
};
const FILE_PATHS = {
  api_calls: 'tool/pytorch-collect/0/api_calls.parquet',
  op_calls: 'tool/pytorch-collect/0/operator_calls.parquet',
};

const FLAT_TABLE_COMPONENT = { name: 'flat_table', schema_version: '1.0' };
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

function buildTools(workload, python) {
  return {
    toolConfigs: [
      {
        name: TOOL_PYTORCH_COLLECT.name,
        params: { python },
        workload,
        env: {},
      },
    ],
  };
}

function readyPyTorchAnalysis(context) {
  const tools = buildTools(
    context.getWorkload(),
    context.getParameter('python'),
  );
  const responses = context.probeTools(tools);
  const advice = collectToolAdvice(tools, responses);
  return {
    status: toolStatusToRecipeStatus(advice),
    advice,
  };
}

function runPyTorchAnalysis(context) {
  context.runTools(
    buildTools(context.getWorkload(), context.getParameter('python')),
  );
}

function operatorSummarySQL(path) {
  return `SELECT operator_name AS operator,
                 COUNT(*) AS num_calls,
                 AVG(ts_end_ns - ts_begin_ns) / 1000000 AS average_time_ms,
                 SUM(ts_end_ns - ts_begin_ns) / 1000000 AS total_time_ms
          FROM read_parquet({{path:${path}}})
          GROUP BY operator_name`;
}

function operatorComparisonSQL() {
  const baseline = operatorSummarySQL(`0:${FILE_PATHS.op_calls}`);
  const current = operatorSummarySQL(`1:${FILE_PATHS.op_calls}`);

  return `WITH baseline AS (
            ${baseline}
          ),
          current AS (
            ${current}
          )
          SELECT COALESCE(baseline.operator, current.operator) AS operator,
                 COALESCE(current.num_calls, 0) AS num_calls,
                 COALESCE(current.num_calls, 0) - COALESCE(baseline.num_calls, 0) AS num_calls_delta,
                 current.average_time_ms AS average_time_ms,
                 current.average_time_ms - baseline.average_time_ms AS average_time_ms_delta,
                 COALESCE(current.total_time_ms, 0) AS total_time_ms,
                 COALESCE(current.total_time_ms, 0) - COALESCE(baseline.total_time_ms, 0) AS total_time_ms_delta
          FROM baseline
          FULL OUTER JOIN current ON baseline.operator = current.operator
          ORDER BY current.total_time_ms DESC NULLS LAST,
                   baseline.total_time_ms DESC NULLS LAST,
                   operator`;
}

function renderPyTorchAnalysis(context) {
  const isComparison = context.getRunDescriptions().length === 2;
  const opSummary = {
    type: 'SQL',
    id: 'operator_summary',
    config: {
      sql: isComparison
        ? operatorComparisonSQL()
        : operatorSummarySQL(FILE_PATHS.op_calls),
      output: {
        name: 'table',
        component_type: FLAT_TABLE_COMPONENT,
      },
    },
  };

  return {
    renderers: [opSummary],
    visualizations: [
      {
        type: 'generic_grid',
        id: 'pytorch_operator_summary',
        rendererId: opSummary.id,
        title: isComparison
          ? 'Operator Summary Comparison'
          : 'Operator Summary',
        description: 'Summary of observerd PyTorch operator calls.',
        config: {
          autoSizeColumns: true,
          data_source: {
            tables: {
              table: [{ renderer_id: opSummary.id, output: 'table' }],
            },
          },
        },
      },
    ],
  };
}

const recipe = {
  name: 'ml_analysis',
  title: 'PyTorch Analysis (ML recipe phase 1)',
  version: '1.0.0',
  api_version: '1.0.0',
  status: 'experimental',
  description:
    'Runs a Python module under PyTorch tracing and reports operator calls.',
  mcp_guidance:
    'Use a non-shell launch workload whose first command argument is the path to a Python module file on the target. The selected Python environment must provide torch; set `python` to use an interpreter from a specific Python environment instead of the target default.',
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_PYTORCH_COLLECT.name,
          version: TOOL_PYTORCH_COLLECT.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'python',
      required: false,
      label: 'Python interpreter',
      description:
        'Path to an interpreter in a Python environment on the target that contains PyTorch. Relative paths are resolved from the workload working directory.',
      config: {
        type: 'input',
        defaultValue: '.venv/bin/python',
      },
    },
  ],
  readyStages: [
    {
      name: 'Check PyTorch Analysis readiness',
      description:
        'Check the Python environment, collector deployment, and workload module.',
      exec: readyPyTorchAnalysis,
    },
  ],
  runStages: [
    {
      name: 'Collect PyTorch call data',
      description:
        'Run the workload under PyTorch API and operator call tracing.',
      exec: runPyTorchAnalysis,
    },
  ],
  renderStages: [
    {
      name: 'Render PyTorch call data',
      description: 'Create API operator call summary.',
      exec: renderPyTorchAnalysis,
    },
  ],
};
