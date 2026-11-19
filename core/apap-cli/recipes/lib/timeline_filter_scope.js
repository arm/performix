// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

// NeoProf exports capture identities, not OS TIDs, in counter.thread.
const SYSTEM_WIDE_THREAD = 0xffffffff;

/**
 * @typedef {{pid?: number|null, tid?: number|null, coreType?: string|null}} TimelineFilters
 * @typedef {{threadIds?: number[], deviceNumbers?: number[], controls: string[],
 * recordedScope: {category: string, value: string, controlId?: string}[],
 * limitations: string[], coreTypes?: {device: number, name: string}[]}} TimelineFilterScope
 */

/** @param {unknown} value @returns {number} */
function identity(value) {
  if (
    !Number.isSafeInteger(value) ||
    Number(value) < 0 ||
    Number(value) >= SYSTEM_WIDE_THREAD
  ) {
    throw new Error(
      'Timeline identity must be a non-negative uint32 below the system-wide sentinel',
    );
  }
  return Number(value);
}

/**
 * Resolve OS selections through capture identities. Sets preserve reused OS IDs
 * without multiplying rows when metadata contains repeated entries.
 * Missing metadata is different from a complete catalogue with no match.
 * @param {{filters: TimelineFilters, processes?: {processes: any[]}, threads?: {threads: any[]}, devices?: {devices: any[]}}} args
 * @returns {TimelineFilterScope}
 */
function resolveTimelineFilterScope({ filters, processes, threads, devices }) {
  const scope = { controls: [], recordedScope: [], limitations: [] };
  const result = /** @type {TimelineFilterScope} */ (scope);
  const { pid, tid, coreType } = filters;
  if (pid != null) identity(pid);
  if (tid != null) identity(tid);
  if (pid != null || tid != null) {
    if (!threads || (pid != null && !processes)) {
      result.limitations.push(
        'Process and thread attribution is unavailable in this capture.',
      );
    } else {
      const processIds = new Set(
        processes?.processes
          .filter((row) => row.process_id === pid)
          .map((row) => identity(row.unique_process_id)),
      );
      result.threadIds = [
        ...new Set(
          threads.threads
            .filter(
              (row) =>
                (pid == null || processIds.has(row.unique_process_id)) &&
                (tid == null || row.thread_id === tid),
            )
            .map((row) => identity(row.unique_thread_id)),
        ),
      ].sort((a, b) => a - b);
      if (pid != null) {
        result.controls.push('process');
        result.recordedScope.push({
          category: 'Process',
          value: [
            ...new Set(
              processes.processes
                .filter((row) => row.process_id === pid)
                .map((row) => row.name)
                .filter(Boolean),
            ),
            `PID ${pid}`,
          ].join(' - '),
          controlId: 'process',
        });
      }
      if (tid != null) {
        result.controls.push('thread');
        result.recordedScope.push({
          category: 'Thread',
          value: [
            ...new Set(
              threads.threads
                .filter((row) =>
                  result.threadIds.includes(row.unique_thread_id),
                )
                .map((row) => row.name)
                .filter(Boolean),
            ),
            `TID ${tid}`,
          ].join(' - '),
          controlId: 'thread',
        });
      }
    }
  }
  if (coreType != null) {
    if (!devices) {
      result.limitations.push(
        'Core type attribution is unavailable in this capture.',
      );
    } else {
      result.deviceNumbers = [
        ...new Set(
          devices.devices
            .filter((row) => row.cluster_name === coreType)
            .map((row) => identity(row.device_no)),
        ),
      ].sort((a, b) => a - b);
      result.controls.push('core_type');
      result.recordedScope.push({
        category: 'Core type',
        value: coreType,
        controlId: 'core_type',
      });
    }
  }
  const coreTypes = (devices?.devices ?? [])
    .filter((row) => row.cluster_name)
    .map((row) => ({
      device: identity(row.device_no),
      name: String(row.cluster_name),
    }));
  if (new Set(coreTypes.map((row) => row.name)).size > 1)
    result.coreTypes = coreTypes;
  return result;
}

/** @param {string} column @param {number[]|undefined} values */
function membership(column, values) {
  if (values === undefined) return 'TRUE';
  return values.length === 0
    ? 'FALSE'
    : `${column} IN (${values.map(identity).join(', ')})`;
}

/**
 * Build a SQL condition for device and thread filtering.
 * Retain system-wide-only sources when a thread filter is selected, without
 * mixing sentinel rows into attributed results.
 * Check attribution across the whole source so panning cannot change it.
 * @param {TimelineFilterScope|undefined} scope
 */
function buildTimelineFilterPredicate(scope) {
  if (!scope) return '';
  const device = membership('source.device_no', scope.deviceNumbers);
  const thread = membership('source.thread', scope.threadIds);
  const attribution =
    scope.threadIds === undefined
      ? thread
      : `(${thread} OR NOT EXISTS (
    SELECT 1 FROM scope_source AS attribution
    WHERE attribution.thread <> ${SYSTEM_WIDE_THREAD}
  ))`;
  return `AND (${device}) AND (${attribution})`;
}

/**
 * Build a scope query that returns one row describing the chart's source data:
 * - matches: whether any rows satisfy the applied filters.
 * - attributed: whether any rows have a capture-specific thread identity.
 * - core_matches: whether any rows belong to the selected core type, when filtered.
 * - core_types: the names of core types present, when a device-name mapping is supplied.
 *
 * Checks source identities without expanding timeline bins.
 * @param {TimelineFilterScope} scope
 */
function buildTimelineScopeQuery(scope) {
  const coreMatchQuery =
    scope.deviceNumbers === undefined
      ? ''
      : `, EXISTS (SELECT 1 FROM scope_source AS source WHERE ${membership('source.device_no', scope.deviceNumbers)}) AS core_matches`;
  const coreTypeQuery = scope.coreTypes?.length
    ? `, (SELECT string_agg(DISTINCT core_type, ', ' ORDER BY core_type) FROM (
        SELECT CASE device_no ${scope.coreTypes.map(({ device, name }) => `WHEN ${identity(device)} THEN '${name.replace(/'/g, "''")}'`).join(' ')} END AS core_type
        FROM scope_source
      )) AS core_types`
    : '';
  return `WITH scope_source AS (SELECT * FROM {table})
    SELECT EXISTS (SELECT 1 FROM scope_source AS source WHERE TRUE ${buildTimelineFilterPredicate(scope)}) AS matches,
      EXISTS (SELECT 1 FROM scope_source WHERE thread <> ${SYSTEM_WIDE_THREAD}) AS attributed${coreTypeQuery}${coreMatchQuery}`;
}

/**
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 * @param {TimelineFilters} filters
 * @returns {TimelineFilterScope|undefined}
 */
function readTimelineFilterScope(context, filters) {
  const hasFilters =
    filters.pid != null || filters.tid != null || filters.coreType != null;
  const metadata = {};
  for (const name of ['processes', 'threads', 'devices']) {
    const path = `tool/neoprof/0/output/parquet/metadata/${name}.json`;
    const components = context.listRunComponents(0, path);
    if (components.length > 1)
      throw new Error(`Multiple timeline ${name} metadata components`);
    if (components.length === 1) {
      const document = JSON.parse(
        context.readRunComponent(0, components[0].relativePath),
      );
      if (!Array.isArray(document[name]))
        throw new Error(`Invalid timeline ${name} metadata`);
      metadata[name] = document;
    }
  }
  const scope = resolveTimelineFilterScope({ filters, ...metadata });
  return hasFilters || scope.coreTypes ? scope : undefined;
}

module.exports = {
  readTimelineFilterScope,
  buildTimelineScopeQuery,
  resolveTimelineFilterScope,
  buildTimelineFilterPredicate,
};
