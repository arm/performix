// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

/** @typedef {import('./types').Metric} Metric */
/** @typedef {import('./types').MetricExpression} MetricExpression */
/** @typedef {import('./types').MetricPlan} MetricPlan */
/** @typedef {import('./types').MetricSelection} MetricSelection */

/** @param {MetricExpression} expression @returns {number[]} */
function referencedMetricIds(expression) {
  if (expression.operation === 'metric') return [expression.id];
  if (
    expression.operation === 'runtime-input' ||
    expression.operation === 'literal'
  ) {
    return [];
  }
  if (
    expression.operation === 'minimum' ||
    expression.operation === 'maximum'
  ) {
    return expression.arguments.flatMap(referencedMetricIds);
  }
  if (!expression.left || !expression.right) {
    throw new Error(`Invalid ${expression.operation} metric expression`);
  }
  return [
    ...referencedMetricIds(expression.left),
    ...referencedMetricIds(expression.right),
  ];
}

/**
 * @param {Metric[]} selectedMetrics
 * @param {Map<number, Metric>} metricsById
 */
function collectRequiredMetrics(selectedMetrics, metricsById) {
  const requiredMetrics = new Map();
  const visiting = new Set();

  // A depth-first walk produces dependency-first insertion order. `visiting`
  // represents the active recursion path and therefore detects AST cycles.
  /** @param {Metric} metric */
  function visit(metric) {
    if (requiredMetrics.has(metric.id)) return;
    if (visiting.has(metric.id)) {
      throw new Error(
        `Circular metric dependency involving metric ID ${metric.id}`,
      );
    }

    visiting.add(metric.id);
    if (metric.expression) {
      for (const dependencyId of referencedMetricIds(metric.expression)) {
        const dependency = metricsById.get(dependencyId);
        if (!dependency) {
          throw new Error(
            `Metric ${metric.id} references missing metric ID ${dependencyId}`,
          );
        }
        visit(dependency);
      }
    }
    visiting.delete(metric.id);
    requiredMetrics.set(metric.id, metric);
  }

  selectedMetrics.forEach(visit);
  return requiredMetrics;
}

/** @param {MetricSelection} selection @returns {MetricPlan} */
function resolveMetricDependencies(selection) {
  // A group and one of its metrics may both be requested.
  const selectedMetrics = [
    ...new Map(
      selection.selectedMetrics.map((metric) => [metric.id, metric]),
    ).values(),
  ];
  return {
    ...selection,
    selectedMetrics,
    requiredMetrics: [
      ...collectRequiredMetrics(
        selectedMetrics,
        selection.metricsById,
      ).values(),
    ],
  };
}

module.exports = { resolveMetricDependencies, referencedMetricIds };
