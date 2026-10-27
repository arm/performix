// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const metricSetIdsByProduct = require('./gpu-metrics/groups/products.json');

/**
 * @param {string} name
 * @returns {string}
 */
// Removes spaces and capitalizes the following letter: "shader core" -> "shaderCore".
function selectorName(name) {
  return name
    .trim()
    .replace(/\s+(.)/g, (_, character) => character.toUpperCase());
}

/**
 * @param {*} expression
 * @param {Map<number, *>} metricsById
 * @param {Set<string>} aliases
 */
function addExpressionAliases(expression, metricsById, aliases) {
  if (expression.operation === 'metric') {
    addMetricAliases(metricsById.get(expression.id), metricsById, aliases);
    return;
  }
  if (expression.left) {
    addExpressionAliases(expression.left, metricsById, aliases);
  }
  if (expression.right) {
    addExpressionAliases(expression.right, metricsById, aliases);
  }
  for (const argument of expression.arguments ?? []) {
    addExpressionAliases(argument, metricsById, aliases);
  }
}

/**
 * @param {*} metric
 * @param {Map<number, *>} metricsById
 * @param {Set<string>} aliases
 */
function addMetricAliases(metric, metricsById, aliases) {
  if (metric.kind === 'hardware') {
    aliases.add(metric.machineName);
  } else if (metric.kind === 'derived') {
    addExpressionAliases(metric.expression, metricsById, aliases);
  }
}

/**
 * Selects hardware metric aliases from the GPU's semantic layout. Selectors
 * use `section.group` for a whole group or `section.group.metric` for one
 * metric. Sections, groups, and metrics absent from this GPU's layout are
 * reported as unresolved selectors.
 *
 * @param {string | undefined} gpuName
 * @param {string[]} selectors
 * @returns {{aliases: string[], unresolvedSelectors: string[]}}
 */
function resolveGPUCounterAliases(gpuName, selectors) {
  const metricSetId = metricSetIdsByProduct[gpuName];
  if (metricSetId === undefined) {
    return { aliases: [], unresolvedSelectors: [...selectors] };
  }

  const metricSet = require(
    `./gpu-metrics/groups/metric-set-${metricSetId}.json`,
  );
  const metricsById = new Map(
    metricSet.sections.flatMap((section) =>
      section.groups.flatMap((group) =>
        group.metrics.map((metric) => [metric.id, metric]),
      ),
    ),
  );
  const requested = new Set(selectors);
  const resolvedSelectors = new Set();
  const aliases = new Set();
  for (const section of metricSet.sections) {
    for (const group of section.groups) {
      const groupSelector = `${selectorName(section.name)}.${selectorName(group.name)}`;
      const wholeGroupSelected = requested.has(groupSelector);
      if (wholeGroupSelected) {
        resolvedSelectors.add(groupSelector);
      }
      for (const metric of group.metrics) {
        const metricSelector = `${groupSelector}.${selectorName(metric.layoutName)}`;
        if (!wholeGroupSelected && !requested.has(metricSelector)) {
          continue;
        }
        if (metric.kind === 'hardware' || metric.kind === 'derived') {
          addMetricAliases(metric, metricsById, aliases);
          if (!wholeGroupSelected) {
            resolvedSelectors.add(metricSelector);
          }
        }
      }
    }
  }
  return {
    aliases: [...aliases],
    unresolvedSelectors: selectors.filter(
      (selector) => !resolvedSelectors.has(selector),
    ),
  };
}

/**
 * @param {string | undefined} gpuName
 * @param {string[]} selectors
 * @returns {string[]}
 */
function selectGPUCounterAliases(gpuName, selectors) {
  return resolveGPUCounterAliases(gpuName, selectors).aliases;
}

module.exports = { resolveGPUCounterAliases, selectGPUCounterAliases };
