// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const TOP_LEVEL_COLUMNS = ['start_timestamp', 'end_timestamp', 'device_no'];

/** @typedef {import('./types').BinaryExpression} BinaryExpression */
/** @typedef {import('./types').FunctionExpression} FunctionExpression */
/** @typedef {import('./types').Metric} Metric */
/** @typedef {import('./types').MetricExpression} MetricExpression */

/** @param {string} identifier */
function quoteIdentifier(identifier) {
  return `"${identifier.replaceAll('"', '""')}"`;
}

/**
 * @param {MetricExpression} expression
 * @param {Map<number, Metric>} metricsById
 * @returns {string}
 */
function renderExpression(expression, metricsById) {
  // Referenced derived metrics are available because the caller emits
  // dependency-ordered CTEs.
  switch (expression.operation) {
    case 'metric': {
      const metric = metricsById.get(expression.id);
      if (!metric) {
        throw new Error(
          `Expression references missing metric ID ${expression.id}`,
        );
      }
      return quoteIdentifier(metric.machineName);
    }
    case 'runtime-input':
      if (expression.name !== 'MALI_CONFIG_TIME_SPAN') {
        throw new Error(`Unavailable runtime input ${expression.name}`);
      }
      return '((end_timestamp - start_timestamp) / 1000000000.0)';
    case 'literal':
      if (!Number.isFinite(expression.value)) {
        throw new Error('Metric expression contains a non-finite literal');
      }
      return String(expression.value);
    case 'minimum':
      return renderFunctionExpression('LEAST', expression, metricsById);
    case 'maximum':
      return renderFunctionExpression('GREATEST', expression, metricsById);
    case 'divide': {
      const left = renderExpression(expression.left, metricsById);
      const right = renderExpression(expression.right, metricsById);
      return `(${left} / NULLIF(${right}, 0))`;
    }
    case 'add':
      return renderBinaryExpression('+', expression, metricsById);
    case 'subtract':
      return renderBinaryExpression('-', expression, metricsById);
    case 'multiply':
      return renderBinaryExpression('*', expression, metricsById);
    default:
      throw new Error(
        `Unsupported metric operation: ${/** @type {any} */ (expression).operation}`,
      );
  }
}

/**
 * @param {string} operator
 * @param {BinaryExpression} expression
 * @param {Map<number, Metric>} metricsById
 */
function renderBinaryExpression(operator, expression, metricsById) {
  const left = renderExpression(expression.left, metricsById);
  const right = renderExpression(expression.right, metricsById);
  return `(${left} ${operator} ${right})`;
}
/**
 * @param {string} functionName
 * @param {FunctionExpression} expression
 * @param {Map<number, Metric>} metricsById
 */
function renderFunctionExpression(functionName, expression, metricsById) {
  const argumentsSql = expression.arguments.map((argument) =>
    renderExpression(argument, metricsById),
  );
  return `${functionName}(${argumentsSql.join(', ')})`;
}

/** @param {import('./types').MetricPlan} plan */
function buildMetricSql(plan) {
  const { selectedMetrics, requiredMetrics, metricsById } = plan;
  const derivedMetrics = requiredMetrics.filter((metric) => metric.expression);
  const ctes = ['metric_0 AS (SELECT * FROM {{table:source}})'];

  // The plan is already dependency ordered. Each step adds one metric so
  // later expressions can refer to it without another dependency analysis.
  derivedMetrics.forEach((metric, index) => {
    ctes.push(`metric_${index + 1} AS (
  SELECT *, ${renderExpression(metric.expression, metricsById)} AS ${quoteIdentifier(metric.machineName)}
  FROM metric_${index}
)`);
  });

  const outputColumns = selectedMetrics.map((metric) =>
    quoteIdentifier(metric.machineName),
  );
  return `WITH\n  ${ctes.join(',\n  ')}\nSELECT\n  ${[
    ...TOP_LEVEL_COLUMNS,
    ...outputColumns,
  ].join(',\n  ')}\nFROM metric_${derivedMetrics.length}`;
}

module.exports = { buildMetricSql };
