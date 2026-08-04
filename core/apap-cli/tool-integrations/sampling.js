// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const insufficientSamplesMessageCode =
  'tool_integrations.common.INSUFFICIENT_SAMPLES';

/** @typedef {{code: string, metadata: Record<string, string | number>}} CollectorError */

/**
 * @param {number} interval
 * @param {number} minimumSamples
 * @returns {number}
 */
function getMinimumSampleDuration(interval, minimumSamples) {
  return interval * minimumSamples;
}

/**
 * @param {{
 *   tool: string,
 *   interval: number,
 *   duration: number,
 *   minimumSamples: number
 * }} input
 * @returns {CollectorError}
 */
function createInsufficientSamplesError(input) {
  return {
    code: insufficientSamplesMessageCode,
    metadata: {
      tool: input.tool,
      interval: String(input.interval),
      duration: String(input.duration),
      minimumDuration: String(
        getMinimumSampleDuration(input.interval, input.minimumSamples),
      ),
      minimumSamples: String(input.minimumSamples),
    },
  };
}

/**
 * Validate that a finite collection window can contain the required number of
 * samples. A duration of zero represents an unlimited collection window.
 *
 * @param {{
 *   tool: string,
 *   interval: number,
 *   duration: number,
 *   minimumSamples: number
 * }} input
 * @returns {CollectorError | null}
 */
function validateSampleWindow(input) {
  if (input.duration === 0) {
    return null;
  }

  const minimumDuration = getMinimumSampleDuration(
    input.interval,
    input.minimumSamples,
  );
  const tolerance = Math.max(1e-9, minimumDuration * 1e-9);
  return input.duration + tolerance < minimumDuration
    ? createInsufficientSamplesError(input)
    : null;
}

/**
 * Limit completed samples to the number of scheduled sample slots inside a
 * collection window.
 *
 * @param {{
 *   totalSamples: number,
 *   interval: number,
 *   duration: number
 * }} input
 * @returns {number}
 */
function getEligibleSampleCount(input) {
  const tolerance = Math.max(1e-9, input.duration * 1e-9);
  const scheduledWithinWindow = Math.floor(
    (input.duration + tolerance) / input.interval,
  );
  return Math.min(input.totalSamples, scheduledWithinWindow);
}

/**
 * Resolve the result of a sampled collection after cleanup.
 *
 * @param {{
 *   tool: string,
 *   interval: number,
 *   duration: number,
 *   minimumSamples: number,
 *   eligibleSamples: number | null,
 *   collectorError: CollectorError | null,
 *   cleanupCausedCollectorError: boolean,
 *   inspectionError: CollectorError
 * }} input
 * @returns {CollectorError | null}
 */
function resolveSampleCollectionError(input) {
  const nonCleanupCollectorError = input.cleanupCausedCollectorError
    ? null
    : input.collectorError;
  const unrelatedCollectorError =
    nonCleanupCollectorError?.code === insufficientSamplesMessageCode
      ? null
      : nonCleanupCollectorError;
  if (unrelatedCollectorError) {
    return unrelatedCollectorError;
  }

  const windowError = validateSampleWindow(input);
  if (windowError) {
    return windowError;
  }

  if (input.eligibleSamples === null) {
    return nonCleanupCollectorError ?? input.inspectionError;
  }

  if (input.eligibleSamples < input.minimumSamples) {
    return createInsufficientSamplesError(input);
  }

  return nonCleanupCollectorError;
}

module.exports = {
  createInsufficientSamplesError,
  getEligibleSampleCount,
  getMinimumSampleDuration,
  resolveSampleCollectionError,
  validateSampleWindow,
};
