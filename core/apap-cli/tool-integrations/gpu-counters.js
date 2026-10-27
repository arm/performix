// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

// Temporary workaround: correlate events.xml descriptions and units with APX
// metrics to attach canonical aliases to active GPU counters. Remove the
// events.xml query, XML parsing, and APX matching once NEOPROF-1007 is merged:
// https://jira.arm.com/browse/NEOPROF-1007
const metricSetIdsByProduct = require('../recipes/lib/gpu-metrics/groups/products.json');
const GPU_COUNTER_PREFIX = 'ARM_Mali-';

/**
 * @typedef {{title: string, name: string, counter: string, alias?: string}} GPUCounter
 * @typedef {{counter: string, description: string, units: string}} GPUEvent
 */

/**
 * @param {string} output
 * @returns {GPUCounter[]}
 */
function parseGPUCounters(output) {
  const eventPattern = /^\s*\*\s+(\S+)\s+-\s+(.+?):\s+(.+?)\s*$/gm;
  const counters = [];

  for (const match of output.matchAll(eventPattern)) {
    const [, counter, title, name] = match;
    if (counter.startsWith(GPU_COUNTER_PREFIX)) {
      counters.push({
        title,
        name,
        counter,
      });
    }
  }

  return counters;
}

/**
 * Decodes XML attribute entities.
 *
 * @param {string} value
 * @returns {string}
 */
function decodeXMLAttribute(value) {
  return value
    .replaceAll('&quot;', '"')
    .replaceAll('&apos;', "'")
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&amp;', '&');
}

/**
 * Extracts GPU counter metadata from events.xml.
 *
 * @param {string} output
 * @returns {GPUEvent[]}
 */
function parseGPUEventsXML(output) {
  const counters = [];

  let eventStart = output.indexOf('<event ');
  while (eventStart !== -1) {
    const eventEnd = output.indexOf('>', eventStart);
    if (eventEnd === -1) break;
    const event = output.slice(eventStart, eventEnd);
    const counter = xmlAttribute(event, 'counter');
    const description = xmlAttribute(event, 'description');
    const units = xmlAttribute(event, 'units');
    if (counter?.startsWith(GPU_COUNTER_PREFIX) && description && units) {
      counters.push({ counter, description, units });
    }
    eventStart = output.indexOf('<event ', eventEnd + 1);
  }

  return counters;
}

/**
 * @param {string} element
 * @param {string} name
 * @returns {string | undefined}
 */
function xmlAttribute(element, name) {
  const marker = `${name}="`;
  const start = element.indexOf(marker);
  if (start === -1) return undefined;
  const valueStart = start + marker.length;
  const end = element.indexOf('"', valueStart);
  return end === -1
    ? undefined
    : decodeXMLAttribute(element.slice(valueStart, end));
}

/**
 * @param {string} gpuName
 * @returns {number | undefined}
 */
function matchGPUToMetricSet(gpuName) {
  return metricSetIdsByProduct[gpuName];
}

/**
 * Normalizes metric descriptions for matching: "GPU active.  " -> "gpu active".
 *
 * @param {string} description
 * @returns {string}
 */
function normalizeMetricDescription(description) {
  return description
    .trim()
    .replace(/\.+$/, '')
    .replace(/\s+/g, ' ')
    .toLowerCase();
}

/**
 * Builds a description-and-unit matching key.
 *
 * @param {string} description
 * @param {string} unit
 * @returns {string}
 */
function metricDescriptionKey(description, unit) {
  return `${normalizeMetricDescription(description)}\0${unit.trim().toLowerCase()}`;
}

/**
 * Returns active counters with matching APX canonical aliases attached.
 *
 * @param {GPUEvent[]} counters
 * @param {number | undefined} metricSetId
 * @param {GPUCounter[]} activeCounters
 * @returns {(GPUCounter & {alias: string})[]}
 */
function translateGPUCounters(counters, metricSetId, activeCounters) {
  if (metricSetId === undefined) {
    return [];
  }

  const activeNativeCounters = new Set(
    activeCounters.map((counter) => counter.counter),
  );
  const metricSet = require(
    `../recipes/lib/gpu-metrics/groups/metric-set-${metricSetId}.json`,
  );
  const metrics = metricSet.sections.flatMap((section) =>
    section.groups.flatMap((group) => group.metrics),
  );
  /** @type {Map<string, string>} */
  const metricsByDescription = new Map();
  for (const metric of metrics) {
    if (
      metric.kind !== 'hardware' ||
      typeof metric.shortDescription !== 'string' ||
      typeof metric.unit !== 'string'
    ) {
      continue;
    }

    const key = metricDescriptionKey(metric.shortDescription, metric.unit);
    if (metricsByDescription.has(key)) {
      throw new Error(
        `Multiple APX metrics have description ${JSON.stringify(metric.shortDescription)} and unit ${JSON.stringify(metric.unit)}`,
      );
    }
    metricsByDescription.set(key, metric.machineName);
  }

  /** @type {Map<string, string>} */
  const translatedCounters = new Map();
  for (const counter of counters) {
    if (!activeNativeCounters.has(counter.counter)) {
      continue;
    }

    const machineName = metricsByDescription.get(
      metricDescriptionKey(counter.description, counter.units),
    );
    if (machineName !== undefined) {
      translatedCounters.set(machineName, counter.counter);
    }
  }

  const aliasesByNativeCounter = new Map(
    [...translatedCounters].map(([alias, counter]) => [counter, alias]),
  );
  const aliasedCounters = [];
  for (const counter of activeCounters) {
    const alias = aliasesByNativeCounter.get(counter.counter);
    if (alias !== undefined) {
      aliasedCounters.push({ ...counter, alias });
    }
  }
  return aliasedCounters;
}

module.exports = {
  parseGPUCounters,
  parseGPUEventsXML,
  matchGPUToMetricSet,
  translateGPUCounters,
};
