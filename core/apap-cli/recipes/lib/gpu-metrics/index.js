// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const { selectGPUMetrics } = require('./select-metrics');
const { resolveMetricDependencies } = require('./resolve-dependencies');

/**
 * Resolves selected metrics to the hardware counters needed to collect them.
 * @param {string | undefined} gpuName
 * @param {string[]} selectors
 * @returns {{aliases: string[], unresolvedSelectors: string[]}}
 */
function resolveGPUHardwareCounterAliases(gpuName, selectors) {
  const { selection, unresolvedSelectors } = selectGPUMetrics(
    gpuName,
    selectors,
  );
  if (!selection) return { aliases: [], unresolvedSelectors };

  const { requiredMetrics } = resolveMetricDependencies(selection);
  const aliases = new Set(
    requiredMetrics
      .filter((metric) => metric.kind === 'hardware')
      .map((metric) => metric.machineName),
  );
  return { aliases: [...aliases], unresolvedSelectors };
}

module.exports = { resolveGPUHardwareCounterAliases };
