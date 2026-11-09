// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const metricSetIdsByProduct = require('./groups/products.json');

/** @typedef {import('./types').Metric} Metric */

/** @param {string} name */
// Removes spaces and capitalizes the following letter: "shader core" -> "shaderCore".
function selectorName(name) {
  return name
    .trim()
    .replace(/\s+(.)/g, (_, character) => character.toUpperCase());
}

/** @type {Set<string> | undefined} */
let knownSectionSelectors;

function getKnownSectionSelectors() {
  if (!knownSectionSelectors) {
    knownSectionSelectors = new Set();
    for (const id of new Set(Object.values(metricSetIdsByProduct))) {
      /** @type {import('./types').MetricSet} */
      const layout = require(`./groups/metric-set-${id}.json`);
      for (const section of layout.sections) {
        knownSectionSelectors.add(`${selectorName(section.name)}.*`);
      }
    }
  }
  return knownSectionSelectors;
}

/**
 * Selects all performance metrics (*), sections (Section.*), groups, or metrics.
 * Known section wildcards select where supported across GPU generations.
 * Explicit groups, metrics, and unknown selectors must resolve on this GPU.
 * @param {string | undefined} gpuName
 * @param {string[]} selectors
 */
function selectGPUMetrics(gpuName, selectors) {
  const metricSetId = metricSetIdsByProduct[gpuName];
  if (metricSetId === undefined) {
    return { selection: null, unresolvedSelectors: [...selectors] };
  }

  /** @type {import('./types').MetricSet} */
  const metricSet = require(`./groups/metric-set-${metricSetId}.json`);
  /** @type {Map<number, Metric>} */
  const metricsById = new Map();
  for (const section of metricSet.sections) {
    for (const group of section.groups) {
      for (const metric of group.metrics) metricsById.set(metric.id, metric);
    }
  }

  const requested = new Set(selectors);
  const resolvedSelectors = new Set();
  const allMetricsSelected = requested.has('*');
  if (allMetricsSelected) resolvedSelectors.add('*');
  /** @type {Metric[]} */
  const selectedMetrics = [];
  /** @type {import("./types").SelectedMetricGroup[]} */
  const selectedGroups = [];
  for (const section of metricSet.sections) {
    const sectionSelector = `${selectorName(section.name)}.*`;
    const wholeSectionSelected =
      (allMetricsSelected && section.name !== 'Constants') ||
      requested.has(sectionSelector);
    if (wholeSectionSelected) resolvedSelectors.add(sectionSelector);
    for (const group of section.groups) {
      const groupSelector = `${selectorName(section.name)}.${selectorName(group.name)}`;
      const wholeGroupSelected =
        wholeSectionSelected || requested.has(groupSelector);
      if (requested.has(groupSelector)) resolvedSelectors.add(groupSelector);

      const groupMetrics = [];
      for (const metric of group.metrics) {
        const metricSelector = `${groupSelector}.${selectorName(metric.layoutName)}`;
        if (!wholeGroupSelected && !requested.has(metricSelector)) continue;
        if (metric.kind !== 'hardware' && metric.kind !== 'derived') continue;

        selectedMetrics.push(metric);
        groupMetrics.push(metric);
        if (requested.has(metricSelector))
          resolvedSelectors.add(metricSelector);
      }
      if (groupMetrics.length > 0) {
        selectedGroups.push({
          id: groupSelector,
          name: `${section.name}: ${group.name}`,
          description: group.longDescription ?? '',
          metrics: groupMetrics,
        });
      }
    }
  }

  return {
    selection: { metricSetId, selectedMetrics, selectedGroups, metricsById },
    unresolvedSelectors: selectors.filter(
      (selector) =>
        !resolvedSelectors.has(selector) &&
        !(selector.endsWith('.*') && getKnownSectionSelectors().has(selector)),
    ),
  };
}

module.exports = { selectGPUMetrics };
