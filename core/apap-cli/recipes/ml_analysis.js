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
const { makeGroupingSQL } = require('./lib/sql_grouping');
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

// Rules are ordered: overload-specific exceptions precede operator families.
const operatorGroups = [
  {
    label: 'Elementwise arithmetic and activations',
    patterns: ['^aten\\.(?:max|min)\\.other(?:_out)?$'],
  },
  {
    label: 'Convolution',
    patterns: [
      '^aten\\.(?:_?convolution|conv[123]d|conv_transpose[123]d)(?:_backward)?\\.',
    ],
  },
  {
    label: 'Matrix operations',
    patterns: [
      '^aten\\.(?:mm|bmm|addmm|baddbmm|addbmm|matmul|linear|mv|addmv|dot|vdot|outer)\\.',
    ],
  },
  {
    label: 'Quantization',
    patterns: [
      '^aten\\.(?:quantize_per_(?:tensor|channel)|dequantize|fake_quantize_per_(?:tensor|channel)_affine)(?:_[a-z0-9_]+)?\\.',
    ],
  },
  {
    label: 'Normalization',
    patterns: [
      '^aten\\.(?:native_layer_norm|layer_norm)(?:_backward)?\\.',
      '^aten\\.(?:native_batch_norm|batch_norm)(?:_backward)?\\.',
      '^aten\\.(?:group_norm|native_group_norm|rms_norm)(?:_backward)?\\.',
      '^aten\\.(?:_softmax|softmax|_log_softmax|log_softmax)(?:_backward)?\\.',
    ],
  },
  {
    label: 'Pooling',
    patterns: [
      '^aten\\.(?:_?adaptive_(?:avg|max)_pool[123]d|(?:avg|max)_pool[123]d(?:_with_indices)?)(?:_backward)?\\.',
    ],
  },
  {
    label: 'Attention',
    patterns: [
      '^aten\\.(?:_?scaled_dot_product_[a-z0-9_]+|_native_multi_head_attention)\\.',
    ],
  },
  {
    label: 'Embedding',
    patterns: ['^aten\\.(?:embedding|_?embedding_bag)(?:_[a-z0-9_]+)?\\.'],
  },
  {
    label: 'Reductions',
    patterns: [
      '^aten\\.(?:any|all|sum|mean|prod|amax|amin|argmax|argmin|std|var|std_mean|var_mean|logsumexp)\\.',
      '^aten\\.(?:max|min)\\.',
    ],
  },
  {
    label: 'Cumulative operations',
    patterns: ['^aten\\.(?:cumsum|cumprod|cummax|cummin|logcumsumexp)\\.'],
  },
  {
    label: 'Comparisons and logical operations',
    patterns: [
      '^aten\\.(?:lt|le|gt|ge|eq|ne|isin)_?\\.',
      '^aten\\.(?:isfinite|isinf|isnan)_?\\.',
      '^aten\\.logical_(?:and|or|xor|not)_?\\.',
      '^aten\\.bitwise_(?:and|or|xor|not|left_shift|right_shift)_?\\.',
    ],
  },
  {
    label: 'Elementwise arithmetic and activations',
    patterns: [
      '^aten\\.(?:add|sub|rsub|mul|div|true_divide|floor_divide|pow)_?\\.',
      '^aten\\.(?:maximum|minimum|fmax|fmin)_?\\.',
      '^aten\\.(?:tanh|relu|gelu|sigmoid|silu|elu|leaky_relu|hardtanh|hardswish)_?\\.',
      '^aten\\.(?:abs|neg|exp|expm1|log|log1p|sqrt|rsqrt|reciprocal|sin|cos)_?\\.',
      '^aten\\.(?:floor|ceil|round|clamp|clamp_min|clamp_max)_?\\.',
    ],
  },
  {
    label: 'Shape and indexing',
    patterns: [
      '^aten\\.(?:unsqueeze|squeeze|view|_unsafe_view|reshape|flatten)_?\\.',
      '^aten\\.(?:transpose|t|permute|expand|as_strided)_?\\.',
      '^aten\\.(?:slice|select|split|split_with_sizes|unbind)_?\\.',
      '^aten\\.(?:gather|scatter|index_select|index|index_put)_?\\.',
    ],
  },
  {
    label: 'Copy and assembly',
    patterns: [
      '^aten\\.(?:_to_copy|to|copy|clone|cat|concat|stack|masked_fill|where|contiguous)_?\\.',
    ],
  },
  {
    label: 'Tensor creation and initialization',
    patterns: [
      '^aten\\.(?:zeros|ones|empty|full)(?:_like|_strided)?\\.',
      '^aten\\.(?:arange|linspace|logspace|eye|fill_|zero_)\\.',
    ],
  },
  {
    label: 'Random sampling',
    patterns: [
      '^aten\\.(?:uniform|normal|random|bernoulli|rand|randn|randint|randperm|multinomial)(?:_like)?_?\\.',
    ],
  },
  {
    label: 'Framework and scalar operations',
    patterns: [
      '^aten\\.(?:lift_fresh|lift_fresh_copy|detach|_local_scalar_dense|alias)_?\\.',
    ],
  },
];

function operatorSummarySQL(path) {
  const groupExpression = makeGroupingSQL({
    column: 'operator_name',
    groups: operatorGroups,
    fallback: 'Other',
  });
  return `WITH summary AS (
          SELECT operator_name AS operator,
                 ${groupExpression} AS operator_group,
                 COUNT(*) AS num_calls,
                 AVG(ts_end_ns - ts_begin_ns) / 1000000 AS average_time_ms,
                 CAST(SUM(ts_end_ns - ts_begin_ns) AS DOUBLE) / 1000000 AS total_time_ms
          FROM read_parquet({{path:${path}}})
          GROUP BY operator_name
          )
          SELECT operator, operator_group, num_calls,
                 100.0 * num_calls / SUM(num_calls) OVER () AS num_calls_percent,
                 average_time_ms, total_time_ms,
                 COALESCE(100.0 * total_time_ms / NULLIF(SUM(total_time_ms) OVER (), 0), 0) AS total_time_percent
          FROM summary`;
}

function operatorMeasurementSQL({ id, column }) {
  return `SELECT id::INTEGER AS call_tree_id, parent_id::INTEGER AS call_tree_parent_id,
    node_type, id::INTEGER AS symbol_id, ${id}::BIGINT AS measurement_id,
    ${column}::DOUBLE AS measurement_value FROM {{table:source}}`;
}

function operatorComparisonMeasurementSQL({ id, column }) {
  return `SELECT id::INTEGER AS call_tree_id_2, parent_id::INTEGER AS call_tree_parent_id_2,
    node_type, id::INTEGER AS symbol_id_2, ${id}::BIGINT AS measurement_id,
    ${column}_baseline::DOUBLE AS measurement_value_1,
    ${column}::DOUBLE AS measurement_value_2,
    (${column} - ${column}_baseline)::DOUBLE AS delta_value,
    (100.0 * (${column} - ${column}_baseline) / NULLIF(${column}_baseline, 0))::DOUBLE AS delta_percentage
    FROM {{table:source}}`;
}

// Category and operator nodes share the standard call stack table contracts.
function makeOperatorCallstackRenderers(summary, comparison = false) {
  const makeSQL = (name, sql, component = name, input = summary) => ({
    type: 'SQL',
    id: `operator_${name}`,
    config: {
      sql,
      inputs: [{ name: 'source', component_type: FLAT_TABLE_COMPONENT }],
      data_source: {
        tables: { source: [{ renderer_id: input.id, output: 'table' }] },
      },
      output: {
        name: 'table',
        component_type: {
          name: component,
          schema_version:
            {
              drilldown: '0.2.4',
              drilldown_measurements: '2.1',
              symbols: '1.0.0',
              images: '1.0.0',
              source_files: '1.0.0',
            }[component] ?? '1.0',
        },
      },
    },
  });
  const baselineColumns = comparison
    ? `,
      SUM(num_calls_baseline)::BIGINT AS num_calls_baseline,
      SUM(total_time_ms_baseline) AS total_time_ms_baseline,
      SUM(num_calls_percent_baseline) AS num_calls_percent_baseline,
      SUM(total_time_percent_baseline) AS total_time_percent_baseline`
    : '';
  const baselineCategory = comparison
    ? `, num_calls_baseline, num_calls_percent_baseline,
      total_time_ms_baseline / NULLIF(num_calls_baseline, 0) AS average_time_ms_baseline,
      total_time_ms_baseline, total_time_percent_baseline`
    : '';
  const baselineOperator = comparison
    ? `, s.num_calls_baseline, s.num_calls_percent_baseline,
      s.average_time_ms_baseline, s.total_time_ms_baseline, s.total_time_percent_baseline`
    : '';
  const nodes = makeSQL(
    'nodes',
    `
    WITH categories AS (
      SELECT operator_group, ROW_NUMBER() OVER (ORDER BY operator_group) AS id,
             SUM(num_calls)::BIGINT AS num_calls, SUM(total_time_ms) AS total_time_ms,
             SUM(num_calls_percent) AS num_calls_percent,
             SUM(total_time_percent) AS total_time_percent ${baselineColumns}
      FROM {{table:source}} GROUP BY operator_group
    )
    SELECT id AS id, -1::BIGINT AS parent_id, 'category' AS node_type,
           operator_group AS label, num_calls, num_calls_percent,
           total_time_ms / NULLIF(num_calls, 0) AS average_time_ms, total_time_ms, total_time_percent ${baselineCategory}
    FROM categories
    UNION ALL
    SELECT (SELECT COUNT(*) FROM categories) + ROW_NUMBER() OVER (ORDER BY s.operator) AS id,
           c.id AS parent_id, 'operator' AS node_type, s.operator AS label,
           s.num_calls AS num_calls, s.num_calls_percent AS num_calls_percent,
           s.average_time_ms AS average_time_ms, s.total_time_ms AS total_time_ms,
           s.total_time_percent AS total_time_percent ${baselineOperator}
    FROM {{table:source}} s JOIN categories c USING (operator_group)
  `,
    'flat_table',
  );
  const measurements = [
    {
      id: 4,
      name: 'Total duration (ms)',
      units: 'ms',
      column: 'total_time_ms',
    },
    {
      id: 5,
      name: 'Total duration (% of total)',
      units: 'percent',
      column: 'total_time_percent',
    },
    {
      id: 3,
      name: 'Average duration (ms)',
      units: 'ms',
      column: 'average_time_ms',
    },
    { id: 1, name: 'Calls', units: '', column: 'num_calls' },
    {
      id: 2,
      name: 'Calls (% of total)',
      units: 'percent',
      column: 'num_calls_percent',
    },
  ];
  const renderers = [
    nodes,
    makeSQL(
      comparison ? 'deltas' : 'drilldown',
      measurements
        .map(
          comparison
            ? operatorComparisonMeasurementSQL
            : operatorMeasurementSQL,
        )
        .join(' UNION ALL '),
      comparison ? 'flat_table' : 'drilldown',
      nodes,
    ),
    makeSQL(
      'symbols',
      `SELECT id::INTEGER AS symbol_id, label AS name,
      NULL::INTEGER AS image_id, NULL::INTEGER AS source_file_id FROM {{table:source}}`,
      'symbols',
      nodes,
    ),
    makeSQL(
      'measurements',
      `SELECT * FROM (VALUES ${measurements
        .map(
          ({ id, name, units, column }) =>
            `(${id}::BIGINT, '${name}', '${units}', 'pytorch.operator.${column}')`,
        )
        .join(',')}) AS m(measurement_id, name, units, identifier)`,
      'drilldown_measurements',
    ),
    makeSQL(
      'measurement_order',
      `SELECT * FROM (VALUES ${measurements
        .map(({ id }, index) => `(${id}::BIGINT, ${index}::BIGINT)`)
        .join(',')}) AS m(measurement_id, order_index)`,
    ),
    makeSQL(
      'images',
      'SELECT NULL::INTEGER AS image_id, NULL::VARCHAR AS image_name WHERE FALSE',
    ),
    makeSQL(
      'source_files',
      `SELECT NULL::INTEGER AS source_file_id,
      NULL::VARCHAR AS target_location, NULL::VARCHAR AS host_location WHERE FALSE`,
    ),
  ];
  const tables = {
    [comparison ? 'deltas' : 'drilldown']: [
      {
        renderer_id: comparison ? 'operator_deltas' : 'operator_drilldown',
        output: 'table',
      },
    ],
    symbols: [{ renderer_id: 'operator_symbols', output: 'table' }],
    measurements: [{ renderer_id: 'operator_measurements', output: 'table' }],
    measurementOrder: [
      { renderer_id: 'operator_measurement_order', output: 'table' },
    ],
    images: [{ renderer_id: 'operator_images', output: 'table' }],
    source_files: [{ renderer_id: 'operator_source_files', output: 'table' }],
  };
  return { renderers, tables };
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
          SELECT COALESCE(baseline.num_calls, 0) AS num_calls_baseline,
                 COALESCE(baseline.num_calls_percent, 0) AS num_calls_percent_baseline,
                 baseline.average_time_ms AS average_time_ms_baseline,
                 COALESCE(baseline.total_time_ms, 0) AS total_time_ms_baseline,
                 COALESCE(baseline.total_time_percent, 0) AS total_time_percent_baseline,
                 COALESCE(baseline.operator, current.operator) AS operator,
                 COALESCE(baseline.operator_group, current.operator_group) AS operator_group,
                 COALESCE(current.num_calls, 0) AS num_calls,
                 COALESCE(current.num_calls_percent, 0) AS num_calls_percent,
                 current.average_time_ms AS average_time_ms,
                 COALESCE(current.total_time_ms, 0) AS total_time_ms,
                 COALESCE(current.total_time_percent, 0) AS total_time_percent
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
  const viewport = {
    type: 'TraceViewportRenderer',
    id: 'pytorch_trace',
    config: {
      data_source: {
        tables: {
          tracks: [{ renderer_id: traceData.id, output: 'tracks' }],
          spans: [{ renderer_id: traceData.id, output: 'spans' }],
        },
      },
    },
  };
  return { apiCalls, operatorCalls, traceData, viewport };
}

function makeTraceVisualization(traceData, viewport, durationNs) {
  return {
    type: 'timeline',
    id: 'pytorch_trace',
    rendererId: viewport.id,
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
          metadata: [{ renderer_id: traceData.id, output: 'metadata' }],
        },
      },
      groups: {
        pytorch: {
          index: 0,
          type: 'trace',
          title: 'PyTorch',
          config: {
            datasetId: viewport.id,
            trackSourceKey: 'tracks',
            spanSourceKey: 'spans',
            metadataSourceKey: 'metadata',
            tooltip: 'pytorch',
          },
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
  const callstack = makeOperatorCallstackRenderers(opSummary, isComparison);
  renderers.push(...callstack.renderers);
  const visualizations = [
    {
      type: isComparison ? 'call_stack_comparison' : 'call_stack',
      id: 'pytorch_operator_summary',
      rendererId: opSummary.id,
      title: isComparison ? 'Operator Summary Comparison' : 'Operator Summary',
      description:
        'Operator calls grouped by category. Category averages are weighted by call count. Percentages use all calls and summed operator durations in each run, before filtering; duration share is not wall-clock time. Comparisons include operators observed in either run.',
      config: {
        presentation: 'operators',
        grouping: true,
        noDataMessage: 'No PyTorch operator calls were captured.',
        data_source: { tables: callstack.tables },
      },
    },
  ];

  if (!isComparison) {
    const durationNs = getPyTorchCollectionDurationNs(context);
    const trace = makeTraceRenderers();
    renderers.push(
      trace.apiCalls,
      trace.operatorCalls,
      trace.traceData,
      trace.viewport,
    );
    visualizations.push(
      makeTraceVisualization(trace.traceData, trace.viewport, durationNs),
    );
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
