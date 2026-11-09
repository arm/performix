// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  resolveMetricDependencies,
  referencedMetricIds,
} = require('./resolve-dependencies');
const {
  translateGPUCounters,
} = require('../../../tool-integrations/gpu-counters');

const GPU_TIMELINE_BIN_DURATION_NS = 100_000_000;
const COUNTER_CAPABILITY_TYPE = 'tool_capabilities/counter';
const TIMESTAMP_COLUMNS = ['start_timestamp', 'end_timestamp', 'device_no'];

/** @param {Record<string, any>} capabilities @param {number} metricSetId */
function collectedCountersByAlias(capabilities, metricSetId) {
  const collectedCounters = [];
  for (const capability of Object.values(capabilities)) {
    if (
      capability?.componentType?.name !== COUNTER_CAPABILITY_TYPE ||
      capability.state !== 'collected'
    ) {
      continue;
    }

    const {
      key_type: keyType,
      series_id: seriesId,
      counter_class: counterClass,
      description,
      units,
    } = capability.payload ?? {};
    if (
      !Number.isSafeInteger(keyType) ||
      keyType < 0 ||
      !Number.isSafeInteger(seriesId) ||
      seriesId < 0 ||
      typeof description !== 'string' ||
      typeof units !== 'string'
    ) {
      throw new Error('Collected GPU counter capability is invalid');
    }
    collectedCounters.push({
      counter: `${keyType}:${seriesId}`,
      description,
      units,
      title: capability.payload.counter_title,
      name: capability.payload.counter_name,
      keyType,
      seriesId,
      counterClass,
    });
  }

  // The tool integration already matches GPU descriptions and units to APX
  // aliases at probe time. Apply that same match to the collected metadata.
  const aliasedCounters = translateGPUCounters(
    collectedCounters,
    metricSetId,
    collectedCounters,
  );
  return new Map(aliasedCounters.map((counter) => [counter.alias, counter]));
}

/** @param {string} identifier */
function quoteIdentifier(identifier) {
  return `"${identifier.replaceAll('"', '""')}"`;
}

/** @param {string[]} counterPaths */
function renderCounterSource(counterPaths) {
  if (counterPaths.length === 0) {
    throw new Error('NeoProf GPU metrics require counter data');
  }
  const paths = counterPaths.map((path) => `{{path:${path}}}`).join(', ');
  return `read_parquet([${paths}], hive_partitioning = true, union_by_name = true)`;
}

function renderHardwareColumns(hardwareMetrics, countersByAlias) {
  return hardwareMetrics.map((metric) => {
    const counter = countersByAlias.get(metric.machineName);
    if (!counter) {
      throw new Error(
        `Resolved counter data is missing for metric ${metric.id} (${metric.layoutName})`,
      );
    }

    // NeoProf stores counter series as rows. Pivot each required series into
    // the machine-name column expected by the generic metric SQL layer.
    const value =
      `MAX(CASE WHEN key_type = ${counter.keyType} ` +
      `AND series_id = ${counter.seriesId} ` +
      'THEN sample_value END)';
    // Like sysutil, restore omitted zero samples only for collected delta
    // counters. Other counter classes must preserve missing values as NULL.
    const normalizedValue =
      counter.counterClass === 0 ? `COALESCE(${value}, 0)` : value;
    return `${normalizedValue} * ${metric.scaleMultiplier ?? 1} AS ${quoteIdentifier(metric.machineName)}`;
  });
}

/** @param {import('./types').MetricExpression} expression */
function hasAvailableRuntimeInputs(expression) {
  switch (expression.operation) {
    case 'runtime-input':
      // Bin duration is available from timestamps. Device configuration is
      // not included in the collected capabilities or capture metadata.
      return expression.name === 'MALI_CONFIG_TIME_SPAN';
    case 'metric':
    case 'literal':
      return true;
    case 'minimum':
    case 'maximum':
      return expression.arguments.every(hasAvailableRuntimeInputs);
    default:
      return (
        hasAvailableRuntimeInputs(expression.left) &&
        hasAvailableRuntimeInputs(expression.right)
      );
  }
}

/**
 * Keep a selected metric only when all of its hardware dependencies were
 * collected. Missing counters are allowed during warning-only collections.
 * @param {import('./types').MetricSelection} selection
 * @param {Record<string, any>} capabilities
 */
function selectCollectedGPUMetrics(selection, capabilities) {
  const countersByAlias = collectedCountersByAlias(
    capabilities,
    selection.metricSetId,
  );
  const plan = resolveMetricDependencies(selection);
  const availableIds = new Set();
  // Dependencies precede their consumers, so availability needs one pass.
  for (const metric of plan.requiredMetrics) {
    const available =
      metric.kind === 'hardware'
        ? countersByAlias.has(metric.machineName)
        : hasAvailableRuntimeInputs(metric.expression) &&
          referencedMetricIds(metric.expression).every((id) =>
            availableIds.has(id),
          );
    if (available) availableIds.add(metric.id);
  }
  const collectedPlan = resolveMetricDependencies({
    ...selection,
    selectedMetrics: plan.selectedMetrics.filter((metric) =>
      availableIds.has(metric.id),
    ),
  });
  return { ...collectedPlan, counters: [...countersByAlias.values()] };
}

function buildNeoprofHardwareSql(plan, counterPaths, captureMetadataPath) {
  const hardwareMetrics = plan.requiredMetrics.filter(
    (metric) => metric.kind === 'hardware',
  );
  const countersByAlias = new Map(
    plan.counters.map((counter) => [counter.alias, counter]),
  );
  const requiredCounters = hardwareMetrics.map((metric) =>
    countersByAlias.get(metric.machineName),
  );
  const hardwareColumns = renderHardwareColumns(
    hardwareMetrics,
    countersByAlias,
  );
  const counterFilter = requiredCounters
    .map(
      (counter) =>
        `(key_type = ${counter.keyType} AND series_id = ${counter.seriesId})`,
    )
    .join(' OR ');
  // Read the counter files, select 100 ms bins, convert interval totals to
  // per-bin values, expand compressed intervals, then pivot counters into
  // metric columns grouped by bin timestamps and device.
  //
  // Example for one device (times in ms):
  // Input counter | Start | End | Total
  // Queued        |     0 | 200 |   200
  // Stall         |     0 | 100 |    10
  // Stall         |   100 | 200 |    20
  //
  // NeoProf compacts equal adjacent bins independently for each counter.
  // Queued before compaction: two 100 ms bins
  // Time       0 ms           100 ms          200 ms
  //            ├──────────────┼───────────────┤
  // Value      │     100      │      100      │
  //            └──────────────┴───────────────┘
  //
  // Queued after compaction: one stored row containing the interval total
  // Time       0 ms                           200 ms
  //            ├─────────────────────────────┤
  // Value      │         total = 200         │
  //            └─────────────────────────────┘
  //
  // Recover the per-bin value: 200 * 100 ms / 200 ms = 100.
  // Without normalization, expansion would repeat 200 twice, incorrectly
  // turning the original total of 200 into 400.
  //
  // After normalization, expansion and pivot:
  // Start | End | Queued | Stall
  //     0 | 100 |    100 |    10
  //   100 | 200 |    100 |    20
  //
  // Final expression: active = queued - stall
  // Start | End | Active
  //     0 | 100 |     90
  //   100 | 200 |     80
  return `WITH
  raw_counters AS (
    SELECT * FROM ${renderCounterSource(counterPaths)}
  ),
  binned_counters AS (
    SELECT
      start_timestamp,
      end_timestamp,
      device_no,
      key_type,
      series_id,
      value * bin_duration / (end_timestamp - start_timestamp) AS sample_value
    FROM raw_counters
    WHERE bin_duration = ${GPU_TIMELINE_BIN_DURATION_NS}
      AND (${counterFilter})
  ),
  capture AS (
    SELECT duration FROM read_json({{path:${captureMetadataPath}}},
      columns = {duration: 'BIGINT'})
  ),
  gpu_devices AS (
    SELECT DISTINCT device_no FROM raw_counters
    WHERE ${counterFilter}
  ),
  -- Capture bounds and the zero origin are independent of omitted samples
  -- and of the other timeline resolutions. Include a final partial bin.
  capture_grid AS (
    SELECT bin_start AS start_timestamp,
      bin_start + ${GPU_TIMELINE_BIN_DURATION_NS} AS end_timestamp,
      device_no
    FROM capture CROSS JOIN gpu_devices
    CROSS JOIN generate_series(
      0::BIGINT, duration - 1,
      ${GPU_TIMELINE_BIN_DURATION_NS}
    ) AS bins(bin_start)
  ),
  expanded_counters AS (
    SELECT
      bin_start AS start_timestamp,
      bin_start + ${GPU_TIMELINE_BIN_DURATION_NS} AS end_timestamp,
      device_no,
      key_type,
      series_id,
      sample_value
    FROM binned_counters
    CROSS JOIN generate_series(
      start_timestamp,
      end_timestamp - ${GPU_TIMELINE_BIN_DURATION_NS},
      ${GPU_TIMELINE_BIN_DURATION_NS}
    ) AS bins(bin_start)
  )
SELECT
  ${[...TIMESTAMP_COLUMNS, ...hardwareColumns].join(',\n  ')}
FROM capture_grid
LEFT JOIN expanded_counters USING (start_timestamp, end_timestamp, device_no)
GROUP BY ${TIMESTAMP_COLUMNS.join(', ')}`;
}

module.exports = { buildNeoprofHardwareSql, selectCollectedGPUMetrics };
