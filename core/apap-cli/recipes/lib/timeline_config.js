// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const TABLE_PLACEHOLDER = '{table}';
const RANGE_START_PLACEHOLDER = '{rangeStart}';
const RANGE_END_PLACEHOLDER = '{rangeEnd}';

/**
 * Build the chart-ready query shared by every LoD source in a logical group.
 *
 * The current sources contain expanded bins, so this query selects the source
 * and restricts its results to the requested viewport.
 *
 * @returns {string}
 */
function buildTimelinePivotQuery() {
  return `
    PIVOT (
      SELECT
        x_start,
        concat(
          'dev',
          CAST(device_no AS VARCHAR),
          '_thread',
          CAST(thread AS VARCHAR)
        ) AS device_thread_key,
        value
      FROM ${TABLE_PLACEHOLDER}
      WHERE x_start >= ${RANGE_START_PLACEHOLDER}
        AND x_start < ${RANGE_END_PLACEHOLDER}
    )
    ON device_thread_key
    USING MAX(value)
    GROUP BY x_start
    ORDER BY x_start
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

  /** @type {Map<string, {seriesId: number, sources: TimelineSource[]}>} */
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
      existingGroup.sources.push(source);
    } else {
      sourcesByGroup.set(source.rawSeriesKey, {
        seriesId: source.seriesId,
        sources: [source],
      });
    }
  }

  const logicalGroups = [...sourcesByGroup.entries()]
    .map(([groupKey, group]) => ({
      groupKey,
      seriesId: group.seriesId,
      sources: [...group.sources].sort(
        (left, right) => left.binDuration - right.binDuration,
      ),
    }))
    .sort(
      (left, right) =>
        left.seriesId - right.seriesId ||
        left.groupKey.localeCompare(right.groupKey),
    );

  /** @type {Record<string, any[]>} */
  const tables = {};
  /** @type {Record<string, any>} */
  const groups = {};
  const query = buildTimelinePivotQuery();

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

    const seriesLabel = `Series ${group.seriesId}`;
    groups[group.groupKey] = {
      title: seriesLabel,
      type: 'line',
      index: groupIndex,
      description: `Timeline data for ${seriesLabel}.`,
      lods,
      config: {
        xAxisTitle: 'Time (ns)',
        yAxisTitle: 'Value',
        customQuery: {
          tableNamePlaceholder: TABLE_PLACEHOLDER,
          rangeStartPlaceholder: RANGE_START_PLACEHOLDER,
          rangeEndPlaceholder: RANGE_END_PLACEHOLDER,
          query,
        },
        series: [
          {
            type: 'pattern',
            name: { template: 'Device {y1} Thread {y2}' },
            xColumn: 'x_start',
            yColumn: { pattern: '^dev(\\d+)_thread(\\d+)$' },
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
          xAxisUnit: 'ns',
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
 * Convert the temporary neoprof capture-metadata JSON contract into the
 * provider-owned Timeline domain. The JSON is produced from
 * capture_metadata.parquet by the parquet-to-json converter.
 *
 * @param {{timelineSources: TimelineSource[], captureMetadata: unknown}} args
 * @returns {{visualizations: any[]}}
 */
function buildNeoprofTimelineVisualization(args) {
  if (
    !Array.isArray(args.captureMetadata) ||
    args.captureMetadata.length !== 1
  ) {
    throw new Error(
      'Timeline capture metadata must contain exactly one capture row',
    );
  }

  const captureRow = args.captureMetadata[0];
  if (!captureRow || typeof captureRow !== 'object') {
    throw new Error('Timeline capture metadata row must be an object');
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
  });
}

module.exports = {
  buildNeoprofTimelineVisualization,
  buildTimelinePivotQuery,
  buildTimelineVisualization,
};
