// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const TIMELINE_COUNTER_DATA_RELATIVE_PATH_PATTERN =
  /^(.+)\/key_type=(\d+)\/series_id=(\d+)\/bin_duration=(\d+)\/counter\.parquet$/i;

const sql = (strings, ...values) => {
  const text = String.raw({ raw: strings }, ...values).trim();
  const lines = text.split('\n');
  const indentedLines = lines.slice(1).filter((line) => line.trim().length > 0);
  const indentation =
    indentedLines.length === 0
      ? 0
      : Math.min(...indentedLines.map((line) => line.match(/^ */)[0].length));

  return [
    lines[0],
    ...lines
      .slice(1)
      .filter((line) => line.trim().length > 0)
      .map((line) => line.slice(indentation)),
  ].join('\n');
};

/**
 * @param {string} componentRelativePath
 * @returns {{
 *   componentRelativePath: string,
 *   rawSeriesKey: string,
 *   keyType: number,
 *   seriesId: number,
 *   binDuration: number,
 * } | null}
 */
function parseTimelineCounterDataPath(componentRelativePath) {
  const relativePath = String(componentRelativePath ?? '');
  const match = relativePath.match(TIMELINE_COUNTER_DATA_RELATIVE_PATH_PATTERN);
  if (!match) {
    return null;
  }

  const keyType = Number(match[2]);
  const seriesId = Number(match[3]);
  const binDuration = Number(match[4]);
  if (
    !Number.isSafeInteger(keyType) ||
    keyType < 0 ||
    !Number.isSafeInteger(seriesId) ||
    seriesId < 0 ||
    !Number.isSafeInteger(binDuration) ||
    binDuration <= 0
  ) {
    return null;
  }

  return {
    componentRelativePath: relativePath,
    rawSeriesKey: `key_${keyType}_series_${seriesId}`,
    keyType,
    seriesId,
    binDuration,
  };
}

/**
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 * @param {number} runIndex
 * @param {string} counterParquetPattern
 * @returns {{
 *   componentRelativePath: string,
 *   rawSeriesKey: string,
 *   keyType: number,
 *   seriesId: number,
 *   binDuration: number,
 * }[]}
 */
function findTimelineCounterBindings(context, runIndex, counterParquetPattern) {
  const bindings = [];
  const seenBindings = new Set();
  const components = context.listRunComponents(runIndex, counterParquetPattern);

  for (const component of components) {
    const componentPath = String(component?.relativePath ?? '');
    const parsedPath = parseTimelineCounterDataPath(componentPath);
    if (!parsedPath) {
      throw new Error(
        `Invalid timeline counter component path: ${componentPath}`,
      );
    }

    const bindingKey = `${parsedPath.keyType}:${parsedPath.seriesId}:${parsedPath.binDuration}`;
    if (seenBindings.has(bindingKey)) {
      continue;
    }

    seenBindings.add(bindingKey);
    bindings.push({
      ...parsedPath,
    });
  }

  if (bindings.length === 0) {
    return [];
  }

  bindings.sort((left, right) => {
    return (
      left.binDuration - right.binDuration ||
      left.keyType - right.keyType ||
      left.seriesId - right.seriesId ||
      left.componentRelativePath.localeCompare(right.componentRelativePath)
    );
  });

  return bindings;
}

/**
 * @param {number} binDuration
 * @param {string} rawSeriesKey
 * @returns {string}
 */
function seriesComponentTypeName(binDuration, rawSeriesKey) {
  return `timeline-source-${rawSeriesKey}-${binDuration}`;
}

/**
 * @param {number} binDuration
 * @param {string} rawSeriesKey
 * @returns {string}
 */
function seriesOutputName(binDuration, rawSeriesKey) {
  return `timeline_source_${rawSeriesKey}_${binDuration}`;
}

/**
 * @param {string} rendererIdPrefix
 * @param {string} rawSeriesKey
 * @param {number} binDuration
 * @returns {string}
 */
function seriesRendererID(rendererIdPrefix, rawSeriesKey, binDuration) {
  return `${rendererIdPrefix}_source_${rawSeriesKey}_${binDuration}`;
}

/**
 * @param {Object} args
 * @param {number} args.binDuration
 * @param {string} args.componentRelativePath
 * @param {string} args.rendererIdPrefix
 * @param {string} args.rawSeriesKey
 * @returns {any}
 */
function makeSeriesRenderer({
  binDuration,
  componentRelativePath,
  rendererIdPrefix,
  rawSeriesKey,
}) {
  return {
    type: 'SQL',
    id: seriesRendererID(rendererIdPrefix, rawSeriesKey, binDuration),
    config: {
      sql: sql`
        -- Preserve compressed source intervals so the provider-authored range
        -- query can discard unrelated rows before expanding bins.
        SELECT
          CAST(base.start_timestamp AS BIGINT) AS start_timestamp,
          CAST(base.end_timestamp AS BIGINT) AS end_timestamp,
          base.series_id,
          CAST(base.bin_duration AS BIGINT) AS bin_duration,
          base.device_no,
          base.thread,
          base.value
        FROM (
          SELECT *
          FROM read_parquet(
            {{path:${componentRelativePath}}},
            hive_partitioning = true
          )
          UNION ALL BY NAME
          -- Legacy capabilities do not record counter_class. Supply the
          -- numeric column so an older activity file binds as an empty source
          -- instead of preventing the whole run from rendering.
          SELECT CAST(NULL AS DOUBLE) AS value
          WHERE FALSE
        ) AS base
        WHERE base.value IS NOT NULL
      `,
      inputs: [],
      output: {
        name: seriesOutputName(binDuration, rawSeriesKey),
        description: `Compressed source ${rawSeriesKey} for bin duration ${binDuration}.`,
        cardinality: 'one',
        component_type: {
          name: seriesComponentTypeName(binDuration, rawSeriesKey),
          schema_version: '1.0',
        },
      },
    },
  };
}

/**
 * Build one compressed timeline SQL source per counter parquet file.
 *
 * Each source reads only its own parquet file. This avoids inspecting every
 * parquet schema in a capture when activity and numeric counter schemas coexist.
 * - Provider-authored presentation queries filter and expand only the requested
 *   range before aggregation and chart projection.
 *
 * @param {Object} args
 * @param {string} [args.rendererId]
 * @param {{
 *   componentRelativePath: string,
 *   rawSeriesKey: string,
 *   keyType: number,
 *   seriesId: number,
 *   binDuration: number,
 * }[]} args.bindings
 * @returns {{
 *   renderers: any[],
 *   timelineSources: Array<{
 *     rawSeriesKey: string,
 *     keyType: number,
 *     rendererId: string,
 *     output: string,
 *     seriesId: number,
 *     binDuration: number,
 *   }>,
 * }}
 */
function buildTimelineSQLRendererBundle(args) {
  const rendererId = args.rendererId ?? 'timeline_sql';
  const bindings = args.bindings ?? [];
  if (bindings.length === 0) {
    return { renderers: [], timelineSources: [] };
  }
  const renderers = [];
  const timelineSources = [];

  for (const binding of bindings) {
    const seriesRenderer = makeSeriesRenderer({
      binDuration: binding.binDuration,
      componentRelativePath: binding.componentRelativePath,
      rendererIdPrefix: rendererId,
      rawSeriesKey: binding.rawSeriesKey,
    });
    renderers.push(seriesRenderer);

    timelineSources.push({
      rawSeriesKey: binding.rawSeriesKey,
      keyType: binding.keyType,
      rendererId: seriesRenderer.id,
      output: seriesOutputName(binding.binDuration, binding.rawSeriesKey),
      seriesId: binding.seriesId,
      binDuration: binding.binDuration,
    });
  }

  return {
    renderers,
    timelineSources,
  };
}

module.exports = {
  findTimelineCounterBindings,
  buildTimelineSQLRendererBundle,
};
