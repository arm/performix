// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

/**
 * @typedef {{title: string, name: string, counter: string}} GPUCounter
 */

/**
 * @param {string} output
 * @param {string} gpuName
 * @returns {GPUCounter[]}
 */
function parseGPUCounters(output, gpuName) {
  // Immortalis public names can differ from the Mali counter product name.
  const counterPrefix = gpuName.startsWith('Immortalis-')
    ? 'ARM_Mali-'
    : `ARM_${gpuName}_`;
  const eventPattern = /^\s*\*\s+(\S+)\s+-\s+(.+?):\s+(.+?)\s*$/gm;
  const counters = [];

  for (const match of output.matchAll(eventPattern)) {
    const [, counter, title, name] = match;
    if (counter.startsWith(counterPrefix)) {
      counters.push({
        title,
        name,
        counter,
      });
    }
  }

  return counters;
}

module.exports = { parseGPUCounters };
