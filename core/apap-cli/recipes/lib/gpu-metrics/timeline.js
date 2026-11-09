// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const { selectGPUMetrics } = require('./select-metrics');
const { buildMetricSql } = require('./create-sql');
const {
  buildNeoprofHardwareSql,
  selectCollectedGPUMetrics,
} = require('./neoprof');
const TOOL_NEOPROF = { name: 'neoprof' };
const GPU_CAPTURE_METADATA_COMPONENT =
  'tool/neoprof/0/output/parquet/metadata/capture_metadata.json';
const GPU_TIMELINE_COUNTER_COMPONENT =
  'tool/neoprof/0/output/parquet/timeline/**/counter.parquet';

/**
 * @typedef {Object} GpuTimelineParts
 * @property {Record<string, {renderer_id: string, output: string}[]>} tables
 * @property {Record<string, {title: string, description: string, type: string, index: number, config: object}>} groups
 */

/**
 * @param {import("../../docs/jsdocs").RenderExecutionContext} context
 * @param {string[]} requestedMetrics
 * @param {object} [options]
 * @param {boolean} [options.optional=false] Skip unavailable GPU support or metrics, but not invalid collected data.
 * @param {Record<string, string>} [options.groupTitles]
 * @param {'s' | 'ns'} [options.timeUnit='s']
 * @returns {GpuTimelineParts & {
 *   renderers: {type: string, id: string, config: object}[],
 *   gpuName: string | null,
 *   metricGroups: import('./types').SelectedMetricGroup[]
 * }}
 */
function buildGpuRenderParts(
  context,
  requestedMetrics,
  { optional = false, groupTitles = {}, timeUnit = 's' } = {},
) {
  const emptyParts = {
    renderers: [],
    tables: {},
    groups: {},
    gpuName: null,
    metricGroups: [],
  };
  const capabilities = context
    .getToolCapabilities(0, {
      toolName: TOOL_NEOPROF.name,
      invocationIndex: 0,
    })
    .list();
  const gpuName = capabilities.platform?.payload?.gpu_name;
  if (!gpuName) {
    if (optional) return emptyParts;
    throw { code: 'recipes.gpu.GPU_NOT_IDENTIFIED' };
  }

  const { selection, unresolvedSelectors } = selectGPUMetrics(
    gpuName,
    requestedMetrics,
  );
  if (!selection || (!optional && unresolvedSelectors.length > 0)) {
    if (optional) return emptyParts;
    throw {
      code: 'recipes.gpu.METRICS_UNAVAILABLE',
      metadata: { gpu: gpuName, selectors: unresolvedSelectors.join(', ') },
    };
  }
  const metricPlan = selectCollectedGPUMetrics(selection, capabilities);
  if (metricPlan.selectedMetrics.length === 0) {
    return emptyParts;
  }
  const collectedMetricIds = new Set(
    metricPlan.selectedMetrics.map((metric) => metric.id),
  );
  const collectedGroups = selection.selectedGroups
    .map((group) => ({
      ...group,
      metrics: group.metrics.filter((metric) =>
        collectedMetricIds.has(metric.id),
      ),
    }))
    .filter((group) => group.metrics.length > 0);
  const counterPaths = context
    .listRunComponents(0, GPU_TIMELINE_COUNTER_COMPONENT)
    .map((component) => component.relativePath);
  const captureMetadata = context.listRunComponents(
    0,
    GPU_CAPTURE_METADATA_COMPONENT,
  );
  if (counterPaths.length === 0 || captureMetadata.length !== 1) {
    throw {
      code: 'recipes.gpu.TIMELINE_DATA_MISSING',
      metadata: { gpu: gpuName },
    };
  }

  const hardwareMetricsComponent = {
    name: 'gpu-hardware-metrics',
    schema_version: '1.0',
  };

  return {
    gpuName,
    metricGroups: collectedGroups,
    renderers: [
      {
        type: 'SQL',
        id: 'neoprof_gpu_hardware_metrics',
        config: {
          sql: buildNeoprofHardwareSql(
            metricPlan,
            counterPaths,
            captureMetadata[0].relativePath,
          ),
          output: {
            name: 'hardware_metrics',
            description: 'Normalized GPU hardware metrics',
            cardinality: 'one',
            component_type: hardwareMetricsComponent,
          },
        },
      },
      {
        type: 'SQL',
        id: 'gpu_metrics',
        config: {
          sql: buildMetricSql(metricPlan),
          inputs: [
            {
              name: 'source',
              description: 'Normalized GPU hardware metrics',
              cardinality: 'one',
              component_type: hardwareMetricsComponent,
            },
          ],
          data_source: {
            tables: {
              source: [
                {
                  renderer_id: 'neoprof_gpu_hardware_metrics',
                  output: 'hardware_metrics',
                },
              ],
            },
          },
          output: {
            name: 'metrics',
            description: 'Selected GPU metrics',
            cardinality: 'one',
            component_type: { name: 'flat_table', schema_version: '1.0' },
          },
        },
      },
    ],
    ...buildGpuTimelineParts(collectedGroups, gpuName, groupTitles, {
      timeUnit,
    }),
  };
}

/**
 * @param {import('./types').SelectedMetricGroup[]} metricGroups
 * @param {string} gpuName
 * @param {Record<string, string>} [groupTitles]
 * @param {object} [options]
 * @param {'s' | 'ns'} [options.timeUnit='s']
 * @returns {GpuTimelineParts}
 */
function buildGpuTimelineParts(
  metricGroups,
  gpuName,
  groupTitles = {},
  { timeUnit = 's' } = {},
) {
  const timeColumn = timeUnit === 'ns' ? 'time_ns' : 'time_s';
  const timestamp =
    timeUnit === 'ns' ? 'start_timestamp' : 'start_timestamp / 1000000000.0';
  /** @type {GpuTimelineParts['groups']} */
  const groups = {};
  /** @type {GpuTimelineParts['tables']} */
  const tables = {};
  for (const [index, group] of metricGroups.entries()) {
    const groupId = `gpu_group_${group.metrics[0].id}`;
    const units = [...new Set(group.metrics.map((metric) => metric.unit))];
    const unit = units.length === 1 ? units[0] : undefined;
    const points = group.metrics.map((metric) => {
      const column = `"${metric.machineName.replaceAll('"', '""')}"`;
      return `SELECT ${timestamp} AS ${timeColumn},
              concat('metric_${metric.id}_device_', device_no) AS series,
              ${column} AS value
            FROM {table}`;
    });
    tables[groupId] = [{ renderer_id: 'gpu_metrics', output: 'metrics' }];
    groups[groupId] = {
      title: groupTitles[group.id] ?? group.name,
      description: group.description,
      type: 'line',
      index,
      config: {
        xAxisTitle: 'Time (s)',
        yAxisTitle: unit ?? group.name,
        ...(unit === undefined ? {} : { yAxisUnit: unit }),
        customQuery: {
          tableNamePlaceholder: '{table}',
          // Metrics already contain one row per bin; preserve each metric/device line.
          query: `WITH points AS (${points.join(' UNION ALL ')})
          SELECT * FROM (
            PIVOT points ON series USING MAX(value) GROUP BY ${timeColumn}
          ) ORDER BY ${timeColumn}`,
        },
        series: group.metrics.map((metric) => ({
          type: 'pattern',
          xColumn: timeColumn,
          yColumn: { pattern: `^metric_${metric.id}_device_(\\d+)$` },
          name: {
            template: `${gpuName} - ${metric.humanName ?? metric.layoutName} (device {y1})`,
          },
        })),
      },
    };
  }
  return { tables, groups };
}

module.exports = { buildGpuRenderParts, buildGpuTimelineParts };
