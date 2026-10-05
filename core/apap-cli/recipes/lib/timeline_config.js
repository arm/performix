// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const TABLE_PLACEHOLDER = '{table}';
const RANGE_START_PLACEHOLDER = '{rangeStart}';
const RANGE_END_PLACEHOLDER = '{rangeEnd}';
const NANOSECONDS_PER_SECOND = 1_000_000_000;

/**
 * Build the chart-ready query shared by every LoD source in a logical group.
 *
 * Sources retain compressed intervals. This query first filters overlapping
 * intervals, then expands only source-aligned bin starts in the requested
 * half-open range before applying chart projection.
 *
 * @param {number} [binOrigin]
 * @returns {string}
 */
function buildTimelinePivotQuery(binOrigin) {
  if (binOrigin !== undefined) {
    assertSafeInteger(binOrigin, 'Timeline binOrigin');
  }
  // All intervals for one LoD share an alignment. If no explicit origin is
  // configured, any relevant interval start provides the same grid anchor.
  const binOriginExpression =
    binOrigin === undefined
      ? 'MIN(start_timestamp)'
      : `CAST(${binOrigin} AS BIGINT)`;
  return `
    WITH requested_range AS (
      SELECT
        CAST(${RANGE_START_PLACEHOLDER} AS BIGINT) AS range_start,
        CAST(${RANGE_END_PLACEHOLDER} AS BIGINT) AS range_end
    ),
    relevant_rows AS (
      SELECT
        source.start_timestamp,
        source.end_timestamp,
        source.bin_duration,
        source.value,
        requested_range.range_start,
        requested_range.range_end
      FROM ${TABLE_PLACEHOLDER} AS source
      CROSS JOIN requested_range
      WHERE source.end_timestamp > requested_range.range_start
        AND source.start_timestamp < requested_range.range_end
    ),
    expanded AS (
      SELECT
        CAST(generated.x_start AS BIGINT) AS x_start,
        -- A compressed delta row's value covers its complete interval. Convert
        -- that delta directly to a per-second rate for every represented bin.
        relevant_rows.value * ${NANOSECONDS_PER_SECOND}.0 /
          (relevant_rows.end_timestamp - relevant_rows.start_timestamp) AS value
      FROM relevant_rows
      CROSS JOIN LATERAL generate_series(
        CASE
          WHEN range_start <= start_timestamp
            THEN start_timestamp
          ELSE start_timestamp +
            ((range_start - start_timestamp + bin_duration - 1) // bin_duration) *
            bin_duration
        END,
        LEAST(end_timestamp - bin_duration, range_end - 1),
        bin_duration
      ) AS generated(x_start)
    ),
    aggregated_rows AS (
      SELECT
        x_start,
        SUM(value) AS value
      FROM expanded
      GROUP BY x_start
    ),
    bin_grid AS (
      SELECT CAST(generated.x_start AS BIGINT) AS x_start
      FROM (
        SELECT
          range_start,
          range_end,
          bin_duration,
          ${binOriginExpression} AS bin_origin
        FROM relevant_rows
        GROUP BY
          range_start,
          range_end,
          bin_duration
      ) AS source_range
      CROSS JOIN LATERAL generate_series(
        bin_origin + (
          ((range_start - bin_origin) // bin_duration) +
          CASE
            WHEN ((range_start - bin_origin) % bin_duration) > 0 THEN 1
            ELSE 0
          END
        ) * bin_duration,
        range_end - 1,
        bin_duration
      ) AS generated(x_start)
    )
    SELECT
      bin_grid.x_start,
      COALESCE(aggregated_rows.value, 0) AS value
    FROM bin_grid
    LEFT JOIN aggregated_rows
      ON aggregated_rows.x_start = bin_grid.x_start
    ORDER BY bin_grid.x_start
  `.trim();
}

/**
 * @param {unknown} value
 * @param {string} name
 * @returns {asserts value is number}
 */
function assertSafeInteger(value, name) {
  if (!Number.isSafeInteger(value)) {
    throw new Error(`${name} must be a safe integer`);
  }
}

/**
 * @typedef {Object} TimelineSource
 * @property {string} rawSeriesKey
 * @property {number} [keyType]
 * @property {string} rendererId
 * @property {string} output
 * @property {number} seriesId
 * @property {number} binDuration
 */

/**
 * @typedef {Object} TimelineConfigArgs
 * @property {TimelineSource[]} timelineSources
 * @property {{start: number, end: number, unit: 'ns'}} timeDomain
 * @property {number} [binOrigin]
 * @property {readonly number[]} [expectedBinDurations]
 * @property {string} [incompleteCatalogueMessageCode]
 * @property {Map<string, {title: string, description: string, units: string}>} [metadataBySeriesKey]
 * @property {Map<number, {title: string, description: string, units: string}>} [metadataBySeriesId]
 */

/**
 * Build one logical Timeline group per raw neoprof series.
 *
 * Each renderer source represents one bin duration. The catalogue maps those
 * sources into a group with a shared presentation query.
 *
 * @param {TimelineConfigArgs} args
 * @returns {{visualizations: any[]}}
 */
function buildTimelineVisualization(args) {
  const timelineSources = args.timelineSources ?? [];
  if (timelineSources.length === 0) {
    return { visualizations: [] };
  }

  /** @type {Map<string, {keyType?: number, seriesId: number, sources: TimelineSource[]}>} */
  const sourcesByGroup = new Map();

  for (const source of timelineSources) {
    if (!source || typeof source !== 'object') {
      throw new Error('Timeline source must be an object');
    }
    if (typeof source.rawSeriesKey !== 'string') {
      throw new Error('Timeline source rawSeriesKey must be a string');
    }
    assertSafeInteger(source.seriesId, 'Timeline source seriesId');
    if (source.seriesId < 0) {
      throw new Error('Timeline source seriesId must not be negative');
    }
    if (source.keyType !== undefined) {
      assertSafeInteger(source.keyType, 'Timeline source keyType');
      if (source.keyType < 0) {
        throw new Error('Timeline source keyType must not be negative');
      }
    }
    if (
      typeof source.rendererId !== 'string' ||
      source.rendererId.length === 0
    ) {
      throw new Error('Timeline source rendererId is required');
    }
    if (typeof source.output !== 'string' || source.output.length === 0) {
      throw new Error('Timeline source output is required');
    }

    const existingGroup = sourcesByGroup.get(source.rawSeriesKey);
    if (existingGroup) {
      if (existingGroup.seriesId !== source.seriesId) {
        throw new Error(
          `Timeline group ${source.rawSeriesKey} contains inconsistent series IDs`,
        );
      }
      if (existingGroup.keyType !== source.keyType) {
        throw new Error(
          `Timeline group ${source.rawSeriesKey} contains inconsistent key types`,
        );
      }
      existingGroup.sources.push(source);
    } else {
      sourcesByGroup.set(source.rawSeriesKey, {
        ...(source.keyType === undefined ? {} : { keyType: source.keyType }),
        seriesId: source.seriesId,
        sources: [source],
      });
    }
  }

  const logicalGroups = [...sourcesByGroup.entries()]
    .map(([groupKey, group]) => ({
      groupKey,
      keyType: group.keyType,
      seriesId: group.seriesId,
      sources: [...group.sources].sort(
        (left, right) => left.binDuration - right.binDuration,
      ),
    }))
    .sort(
      (left, right) =>
        (left.keyType ?? -1) - (right.keyType ?? -1) ||
        left.seriesId - right.seriesId ||
        left.groupKey.localeCompare(right.groupKey),
    );

  // Every series must emit every expected LoD file, even when that file
  // contains zero rows.
  const expectedBinDurations = [
    ...(args.expectedBinDurations ??
      logicalGroups[0].sources.map((source) => source.binDuration)),
  ].sort((left, right) => left - right);
  for (const duration of expectedBinDurations) {
    assertSafeInteger(duration, 'Timeline expected bin duration');
    if (duration <= 0) {
      throw new Error('Timeline expected bin duration must be positive');
    }
  }
  if (new Set(expectedBinDurations).size !== expectedBinDurations.length) {
    throw new Error('Timeline expected bin durations must be unique');
  }

  for (const group of logicalGroups) {
    const binDurations = group.sources.map((source) => source.binDuration);
    if (
      binDurations.length !== expectedBinDurations.length ||
      binDurations.some(
        (duration, index) => duration !== expectedBinDurations[index],
      )
    ) {
      const cause = `Timeline LoD catalogue is inconsistent: group ${group.groupKey} defines bin durations [${binDurations.join(', ')}], expected [${expectedBinDurations.join(', ')}]`;
      if (args.incompleteCatalogueMessageCode !== undefined) {
        throw {
          code: args.incompleteCatalogueMessageCode,
          metadata: {
            groupKey: group.groupKey,
            availableBinDurations: binDurations.join(', '),
            expectedBinDurations: expectedBinDurations.join(', '),
          },
          cause,
        };
      }
      throw new Error(cause);
    }
  }

  /** @type {Record<string, any[]>} */
  const tables = {};
  /** @type {Record<string, any>} */
  const groups = {};
  const query = buildTimelinePivotQuery(args.binOrigin);

  for (const [groupIndex, group] of logicalGroups.entries()) {
    const lods = [];
    for (const source of group.sources) {
      const sourceKey = `${group.groupKey}_${source.binDuration}`;
      tables[sourceKey] = [
        {
          renderer_id: source.rendererId,
          output: source.output,
        },
      ];
      lods.push({
        binDuration: source.binDuration,
        sourceKey,
      });
    }

    const seriesMetadata =
      args.metadataBySeriesKey?.get(group.groupKey) ??
      args.metadataBySeriesId?.get(group.seriesId);
    const defaultSeriesLabel =
      group.keyType === undefined
        ? `Series ${group.seriesId}`
        : `Key ${group.keyType}, Series ${group.seriesId}`;
    const seriesLabel = seriesMetadata?.title ?? defaultSeriesLabel;
    groups[group.groupKey] = {
      title: seriesLabel,
      type: 'line',
      index: groupIndex,
      description:
        seriesMetadata?.description ?? `Timeline data for ${seriesLabel}.`,
      lods,
      config: {
        xAxisTitle: 'Time (s)',
        yAxisTitle: 'Rate',
        yAxisUnit:
          seriesMetadata?.units.length > 0
            ? `${seriesMetadata.units}/s`
            : 'events/s',
        customQuery: {
          tableNamePlaceholder: TABLE_PLACEHOLDER,
          rangeStartPlaceholder: RANGE_START_PLACEHOLDER,
          rangeEndPlaceholder: RANGE_END_PLACEHOLDER,
          query,
        },
        series: [
          {
            type: 'single',
            name: 'Total',
            xColumn: 'x_start',
            yColumn: 'value',
          },
        ],
      },
    };
  }

  return {
    visualizations: [
      {
        type: 'timeline',
        id: 'timeline',
        rendererId: logicalGroups[0].sources[0].rendererId,
        title: 'Timeline',
        description: 'Preview timeline data for hotspots analysis.',
        config: {
          xAxisUnit: 's',
          xAxisDisplayScale: 1e-9,
          timeDomain: args.timeDomain,
          ...(args.binOrigin === undefined
            ? {}
            : { binOrigin: args.binOrigin }),
          data_source: {
            tables,
          },
          groups,
        },
      },
    ],
  };
}

/**
 * Convert neoprof capture metadata into the Timeline domain.
 *
 * @param {{
 *   timelineSources: TimelineSource[],
 *   captureMetadata: unknown,
 *   expectedBinDurations?: readonly number[],
 *   incompleteCatalogueMessageCode?: string,
 *   metadataBySeriesKey?: Map<string, {title: string, description: string, units: string}>,
 *   metadataBySeriesId?: Map<number, {title: string, description: string, units: string}>
 * }} args
 * @returns {{visualizations: any[]}}
 */
function buildNeoprofTimelineVisualization(args) {
  const captureRow = args.captureMetadata;
  if (
    !captureRow ||
    typeof captureRow !== 'object' ||
    Array.isArray(captureRow)
  ) {
    throw new Error('Timeline capture metadata must be an object');
  }
  const capture = /** @type {{duration?: unknown, time_unit?: unknown}} */ (
    captureRow
  );
  assertSafeInteger(capture.duration, 'Timeline capture duration');
  if (capture.duration < 0) {
    throw new Error('Timeline capture duration must not be negative');
  }
  if (capture.time_unit !== 'nanoseconds') {
    throw new Error('Timeline capture time_unit must be nanoseconds');
  }

  return buildTimelineVisualization({
    timelineSources: args.timelineSources,
    timeDomain: {
      start: 0,
      end: capture.duration,
      unit: 'ns',
    },
    binOrigin: 0,
    expectedBinDurations: args.expectedBinDurations,
    incompleteCatalogueMessageCode: args.incompleteCatalogueMessageCode,
    metadataBySeriesKey: args.metadataBySeriesKey,
    metadataBySeriesId: args.metadataBySeriesId,
  });
}

module.exports = {
  buildNeoprofTimelineVisualization,
  buildTimelinePivotQuery,
  buildTimelineVisualization,
};
