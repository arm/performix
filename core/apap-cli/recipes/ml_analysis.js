// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const TOOL_PYTORCH_COLLECT = {
  name: 'pytorch-collect',
  version: '1.0.0',
};
const FILE_PATHS = {
  api_calls: 'tool/pytorch-collect/0/api_calls.parquet',
  op_calls: 'tool/pytorch-collect/0/operator_calls.parquet',
  collection_timing: 'tool/pytorch-collect/0/collection_timing.json',
};

const FLAT_TABLE_COMPONENT = { name: 'flat_table', schema_version: '1.0' };
const { makePytorchTraceSQLRenderer } = require('./lib/pytorch_trace');
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
  return `WITH summary AS (
          SELECT operator_name AS operator,
                 COUNT(*) AS num_calls,
                 AVG(ts_end_ns - ts_begin_ns) / 1000000 AS average_time_ms,
                 CAST(SUM(ts_end_ns - ts_begin_ns) AS DOUBLE) / 1000000 AS total_time_ms
          FROM read_parquet({{path:${path}}})
          GROUP BY operator_name
          )
          SELECT operator, num_calls,
                 100.0 * num_calls / SUM(num_calls) OVER () AS num_calls_percent,
                 average_time_ms, total_time_ms,
                 COALESCE(100.0 * total_time_ms / NULLIF(SUM(total_time_ms) OVER (), 0), 0) AS total_time_percent
          FROM summary`;
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
                 COALESCE(current.num_calls_percent, 0) AS num_calls_percent,
                 COALESCE(current.num_calls, 0) - COALESCE(baseline.num_calls, 0) AS num_calls_delta,
                 current.average_time_ms AS average_time_ms,
                 current.average_time_ms - baseline.average_time_ms AS average_time_ms_delta,
                 COALESCE(current.total_time_ms, 0) AS total_time_ms,
                 COALESCE(current.total_time_percent, 0) AS total_time_percent,
                 COALESCE(current.total_time_ms, 0) - COALESCE(baseline.total_time_ms, 0) AS total_time_ms_delta
          FROM baseline
          FULL OUTER JOIN current ON baseline.operator = current.operator
          ORDER BY current.total_time_ms DESC NULLS LAST,
                   baseline.total_time_ms DESC NULLS LAST,
                   operator`;
}

function getPyTorchCollectionDurationNs(context) {
  const timingComponents = context.listRunComponents(
    0,
    FILE_PATHS.collection_timing,
  );
  if (timingComponents.length !== 1) {
    throw new Error('Expected one PyTorch collection timing file');
  }
  const timing = JSON.parse(
    context.readRunComponent(0, timingComponents[0].relativePath),
  );
  const durationNs = BigInt(timing.end_time_ns) - BigInt(timing.start_time_ns);
  if (durationNs < BigInt(0) || durationNs > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error('Invalid PyTorch collection duration');
  }
  return Number(durationNs);
}

function makeTraceRenderers() {
  const timeOrigin = {
    sql: `
      SELECT CAST(start_time_ns AS BIGINT) AS zero_time_ns
      FROM read_json_auto({{path:${FILE_PATHS.collection_timing}}})
    `,
  };
  const apiCalls = makePytorchTraceSQLRenderer({
    id: 'pytorch_api_call_spans',
    path: FILE_PATHS.api_calls,
    timeOrigin,
    eventID: 'CAST(api_call_id AS VARCHAR)',
    eventName: 'function_name',
    parent: 'NULL::VARCHAR',
  });
  const operatorCalls = makePytorchTraceSQLRenderer({
    id: 'pytorch_operator_call_spans',
    path: FILE_PATHS.op_calls,
    timeOrigin,
    eventID: 'CAST(operator_call_id AS VARCHAR)',
    eventName: 'operator_name',
    parent: 'CAST(api_call_id AS VARCHAR)',
  });
  const traceData = {
    type: 'TraceDataRenderer',
    id: 'pytorch_trace_data',
    config: {
      tracks: [
        { key: 'pytorch', name: 'PyTorch', kind: 'group' },
        {
          key: 'api_calls',
          parent: 'pytorch',
          name: 'API calls',
          kind: 'span',
        },
        {
          key: 'operator_calls',
          parent: 'pytorch',
          name: 'Operator calls',
          kind: 'span',
        },
      ],
      spans: [
        { input: 'api_spans', track: 'api_calls' },
        {
          input: 'operator_spans',
          track: 'operator_calls',
          parent_source: 'api_spans',
        },
      ],
      data_source: {
        tables: {
          api_spans: [{ renderer_id: apiCalls.id, output: 'spans' }],
          operator_spans: [{ renderer_id: operatorCalls.id, output: 'spans' }],
        },
      },
    },
  };
  return { apiCalls, operatorCalls, traceData };
}

function makeTraceVisualization(traceData, durationNs) {
  return {
    type: 'timeline',
    id: 'pytorch_trace',
    rendererId: traceData.id,
    title: 'PyTorch Trace',
    description: 'PyTorch API and operator calls over time.',
    config: {
      timeDomain: { start: 0, end: durationNs, unit: 'ns' },
      xAxisUnit: 's',
      xAxisDisplayScale: 1e-9,
      data_source: {
        tables: {
          tracks: [{ renderer_id: traceData.id, output: 'tracks' }],
          spans: [{ renderer_id: traceData.id, output: 'spans' }],
        },
      },
      groups: {
        pytorch: {
          index: 0,
          type: 'trace',
          title: 'PyTorch',
          config: { trackSourceKey: 'tracks', spanSourceKey: 'spans' },
        },
      },
    },
  };
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

  const renderers = [opSummary];
  const visualizations = [
    {
      type: 'generic_grid',
      id: 'pytorch_operator_summary',
      rendererId: opSummary.id,
      title: isComparison ? 'Operator Summary Comparison' : 'Operator Summary',
      description:
        'Summary of observed PyTorch operator calls. Percentages use all calls and summed operator durations in the run, before table filtering. Duration share is not a percentage of wall-clock time. Comparisons show percentages for the current run.',
      config: {
        percentageColumns: ['num_calls_percent', 'total_time_percent'],
        columnLabels: {
          operator: 'Operator',
          num_calls: 'Calls',
          num_calls_percent: 'Calls (% of total)',
          num_calls_delta: 'Calls change',
          average_time_ms: 'Average duration (ms)',
          average_time_ms_delta: 'Average duration change (ms)',
          total_time_ms: 'Total duration (ms)',
          total_time_percent: 'Total duration (% of total)',
          total_time_ms_delta: 'Total duration change (ms)',
        },
        data_source: {
          tables: {
            table: [{ renderer_id: opSummary.id, output: 'table' }],
          },
        },
      },
    },
  ];

  if (!isComparison) {
    const durationNs = getPyTorchCollectionDurationNs(context);
    const trace = makeTraceRenderers();
    renderers.push(trace.apiCalls, trace.operatorCalls, trace.traceData);
    visualizations.push(makeTraceVisualization(trace.traceData, durationNs));
  }

  return { renderers, visualizations };
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
        'Path to an interpreter in a Python environment on the target that contains PyTorch. Relative paths are resolved from the workload working directory. Leave empty to use the system default Python interpreter (python3 from PATH).',
      config: {
        type: 'input',
        defaultValue: '',
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
