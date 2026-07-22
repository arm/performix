// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const TOOL_SYSUTIL_TIMELINE = {
  name: 'sysutil-timeline',
  version: '1.0.0',
};

const CPU_SATURATION_THRESHOLD_PERCENT = 80;
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

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
 * Builds the canonical per-sample CPU saturation query.
 *
 * CPU saturation is defined as the number of per-core CPU utilization columns
 * whose value is at or above `CPU_SATURATION_THRESHOLD_PERCENT` for a sample.
 * The synthetic `sample_id` prevents duplicate timestamps from merging separate
 * samples in residence-time calculations.
 *
 * The saturated-core count is computed row-wise with `unpack(COLUMNS(...))`
 * instead of `UNPIVOT` so each sample row stays one row throughout the query.
 * Samples with no numeric per-core CPU values are excluded.
 *
 * The returned query emits `sample_id`, `uptime_s`, and `saturated_cores`.
 *
 * @param {string} tableNamePlaceholder
 * @returns {string}
 */
const makeCpuSaturationSampleQuery = (tableNamePlaceholder) => sql`
  WITH samples AS (
    SELECT
      row_number() OVER () AS sample_id,
      try_cast("uptime_s" AS DOUBLE) AS uptime_s,
      -- Keep CPU columns row-local instead of expanding samples x cores with UNPIVOT.
      list_value(unpack(COLUMNS('^cpu[0-9]+_percent$'))) AS cpu_values
    FROM ${tableNamePlaceholder}
    WHERE try_cast("uptime_s" AS DOUBLE) IS NOT NULL
  ), cpu_saturation AS (
    SELECT
      sample_id,
      uptime_s,
      -- Exclude samples where every per-core value is null or non-numeric.
      list_sum(
        list_transform(
          cpu_values,
          lambda x: CASE WHEN try_cast(x AS DOUBLE) IS NOT NULL THEN 1 ELSE 0 END
        )
      ) AS valid_core_count,
      -- Count cores whose utilization meets the saturation threshold.
      CAST(
        list_sum(
          list_transform(
            cpu_values,
            lambda x: CASE WHEN try_cast(x AS DOUBLE) >= ${CPU_SATURATION_THRESHOLD_PERCENT} THEN 1 ELSE 0 END
          )
        ) AS INTEGER
      ) AS saturated_cores
    FROM samples
  )
  SELECT
    sample_id,
    uptime_s,
    saturated_cores
  FROM cpu_saturation
  WHERE valid_core_count > 0
`;

/**
 * Timeline renderer query for the CPU Saturation chart.
 *
 * It reuses the canonical saturation sample query but exposes only the columns
 * expected by the timeline series: `uptime_s` and `saturated_cores`.
 */
const CPU_SATURATION_TIMELINE_QUERY = `
  WITH cpu_saturation AS (
    ${makeCpuSaturationSampleQuery('{table}')}
  )
  SELECT
    uptime_s,
    saturated_cores
  FROM cpu_saturation
  ORDER BY "uptime_s", sample_id
`;

const TIMELINE_TABLE_PLACEHOLDER = '__TIMELINE__';
const TIMELINE_TABLE_NAME = 'timeline';
const TIMELINE_TABLE_DATA_SOURCE = [
  { renderer_id: 'timeline_csv', output: 'csv' },
];

/**
 * Wraps a generated SQL string in the query-config shape expected by the
 * deep-dive renderer.
 *
 * The renderer does not execute raw SQL directly. It expects the query text
 * plus enough metadata to substitute the real table name for the recipe's
 * timeline CSV and to know which logical table key the result belongs to.
 *
 * All query-builder helpers return this shape so that the individual metric
 * declarations only describe what to compute, not how to wire the query into
 * the renderer contract.
 *
 * @param {string} query
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeTimelineQueryConfig = (query) => ({
  query,
  tableNamePlaceholder: TIMELINE_TABLE_PLACEHOLDER,
  tableKey: TIMELINE_TABLE_NAME,
});

/**
 * Produces the canonical SQL expression for reading a single timeline column as
 * a numeric sample stream.
 *
 * Many timeline CSV fields are strings when read by DuckDB. Converting through
 * `try_cast(... AS DOUBLE)` gives the rest of the helper layer a consistent
 * numeric expression and naturally drops malformed samples to NULL so later
 * `WHERE value IS NOT NULL` filters can remove them.
 *
 * The returned string is not a complete query. It is an expression fragment
 * that is inserted into the `samples` CTEs built below.
 *
 * @param {string} columnName
 * @returns {string}
 */
const makeColumnValueSql = (columnName) =>
  `try_cast("${columnName}" AS DOUBLE)`;

/**
 * Builds the summary query used by a deep-dive metric.
 *
 * Query flow:
 * 1. `bound_source` (optional): emits one row containing derived metadata that
 *    later expressions need, such as the discovered CPU core count. This exists
 *    only for metrics whose sample value depends on runtime table structure.
 * 2. `samples`: emits one row per timeline sample with a single normalized
 *    numeric `value` column. This isolates the metric-specific expression from
 *    the shared summary logic.
 * 3. Final SELECT: reduces the sample stream to peak and average values after
 *    discarding NULLs.
 *
 * The `samples` CTE is the key normalization step: once a metric has been
 * represented as `(timestamp?, value)`, the renderer can reuse the same summary
 * reduction regardless of whether the metric came from one column, a derived
 * expression, or a table-shape-dependent expression.
 *
 * @param {Object} args
 * @param {string} [args.columnName]
 *   Timeline column to read when the metric is a direct column summary.
 * @param {string} [args.sampleValueSql]
 *   Full SQL expression that computes the metric's numeric sample value.
 * @param {string} [args.deriveUpperBoundSql]
 *   Optional one-row SQL subquery whose columns are made available to
 *   `sampleValueSql` through `CROSS JOIN bound_source`.
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeSummaryQuery = ({
  columnName,
  sampleValueSql,
  deriveUpperBoundSql,
}) => {
  const valueSql = sampleValueSql ?? makeColumnValueSql(columnName);
  const withSql = deriveUpperBoundSql
    ? sql`
        WITH bound_source AS (
          ${deriveUpperBoundSql}
        ), samples AS (
          SELECT ${valueSql} AS value
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          CROSS JOIN bound_source
        ), filtered_samples AS (
          SELECT value
          FROM samples
          WHERE value IS NOT NULL
        )
      `
    : sql`
        WITH samples AS (
          SELECT ${valueSql} AS value
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
        ), filtered_samples AS (
          SELECT value
          FROM samples
          WHERE value IS NOT NULL
        )
      `;

  return makeTimelineQueryConfig(sql`
    ${withSql}
    SELECT
      max(value) AS peak_value,
      avg(value) AS average_value
    FROM filtered_samples
  `);
};

/**
 * Builds a histogram query for metrics whose buckets are evenly spaced.
 *
 * Query flow:
 * 1. `bound_source` (optional): emits one metadata row used to discover the
 *    upper bound for derived metrics whose natural range depends on the input
 *    table, such as CPU utilization expressed in "cores busy".
 * 2. `bucket_domain`: materializes the complete set of output buckets. This is
 *    intentionally created before touching samples so the final result can emit
 *    zero-duration buckets and therefore produce a stable histogram shape.
 * 3. `samples`: normalizes the timeline to `(uptime_s, value)` rows. Filtering
 *    here removes any sample that cannot participate in duration accounting.
 * 4. `durations`: transforms point samples into residence durations by looking
 *    at the next sample's timestamp, or the previous one for the final row.
 *    The result is one row per sample value with the amount of time that value
 *    is considered to have been "active".
 * 5. `aggregated`: assigns each sample value to an evenly spaced bucket and
 *    sums the residence durations per bucket.
 * 6. Final SELECT: left-joins the aggregated durations onto the full bucket
 *    domain so empty buckets still appear with `bucket_value = 0`.
 *
 * This helper exists because the duration-accounting part of the query is the
 * same for all linear histograms; only the source value expression and bucket
 * range differ between metrics.
 *
 * @param {Object} args
 * @param {number} args.bucketWidth
 * @param {string} [args.columnName]
 * @param {string} [args.deriveUpperBoundSql]
 * @param {number|string} [args.maxBucketStart]
 * @param {number} [args.minBucketStart=0]
 * @param {string} [args.sampleValueSql]
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeBoundedLinearHistogramQuery = ({
  bucketWidth,
  columnName,
  deriveUpperBoundSql,
  maxBucketStart,
  minBucketStart = 0,
  sampleValueSql,
}) => {
  const valueSql = sampleValueSql ?? makeColumnValueSql(columnName);
  const withSql = deriveUpperBoundSql
    ? sql`
        WITH bound_source AS (
          ${deriveUpperBoundSql}
        ), bucket_domain AS (
          SELECT
            bucket_start,
            bucket_start + ${bucketWidth} AS bucket_end
          FROM bound_source
          CROSS JOIN generate_series(0, upper_bucket_start, ${bucketWidth}) AS t(bucket_start)
        ), samples AS (
          SELECT
            try_cast("uptime_s" AS DOUBLE) AS uptime_s,
            ${valueSql} AS value
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          CROSS JOIN bound_source
          WHERE try_cast("uptime_s" AS DOUBLE) IS NOT NULL
        ), filtered_samples AS (
          SELECT uptime_s, value
          FROM samples
          WHERE value IS NOT NULL
        )
      `
    : sql`
        WITH bucket_domain AS (
          SELECT
            bucket_start,
            bucket_start + ${bucketWidth} AS bucket_end
          FROM generate_series(${minBucketStart}, ${maxBucketStart}, ${bucketWidth}) AS t(bucket_start)
        ), samples AS (
          SELECT
            try_cast("uptime_s" AS DOUBLE) AS uptime_s,
            ${valueSql} AS value
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          WHERE try_cast("uptime_s" AS DOUBLE) IS NOT NULL
        ), filtered_samples AS (
          SELECT uptime_s, value
          FROM samples
          WHERE value IS NOT NULL
        )
      `;
  const bucketUpperBoundSql = deriveUpperBoundSql
    ? 'upper_bucket_start'
    : maxBucketStart;

  return makeTimelineQueryConfig(sql`
    ${withSql}
    , durations AS (
      SELECT
        value,
        coalesce(
          lead(uptime_s) OVER (ORDER BY uptime_s) - uptime_s,
          uptime_s - lag(uptime_s) OVER (ORDER BY uptime_s),
          0
        ) AS sample_duration_s
      FROM filtered_samples
    ), aggregated AS (
      SELECT
        least(
          floor(greatest(value, 0) / ${bucketWidth}) * ${bucketWidth},
          ${bucketUpperBoundSql}
        ) AS bucket_start,
        sum(greatest(sample_duration_s, 0)) AS bucket_value
      FROM durations
      ${deriveUpperBoundSql ? 'CROSS JOIN bound_source' : ''}
      GROUP BY 1
    )
    SELECT
      CAST(bucket_domain.bucket_start AS DOUBLE) AS bucket_start,
      CAST(bucket_domain.bucket_end AS DOUBLE) AS bucket_end,
      coalesce(aggregated.bucket_value, 0) AS bucket_value
    FROM bucket_domain
    LEFT JOIN aggregated USING (bucket_start)
    ORDER BY bucket_domain.bucket_start
  `);
};

/**
 * Summary deep-dive query for CPU saturation.
 *
 * Reduces the canonical per-sample saturation stream to the peak and average
 * number of saturated cores.
 *
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeCpuSaturationSummaryQuery = () =>
  makeTimelineQueryConfig(sql`
    WITH cpu_saturation AS (
      ${makeCpuSaturationSampleQuery(TIMELINE_TABLE_PLACEHOLDER)}
    )
    SELECT
      max(saturated_cores) AS peak_value,
      avg(saturated_cores) AS average_value
    FROM cpu_saturation
  `);

/**
 * Histogram deep-dive query for CPU saturation.
 *
 * Buckets the per-sample saturated-core counts into integer core-count buckets
 * from 0 to the discovered CPU count, then sums residence time in each bucket.
 *
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeCpuSaturationHistogramQuery = () =>
  makeTimelineQueryConfig(sql`
    WITH bound_source AS (
      ${CPU_UTILIZATION_UPPER_BOUND_SQL}
    ), bucket_domain AS (
      SELECT
        bucket_start,
        bucket_start + 1 AS bucket_end
      FROM bound_source
      CROSS JOIN generate_series(0, upper_bucket_start, 1) AS t(bucket_start)
    ), cpu_saturation AS (
      ${makeCpuSaturationSampleQuery(TIMELINE_TABLE_PLACEHOLDER)}
    ), durations AS (
      SELECT
        saturated_cores,
        coalesce(
          lead(uptime_s) OVER (ORDER BY uptime_s, sample_id) - uptime_s,
          uptime_s - lag(uptime_s) OVER (ORDER BY uptime_s, sample_id),
          0
        ) AS sample_duration_s
      FROM cpu_saturation
    ), aggregated AS (
      SELECT
        saturated_cores AS bucket_start,
        sum(greatest(sample_duration_s, 0)) AS bucket_value
      FROM durations
      GROUP BY 1
    )
    SELECT
      CAST(bucket_domain.bucket_start AS DOUBLE) AS bucket_start,
      CAST(bucket_domain.bucket_end AS DOUBLE) AS bucket_end,
      coalesce(aggregated.bucket_value, 0) AS bucket_value
    FROM bucket_domain
    LEFT JOIN aggregated USING (bucket_start)
    ORDER BY bucket_domain.bucket_start
  `);

/**
 * Converts an ordered list of bucket edges into adjacent `(index, start, end)`
 * tuples for explicit-edge histograms.
 *
 * Each value becomes the lower edge of one bucket. The next value is used as
 * that bucket's exclusive upper edge. The last bucket receives `NULL` as its
 * upper edge to model an open-ended "and above" bucket. A leading `NULL` value
 * models an open-ended lower bucket, such as "below 1024".
 *
 * `bucket_index` is carried through the query so open-ended lower buckets can be
 * joined and ordered reliably; SQL equality does not match `NULL` values.
 *
 * The output is inserted directly into a SQL `VALUES` clause and therefore
 * represents bucket definitions, not sample data. It is used for both concrete
 * bucket starts and CPU-relative bucket multipliers.
 *
 * @param {(number | null)[]} bucketEdges
 * @returns {string}
 */
const makeAdjacentBucketEdgesSql = (bucketEdges) =>
  bucketEdges
    .map(
      (start, index) =>
        `(${index}, ${start ?? 'NULL'}, ${bucketEdges[index + 1] ?? 'NULL'})`,
    )
    .join(',\n          ');

/**
 * Builds the bucket-edge CTE for a static explicit-edge histogram.
 *
 * The returned SQL fragment deliberately does not include `WITH`; callers can
 * compose it with other CTEs before running the shared residence-time
 * aggregation query.
 *
 * @param {(number | null)[]} bucketStarts
 * @returns {string}
 */
const makeStaticBucketEdgesCteSql = (bucketStarts) => sql`
  bucket_edges AS (
    SELECT *
    FROM (
      VALUES ${makeAdjacentBucketEdgesSql(bucketStarts)}
    ) AS t(bucket_index, bucket_start, bucket_end)
  )
`;

/**
 * Builds the bucket-edge CTEs for a CPU-relative explicit-edge histogram.
 *
 * `bound_source` discovers the target CPU count from the runtime timeline table
 * schema. `multiplier_edges` defines the relative bucket shape. `bucket_edges`
 * then resolves those multipliers into concrete metric values by multiplying
 * them by `core_count`.
 *
 * @param {number[]} bucketMultipliers
 * @returns {string}
 */
const makeCpuRelativeBucketEdgesCteSql = (bucketMultipliers) => sql`
  bound_source AS (
    ${CPU_UTILIZATION_UPPER_BOUND_SQL}
  ), multiplier_edges AS (
    SELECT *
    FROM (
      VALUES ${makeAdjacentBucketEdgesSql(bucketMultipliers)}
    ) AS t(bucket_index, start_multiplier, end_multiplier)
  ), bucket_edges AS (
    SELECT
      bucket_index,
      core_count * start_multiplier AS bucket_start,
      CASE
        WHEN end_multiplier IS NULL THEN NULL
        ELSE core_count * end_multiplier
      END AS bucket_end
    FROM bound_source
    CROSS JOIN multiplier_edges
  )
`;

/**
 * Builds bucket edges from a machine-size-aware hot-start threshold.
 *
 * The supplied SQL expression computes `hot_start` from `core_count` and
 * `scale`; bucket multipliers then place buckets below and above that threshold.
 * A `NULL` multiplier keeps the first bucket lower-open so the renderer labels
 * it as "below N".
 *
 * @param {Object} args
 * @param {(number | null)[]} args.bucketMultipliers
 * @param {string} args.hotStartSql
 * @returns {string}
 */
const makeHotStartRelativeBucketEdgesCteSql = ({
  bucketMultipliers,
  hotStartSql,
}) => sql`
  bound_source AS (
    ${CPU_UTILIZATION_UPPER_BOUND_SQL}
  ), machine_class AS (
    SELECT
      core_count,
      least(log2(greatest(core_count, 1)) / 7, 1) AS scale
    FROM bound_source
  ), hot_start_source AS (
    -- Build count buckets around one machine-size-aware "hot" threshold.
    SELECT ${hotStartSql} AS hot_start
    FROM machine_class
  ), multiplier_edges AS (
    SELECT *
    FROM (
      VALUES ${makeAdjacentBucketEdgesSql(bucketMultipliers)}
    ) AS t(bucket_index, start_multiplier, end_multiplier)
  ), bucket_edges AS (
    SELECT
      bucket_index,
      CASE
        WHEN start_multiplier IS NULL THEN NULL
        -- Count bucket labels should be whole numbers.
        ELSE ceil(hot_start * start_multiplier)
      END AS bucket_start,
      CASE
        WHEN end_multiplier IS NULL THEN NULL
        ELSE ceil(hot_start * end_multiplier)
      END AS bucket_end
    FROM hot_start_source
    CROSS JOIN multiplier_edges
  )
`;

/**
 * Builds a residence-time histogram from a supplied `bucket_edges` CTE.
 *
 * Query flow:
 * 1. The caller-provided CTEs must define `bucket_edges(bucket_index,
 *    bucket_start, bucket_end)`. `bucket_start = NULL` means the first bucket is
 *    lower-open; `bucket_end = NULL` means the final bucket is upper-open.
 * 2. `samples` normalizes the timeline to `(uptime_s, value)` rows.
 * 3. `filtered_samples` removes values that cannot participate in duration
 *    accounting.
 * 4. `durations` converts point samples into residence durations using the next
 *    timestamp, or the previous timestamp for the final row.
 * 5. `aggregated` assigns each sample duration to its bucket and sums residence
 *    time.
 * 6. The final SELECT emits every bucket, including empty buckets, in renderer
 *    row shape.
 *
 * Keeping bucket-edge construction outside this helper lets static and
 * CPU-relative histograms share the same duration and aggregation logic.
 *
 * @param {Object} args
 * @param {string} args.bucketEdgesCteSql
 * @param {string} [args.columnName]
 * @param {string} [args.sampleValueSql]
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeBucketedResidenceHistogramQuery = ({
  bucketEdgesCteSql,
  columnName,
  sampleValueSql,
}) => {
  const valueSql = sampleValueSql ?? makeColumnValueSql(columnName);

  return makeTimelineQueryConfig(sql`
    WITH ${bucketEdgesCteSql}
    , samples AS (
      SELECT
        try_cast("uptime_s" AS DOUBLE) AS uptime_s,
        ${valueSql} AS value
      FROM ${TIMELINE_TABLE_PLACEHOLDER}
      WHERE try_cast("uptime_s" AS DOUBLE) IS NOT NULL
    ), filtered_samples AS (
      SELECT uptime_s, value
      FROM samples
      WHERE value IS NOT NULL
    ), durations AS (
      SELECT
        value,
        coalesce(
          lead(uptime_s) OVER (ORDER BY uptime_s) - uptime_s,
          uptime_s - lag(uptime_s) OVER (ORDER BY uptime_s),
          0
        ) AS sample_duration_s
      FROM filtered_samples
    ), aggregated AS (
      SELECT
        bucket_edges.bucket_index,
        bucket_edges.bucket_start,
        sum(greatest(durations.sample_duration_s, 0)) AS bucket_value
      FROM bucket_edges
      LEFT JOIN durations
        ON (
         bucket_edges.bucket_start IS NULL OR durations.value >= bucket_edges.bucket_start
       )
       AND (
         bucket_edges.bucket_end IS NULL OR durations.value < bucket_edges.bucket_end
       )
      GROUP BY bucket_edges.bucket_index, bucket_edges.bucket_start
    )
    SELECT
      CAST(bucket_edges.bucket_start AS DOUBLE) AS bucket_start,
      CAST(bucket_edges.bucket_end AS DOUBLE) AS bucket_end,
      coalesce(aggregated.bucket_value, 0) AS bucket_value
    FROM bucket_edges
    LEFT JOIN aggregated USING (bucket_index)
    ORDER BY bucket_edges.bucket_index
  `);
};

/**
 * Builds a histogram query for metrics whose bucket boundaries are explicit and
 * possibly non-linear.
 *
 * This is used for rate and size metrics where meaningful ranges are hand-picked
 * powers of two or other domain-specific thresholds.
 *
 * @param {Object} args
 * @param {number[]} args.bucketStarts
 * @param {string} [args.columnName]
 * @param {string} [args.sampleValueSql]
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeEdgeHistogramQuery = ({ bucketStarts, columnName, sampleValueSql }) =>
  makeBucketedResidenceHistogramQuery({
    bucketEdgesCteSql: makeStaticBucketEdgesCteSql(bucketStarts),
    columnName,
    sampleValueSql,
  });

/**
 * Builds an explicit-edge histogram whose bucket edges are multiplied by the
 * discovered CPU core count.
 *
 * This keeps load, runqueue, and kernel rate histograms from treating values
 * that are normal on many-core targets as intrinsically hot.
 *
 * @param {Object} args
 * @param {number[]} args.bucketMultipliers
 * @param {string} [args.columnName]
 * @param {string} [args.sampleValueSql]
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeCpuRelativeEdgeHistogramQuery = ({
  bucketMultipliers,
  columnName,
  sampleValueSql,
}) =>
  makeBucketedResidenceHistogramQuery({
    bucketEdgesCteSql: makeCpuRelativeBucketEdgesCteSql(bucketMultipliers),
    columnName,
    sampleValueSql,
  });

/**
 * Builds an explicit-edge histogram whose bucket edges are derived from a
 * dynamic hot-start threshold.
 *
 * @param {Object} args
 * @param {(number | null)[]} args.bucketMultipliers
 * @param {string} [args.columnName]
 * @param {string} args.hotStartSql
 * @param {string} [args.sampleValueSql]
 * @returns {{query: string, tableNamePlaceholder: string, tableKey: string}}
 */
const makeHotStartRelativeEdgeHistogramQuery = ({
  bucketMultipliers,
  columnName,
  hotStartSql,
  sampleValueSql,
}) =>
  makeBucketedResidenceHistogramQuery({
    bucketEdgesCteSql: makeHotStartRelativeBucketEdgesCteSql({
      bucketMultipliers,
      hotStartSql,
    }),
    columnName,
    sampleValueSql,
  });

/**
 * Creates one deep-dive metric definition in renderer format.
 *
 * This function does not generate data itself. It packages a human-facing
 * metric description together with the summary and histogram queries that power
 * the deep-dive panel, plus the timeline groups that should be shown when the
 * user drills into the metric.
 *
 * The default summary query assumes the metric is backed by a single timeline
 * column. Derived metrics can override that by passing an explicit
 * `summaryQuery`.
 *
 * @param {Object} args
 * @param {string} args.columnName
 * @param {{query: string, tableNamePlaceholder: string, tableKey: string}} args.histogramQuery
 * @param {string} [args.histogramResidenceUnit='seconds']
 * @param {string} args.key
 * @param {string} args.label
 * @param {string} [args.description]
 * @param {{query: string, tableNamePlaceholder: string, tableKey: string}} [args.summaryQuery]
 * @param {string} args.summaryValueUnit
 * @param {string[]} args.timelineGroups
 * @param {string} [args.triggersStatusLabelFor]
 * @returns {Object}
 */
const createDeepDiveRow = ({
  columnName,
  histogramQuery,
  histogramResidenceUnit = 'seconds',
  key,
  label,
  description,
  summaryQuery,
  summaryValueUnit,
  timelineGroups,
  triggersStatusLabelFor,
}) => ({
  key,
  label,
  summaryValueUnit,
  histogramResidenceUnit,
  summaryQuery: summaryQuery ?? makeSummaryQuery({ columnName }),
  histogramQuery,
  timelineDrilldown: {
    visibleGroups: timelineGroups,
  },
  ...(description ? { description } : {}),
  ...(triggersStatusLabelFor ? { triggersStatusLabelFor } : {}),
});

/**
 * Convenience wrapper for metrics whose histogram is defined by explicit bucket
 * edges.
 *
 * It keeps the metric declarations short by generating both the summary query
 * and histogram query from the same value definition:
 * - direct-column metrics use `columnName`
 * - derived metrics provide `sampleValueSql`
 *
 * This is the common path for process counts, rates, bandwidths, and any other
 * metric where bucket spacing is deliberately irregular.
 *
 * @param {Object} args
 * @param {number[]} args.bucketStarts
 * @param {string} args.columnName
 * @param {string} [args.sampleValueSql]
 * @param {{query: string, tableNamePlaceholder: string, tableKey: string}} [args.summaryQuery]
 * @returns {Object}
 */
const createBucketedDeepDiveRow = ({
  bucketStarts,
  columnName,
  sampleValueSql,
  summaryQuery,
  ...row
}) =>
  createDeepDiveRow({
    ...row,
    columnName,
    summaryQuery:
      summaryQuery ??
      (sampleValueSql ? makeSummaryQuery({ sampleValueSql }) : undefined),
    histogramQuery: makeEdgeHistogramQuery({
      bucketStarts,
      columnName,
      sampleValueSql,
    }),
  });

/**
 * Convenience wrapper for metrics whose histogram buckets should scale with the
 * target CPU count.
 *
 * @param {Object} args
 * @param {number[]} args.bucketMultipliers
 * @param {string} args.columnName
 * @param {string} [args.sampleValueSql]
 * @param {{query: string, tableNamePlaceholder: string, tableKey: string}} [args.summaryQuery]
 * @returns {Object}
 */
const createCpuRelativeBucketedDeepDiveRow = ({
  bucketMultipliers,
  columnName,
  sampleValueSql,
  summaryQuery,
  ...row
}) =>
  createDeepDiveRow({
    ...row,
    columnName,
    summaryQuery:
      summaryQuery ??
      (sampleValueSql ? makeSummaryQuery({ sampleValueSql }) : undefined),
    histogramQuery: makeCpuRelativeEdgeHistogramQuery({
      bucketMultipliers,
      columnName,
      sampleValueSql,
    }),
  });

/**
 * Convenience wrapper for metrics whose histogram buckets are derived from a
 * dynamic hot-start threshold.
 *
 * @param {Object} args
 * @param {(number | null)[]} args.bucketMultipliers
 * @param {string} args.columnName
 * @param {string} args.hotStartSql
 * @param {string} [args.sampleValueSql]
 * @param {{query: string, tableNamePlaceholder: string, tableKey: string}} [args.summaryQuery]
 * @returns {Object}
 */
const createHotStartRelativeBucketedDeepDiveRow = ({
  bucketMultipliers,
  columnName,
  hotStartSql,
  sampleValueSql,
  summaryQuery,
  ...row
}) =>
  createDeepDiveRow({
    ...row,
    columnName,
    summaryQuery:
      summaryQuery ??
      (sampleValueSql ? makeSummaryQuery({ sampleValueSql }) : undefined),
    histogramQuery: makeHotStartRelativeEdgeHistogramQuery({
      bucketMultipliers,
      columnName,
      hotStartSql,
      sampleValueSql,
    }),
  });

/**
 * Convenience wrapper for percentage-style metrics that all share the same
 * evenly spaced 0-100 histogram layout.
 *
 * The current convention is 5-point buckets from 0-95, with all higher values
 * clamped into the final 95-100 bucket by the linear histogram helper. This
 * matches the existing histogram behavior while avoiding repeated boilerplate in
 * each percent-based metric declaration.
 *
 * @param {Object} args
 * @param {string} args.columnName
 * @returns {Object}
 */
const createPercentDeepDiveRow = ({ columnName, ...row }) =>
  createDeepDiveRow({
    ...row,
    columnName,
    histogramQuery: makeBoundedLinearHistogramQuery({
      columnName,
      minBucketStart: 0,
      maxBucketStart: 95,
      bucketWidth: 5,
    }),
  });

/**
 * Discovers the number of per-core CPU utilization columns in the timeline
 * table.
 *
 * Output columns:
 * - `core_count`: total number of `cpuN_percent` columns discovered
 * - `upper_bucket_start`: same numeric value, named for reuse by histogram
 *   queries that need an inclusive top bucket boundary
 *
 * This is kept as SQL rather than precomputed in JavaScript because the deep
 * dive runs against a renderer-side DuckDB table whose exact schema is only
 * known at query time.
 */
const CPU_UTILIZATION_UPPER_BOUND_SQL = sql`
  SELECT
    greatest(count(*), 1) AS core_count,
    greatest(count(*), 1) AS upper_bucket_start
  FROM duckdb_columns()
  WHERE table_name = regexp_extract('${TIMELINE_TABLE_PLACEHOLDER}', '([^".]+)"?$', 1)
    AND regexp_full_match(column_name, '^cpu[0-9]+_percent$')
`;
const CPU_RELATIVE_LOAD_BUCKET_MULTIPLIERS = [
  0, 0.25, 0.5, 0.55, 0.6, 0.65, 0.7, 1, 1.25, 1.5,
];
const RUNNING_PROCESS_BUCKET_MULTIPLIERS = [
  0, 0.25, 0.5, 0.75, 1, 1.25, 1.5, 2, 2.5, 3,
];
const PERCENT_BUCKET_STARTS = [
  ...Array.from({ length: 20 }, (_, i) => i * 5),
  100,
];
const BLOCKED_PROCESS_BUCKET_STARTS = [0, 1, 2, 4, 8, 16, 32, 64, 128, 256];
const TOTAL_COUNT_BUCKET_MULTIPLIERS = [
  0, 0.125, 0.25, 0.375, 0.5, 0.75, 1, 1.5, 2, 3,
];

/**
 * Builds a total-count hot-start expression from a scaled floor and per-core
 * allowance.
 *
 * The threshold is the larger of:
 * - a scaled floor, which prevents small machines from getting unrealistically
 *   low thresholds
 * - `baseCount` plus a scaled per-core allowance, which lets larger machines
 *   scale with core count
 *
 * @param {Object} args
 * @param {number} args.baseCount
 * @param {number} args.floorMax
 * @param {number} args.floorMin
 * @param {number} args.perCoreMax
 * @param {number} args.perCoreMin
 * @returns {string}
 */
const makeTotalCountHotStartSql = ({
  baseCount,
  floorMax,
  floorMin,
  perCoreMax,
  perCoreMin,
}) => sql`
  greatest(
    ${floorMin} + (${floorMax} - ${floorMin}) * scale,
    ${baseCount} + (${perCoreMin} + (${perCoreMax} - ${perCoreMin}) * scale) * core_count
  )
`;
const TOTAL_PROCESS_COUNT_HOT_START_SQL = makeTotalCountHotStartSql({
  baseCount: 80,
  floorMin: 120,
  floorMax: 500,
  perCoreMin: 6,
  perCoreMax: 20,
});
const TOTAL_THREAD_COUNT_HOT_START_SQL = makeTotalCountHotStartSql({
  baseCount: 300,
  floorMin: 600,
  floorMax: 2000,
  perCoreMin: 60,
  perCoreMax: 60,
});
const CONTEXT_SWITCH_RATE_PER_CORE_BUCKET_MULTIPLIERS = [
  0, 50, 125, 250, 500, 1250, 2500, 5000, 12500, 25000,
];
const IRQ_RATE_PER_CORE_BUCKET_MULTIPLIERS = [
  0, 25, 50, 125, 250, 500, 1250, 2500, 5000, 12500,
];
const LARGE_RATE_BUCKET_STARTS = [
  0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384,
  32768, 65536, 131072, 262144, 524288,
];
const DISK_IOPS_BUCKET_STARTS = [
  0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384,
];
const DISK_BANDWIDTH_BUCKET_STARTS = [
  0, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864,
  268435456, 1073741824,
];
const MAJOR_FAULT_BUCKET_STARTS = [
  0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192,
];
/**
 * Produces a SQL expression that sums a dynamic set of timeline columns
 * selected by regex.
 *
 * The matching columns are first materialized row-locally with
 * `list_value(unpack(COLUMNS(...)))`, then converted to DOUBLE and summed.
 * Null and non-numeric entries contribute zero; rows whose matching columns are
 * all null are filtered later by the shared query templates after the value has
 * been staged in a `samples` CTE.
 *
 * @param {string} pattern
 * @returns {string}
 */
const sumMatchingColumnsSql = (pattern) =>
  `coalesce(list_sum(list_transform(list_value(unpack(COLUMNS('${pattern}'))), x -> coalesce(try_cast(x AS DOUBLE), 0))), 0)`;
const DISK_READ_IOPS_SQL = sumMatchingColumnsSql('^read_iops_.*$');
const DISK_WRITE_IOPS_SQL = sumMatchingColumnsSql('^write_iops_.*$');
const DISK_READ_BPS_SQL = sumMatchingColumnsSql('^read_bps_.*$');
const DISK_WRITE_BPS_SQL = sumMatchingColumnsSql('^write_bps_.*$');
const NETWORK_RX_BPS_SQL = sumMatchingColumnsSql('^rx_bps_.*$');
const NETWORK_TX_BPS_SQL = sumMatchingColumnsSql('^tx_bps_.*$');
const SWAP_OCCUPANCY_PERCENT_SQL =
  'CASE ' +
  'WHEN try_cast("swap_total_kb" AS DOUBLE) > 0 ' +
  'THEN try_cast("swap_used_kb" AS DOUBLE) * 100.0 / try_cast("swap_total_kb" AS DOUBLE) ' +
  'ELSE NULL ' +
  'END';

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
const recipe = {
  name: 'system_utilization',
  title: 'System Utilization',
  version: '1.0',
  api_version: '1.0.0',
  status: 'preview',
  description:
    'The System Utilization recipe shows how CPU, memory, disk, and network resources are used while your workload runs. It helps you spot saturated system resources, understand utilization trends over time, and correlate workload behavior with broader system activity.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_SYSUTIL_TIMELINE.name,
          version: TOOL_SYSUTIL_TIMELINE.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
    {
      appliesTo: [{ architecture: 'x86_64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_SYSUTIL_TIMELINE.name,
          version: TOOL_SYSUTIL_TIMELINE.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'interval',
      required: false,
      label: 'Interval',
      description: 'Sampling interval in seconds.',
      config: {
        type: 'input',
        defaultValue: '1.0',
      },
    },
    {
      id: 'thread_scan_interval',
      required: false,
      label: 'Thread Scan Interval',
      description: 'Thread scan interval in seconds (defaults to interval).',
      config: {
        type: 'input',
        defaultValue: '1',
      },
    },
  ],
  readyStages: [
    {
      name: 'Checking recipe is ready',
      description:
        'Check dependencies and parameters specified for the System Utilization recipe.',
      exec: readySystemUtilization,
    },
  ],
  runStages: [
    {
      name: 'Collecting utilization data',
      description:
        'Run the System Utilization recipe to capture utilization data.',
      exec: runSystemUtilization,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderSystemUtilization,
    },
  ],
};

/**
 * generateSysutilTimelineConfig generates a ToolConfigurationsArg for the
 * sysutil-timeline tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function generateSysutilTimelineConfig(workload, params) {
  return {
    toolConfigs: [
      {
        name: TOOL_SYSUTIL_TIMELINE.name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readySystemUtilization(context) {
  const workload = context.getWorkload();
  const params = {
    interval: context.getParameter('interval'),
    thread_scan_interval: context.getParameter('thread_scan_interval'),
  };

  const tools = generateSysutilTimelineConfig(workload, params);
  const toolResponses = context.probeTools(tools);

  const allAdvice = collectToolAdvice(tools, toolResponses);

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runSystemUtilization(context) {
  const workload = context.getWorkload();
  const params = {
    interval: context.getParameter('interval'),
    thread_scan_interval: context.getParameter('thread_scan_interval'),
  };

  context.runTools(generateSysutilTimelineConfig(workload, params));
}

const SUMMARY_TIMELINE_GROUP_NAMES = [
  'per_core_cpu',
  'memory_kb',
  'disk_bw',
  'net_bw',
];

const makeTimelineDataSourceTables = (groupNames) =>
  Object.fromEntries(
    groupNames.map((groupName) => [groupName, TIMELINE_TABLE_DATA_SOURCE]),
  );

const pickTimelineGroups = (groups, groupNames) =>
  Object.fromEntries(
    groupNames.map((groupName) => [groupName, groups[groupName]]),
  );

const SUMMARY_TIMELINE_GROUPS = {
  per_core_cpu: {
    title: 'Per-Core CPU Usage',
    type: 'heatmap',
    index: 1,
    description:
      'Core N Use (%):\n' +
      'CPU busy time for each individual core, shown separately. 100% means this core is fully busy.',
    config: {
      xAxisTitle: 'Time (s)',
      yAxisTitle: 'CPU Utilization',
      yAxisUnit: 'percent',
      customQuery: {
        tableNamePlaceholder: '{table}',
        query:
          'SELECT ' +
          'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
          "try_cast(COLUMNS('^cpu\\d+_percent$') AS DOUBLE) " +
          'FROM {table} ORDER BY "uptime_s"',
      },
      series: [
        {
          type: 'pattern',
          xColumn: 'uptime_s',
          yColumn: { pattern: '^cpu(\\d+)_percent$' },
          name: { template: 'Core {y1}' },
        },
      ],
    },
  },
  memory_kb: {
    title: 'Memory Usage',
    type: 'line',
    index: 4,
    description:
      'Mem Total:\n' +
      'Total memory installed on the system.\n\n' +
      'Mem Available:\n' +
      'Estimated available memory, excluding swap memory. Includes free memory and reclaimable cache / buffers.\n\n' +
      'Mem Used:\n' +
      'Memory in use, calculated as total memory minus available memory.',
    config: {
      xAxisTitle: 'Time (s)',
      yAxisTitle: 'kB',
      yAxisUnit: 'kibibyte',
      yAxisDisplayRange: {
        min: 0,
      },
      customQuery: {
        tableNamePlaceholder: '{table}',
        query:
          'SELECT ' +
          'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
          'try_cast("mem_total_kb" AS DOUBLE) AS "mem_total_kb", ' +
          'try_cast("mem_available_kb" AS DOUBLE) AS "mem_available_kb", ' +
          'try_cast("mem_used_kb" AS DOUBLE) AS "mem_used_kb" ' +
          'FROM {table} ORDER BY "uptime_s"',
      },
      series: [
        {
          type: 'single',
          name: 'Mem Total',
          xColumn: 'uptime_s',
          yColumn: 'mem_total_kb',
        },
        {
          type: 'single',
          name: 'Mem Available',
          xColumn: 'uptime_s',
          yColumn: 'mem_available_kb',
        },
        {
          type: 'single',
          name: 'Mem Used',
          xColumn: 'uptime_s',
          yColumn: 'mem_used_kb',
        },
      ],
    },
  },
  disk_bw: {
    title: 'Disk Throughput',
    type: 'heatmap',
    index: 12,
    description:
      'Read:\n' +
      'Read throughput for each storage device, shown separately.\n\n' +
      'Write:\n' +
      'Write throughput for each storage device, shown separately.',
    config: {
      xAxisTitle: 'Time (s)',
      yAxisTitle: 'Bytes/s',
      yAxisUnit: 'B/s',
      customQuery: {
        tableNamePlaceholder: '{table}',
        query:
          'SELECT ' +
          'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
          "try_cast(COLUMNS('^read_bps_.*$') AS DOUBLE), " +
          "try_cast(COLUMNS('^write_bps_.*$') AS DOUBLE) " +
          'FROM {table} ORDER BY "uptime_s"',
      },
      series: [
        {
          type: 'pattern',
          xColumn: 'uptime_s',
          yColumn: { pattern: '^read_bps_(.+)$' },
          name: { template: 'Read {y1}' },
        },
        {
          type: 'pattern',
          xColumn: 'uptime_s',
          yColumn: { pattern: '^write_bps_(.+)$' },
          name: { template: 'Write {y1}' },
        },
      ],
    },
  },
  net_bw: {
    title: 'Network Throughput',
    type: 'heatmap',
    index: 13,
    description:
      'RX:\n' +
      'Receive throughput for each network interface, shown separately.\n\n' +
      'TX:\n' +
      'Transmit throughput for each network interface, shown separately.',
    config: {
      xAxisTitle: 'Time (s)',
      yAxisTitle: 'Bytes/s',
      yAxisUnit: 'B/s',
      customQuery: {
        tableNamePlaceholder: '{table}',
        query:
          'SELECT ' +
          'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
          "try_cast(COLUMNS('^rx_bps_.*$') AS DOUBLE), " +
          "try_cast(COLUMNS('^tx_bps_.*$') AS DOUBLE) " +
          'FROM {table} ORDER BY "uptime_s"',
      },
      series: [
        {
          type: 'pattern',
          xColumn: 'uptime_s',
          yColumn: { pattern: '^rx_bps_(.+)$' },
          name: { template: 'RX {y1}' },
        },
        {
          type: 'pattern',
          xColumn: 'uptime_s',
          yColumn: { pattern: '^tx_bps_(.+)$' },
          name: { template: 'TX {y1}' },
        },
      ],
    },
  },
};

const systemUtilizationSummaryTimelineConfig = {
  xAxisUnit: 's',
  data_source: {
    tables: makeTimelineDataSourceTables(SUMMARY_TIMELINE_GROUP_NAMES),
  },
  groups: pickTimelineGroups(
    SUMMARY_TIMELINE_GROUPS,
    SUMMARY_TIMELINE_GROUP_NAMES,
  ),
};

const systemUtilizationSummaryConfig = {
  data_source: {
    tables: {
      timeline: TIMELINE_TABLE_DATA_SOURCE,
      flat_table: TIMELINE_TABLE_DATA_SOURCE,
      ...makeTimelineDataSourceTables(SUMMARY_TIMELINE_GROUP_NAMES),
    },
  },
  timeline: systemUtilizationSummaryTimelineConfig,
  queries: {
    cpu: {
      utilization: {
        title: 'CPU Utilization',
        description: [
          'CPU utilization is the mean percentage of CPU time spent on non-idle, non-iowait work during a specific time interval.',
        ],
        unit: '%',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: cpu_total_percent
          -- Output: sample_count, cpu_utilization_percent, p95_cpu_utilization_percent
          SELECT
            'cpu_utilization' AS query_id,
            count(*) AS sample_count,
            round(avg("cpu_total_percent"), 2) AS cpu_utilization_percent,
            round(quantile_cont("cpu_total_percent", 0.95), 2) AS p95_cpu_utilization_percent
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          WHERE "cpu_total_percent" IS NOT NULL
        `),
      },
      active_cores: {
        title: 'Active Cores',
        description: [
          'Active cores is the amount of cores that are actively performing work during a specific time interval.',
          'A core is considered active if its utilization is above 5% for at least 20% of the samples in the time interval.',
        ],
        unit: '',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: per-core utilization columns (cpu[N]_percent)
          WITH per_core_utilization AS (
            SELECT
              cpu,
              count(*) FILTER (WHERE utilization > 5.0) AS active_samples,
              count(utilization) AS total_samples
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            UNPIVOT (
              utilization FOR cpu IN (COLUMNS('^cpu[0-9]+_percent$'))
            )
            GROUP BY cpu
          )
          -- Output: sample_count, cpu_count, active_cores
          SELECT
            'cpu_active_cores' AS query_id,
            coalesce(max(total_samples), 0) AS sample_count,
            CAST(count(*) AS INTEGER) AS cpu_count,
            CAST(count(*) FILTER (
              WHERE total_samples > 0
                AND active_samples::DOUBLE / total_samples >= 0.20
            ) AS INTEGER) AS active_cores
          FROM per_core_utilization
        `),
      },
      runqueue_load: {
        title: 'Runqueue Load',
        description: [
          'Runqueue load compares runnable tasks to the number of available CPU cores.',
          'Sustained load means the runqueue length exceeded the CPU count for at least 20% of samples.',
        ],
        unit: '',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: per-core utilization columns (cpu[N]_percent), uptime_s, procs_running, cpu_total_percent
          WITH per_sample_load AS (
            SELECT
              uptime_s,
              "procs_running" AS runqueue_length,
              "cpu_total_percent",
              count(utilization) AS cpu_count,
              count(*) FILTER (WHERE utilization > 5.0) AS active_cores
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            UNPIVOT (
              utilization FOR cpu IN (COLUMNS('^cpu[0-9]+_percent$'))
            )
            WHERE "uptime_s" IS NOT NULL
              AND "procs_running" IS NOT NULL
            GROUP BY uptime_s, runqueue_length, cpu_total_percent
          )
          -- Output: sample_count, cpu_count, avg/p95/max_runqueue_length, runqueue_overloaded_sample_count, runqueue_overloaded_sample_ratio, avg_cpu_percent, avg_active_cores
          SELECT
            'cpu_runqueue_load' AS query_id,
            count(*) AS sample_count,
            CAST(max(cpu_count) AS INTEGER) AS cpu_count,
            round(avg(runqueue_length), 2) AS avg_runqueue_length,
            round(quantile_cont(runqueue_length, 0.95), 2) AS p95_runqueue_length,
            CAST(max(runqueue_length) AS INTEGER) AS max_runqueue_length,
            CAST(count(*) FILTER (WHERE runqueue_length > cpu_count) AS INTEGER) AS runqueue_overloaded_sample_count,
            round(count(*) FILTER (WHERE runqueue_length > cpu_count)::DOUBLE / nullif(count(*), 0), 4) AS runqueue_overloaded_sample_ratio,
            round(avg(cpu_total_percent), 2) AS avg_cpu_percent,
            round(avg(active_cores), 2) AS avg_active_cores
          FROM per_sample_load
        `),
      },
      hot_core: {
        title: 'Hot Core',
        description: [
          'A hot core can reveal CPU load even when total CPU utilization is not high.',
          'A core is considered hot when its p95 utilization is at or above 80%.',
        ],
        unit: '',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: per-core utilization columns (cpu[N]_percent), cpu_total_percent
          WITH per_core_utilization AS (
            SELECT
              cpu,
              quantile_cont(utilization, 0.95) AS p95_core_percent,
              max(utilization) AS max_core_percent
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            UNPIVOT (
              utilization FOR cpu IN (COLUMNS('^cpu[0-9]+_percent$'))
            )
            GROUP BY cpu
          ),
          totals AS (
            SELECT
              count(*) AS sample_count,
              avg("cpu_total_percent") AS avg_cpu_percent
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            WHERE "cpu_total_percent" IS NOT NULL
          )
          -- Output: sample_count, hot_cores, avg_cpu_percent, hottest_core_p95_percent, hottest_core_max_percent
          SELECT
            'cpu_hot_core' AS query_id,
            sample_count,
            CAST(count(*) FILTER (WHERE p95_core_percent >= 80) AS INTEGER) AS hot_cores,
            round(avg_cpu_percent, 2) AS avg_cpu_percent,
            round(max(p95_core_percent), 2) AS hottest_core_p95_percent,
            round(max(max_core_percent), 2) AS hottest_core_max_percent
          FROM per_core_utilization, totals
          GROUP BY sample_count, avg_cpu_percent
        `),
      },
    },
    memory: {
      utilization: {
        title: 'Memory Utilization',
        description: [
          'Memory utilization is the mean percentage of memory used by the system during a specific time interval.',
        ],
        unit: '%',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: mem_used_percent
          -- Output: sample_count, memory_utilization_percent
          SELECT
            'memory_utilization' AS query_id,
            count(*) AS sample_count,
            round(avg("mem_used_percent"), 2) AS memory_utilization_percent
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          WHERE "mem_used_percent" IS NOT NULL
        `),
      },
      peak: {
        title: 'Peak Memory Utilization',
        description: [
          'Peak memory utilization is the maximum percentage of memory used by the system during a specific time interval.',
        ],
        unit: '%',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: mem_used_percent
          -- Output: sample_count, peak_memory_utilization_percent
          SELECT
            'memory_peak' AS query_id,
            count(*) AS sample_count,
            round(max("mem_used_percent"), 2) AS peak_memory_utilization_percent
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          WHERE "mem_used_percent" IS NOT NULL
        `),
      },
      capacity_load: {
        title: 'Memory Capacity Load',
        description: [
          'High memory use based on available memory can indicate elevated load.',
          'The metric is based on total memory minus available memory, so reclaimable cache is accounted for.',
        ],
        unit: '%',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: mem_used_percent
          -- Output: sample_count, avg_memory_percent, p95_memory_percent
          SELECT
            'memory_capacity_load' AS query_id,
            count(*) AS sample_count,
            round(avg("mem_used_percent"), 2) AS avg_memory_percent,
            round(quantile_cont("mem_used_percent", 0.95), 2) AS p95_memory_percent
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          WHERE "mem_used_percent" IS NOT NULL
        `),
      },
      swap_occupancy: {
        title: 'Swap Occupancy',
        description: [
          'Swap occupancy shows whether swap space is in use.',
          'Static swap use can be historical, so this is context only unless swap growth or major faults also show elevated load.',
        ],
        unit: 'kB',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: swap_used_kb, swap_total_kb
          -- Output: sample_count, max_swap_used_kb, max_swap_ratio
          SELECT
            'memory_swap_occupancy' AS query_id,
            count(*) AS sample_count,
            round(coalesce(max("swap_used_kb"), 0), 2) AS max_swap_used_kb,
            round(coalesce(max("swap_used_kb" / nullif("swap_total_kb", 0)), 0), 4) AS max_swap_ratio
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          WHERE "swap_used_kb" IS NOT NULL
        `),
      },
      major_faults: {
        title: 'Major Faults',
        description: [
          'Major page faults indicate pages had to be fetched from disk or swap before execution could continue.',
          'Major fault load is reported when major faults average at least 10/s and appear in at least 20% of samples.',
        ],
        unit: '/s',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: pgmajfaults_per_s
          -- Output: sample_count, avg_major_faults_per_s, p95_major_faults_per_s, major_fault_sample_count, major_fault_sample_ratio
          SELECT
            'memory_major_faults' AS query_id,
            count(*) AS sample_count,
            round(avg("pgmajfaults_per_s"), 2) AS avg_major_faults_per_s,
            round(quantile_cont("pgmajfaults_per_s", 0.95), 2) AS p95_major_faults_per_s,
            CAST(sum(("pgmajfaults_per_s" > 0)::INTEGER) AS INTEGER) AS major_fault_sample_count,
            round(avg(("pgmajfaults_per_s" > 0)::INTEGER), 4) AS major_fault_sample_ratio
          FROM ${TIMELINE_TABLE_PLACEHOLDER}
          WHERE "pgmajfaults_per_s" IS NOT NULL
        `),
      },
    },
    disk: {
      usage: {
        title: 'Disk Usage',
        description: [
          'Estimated total bytes read and written across all storage devices.',
        ],
        unit: 'bytes',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: read_bps_* and write_bps_* columns, uptime_s
          WITH disk_rates AS (
            SELECT
              uptime_s,
              coalesce(sum(bps) FILTER (WHERE metric LIKE 'read_bps_%'), 0) AS read_bps,
              coalesce(sum(bps) FILTER (WHERE metric LIKE 'write_bps_%'), 0) AS write_bps
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            UNPIVOT (
              bps FOR metric IN (COLUMNS('^(read|write)_bps_.*$'))
            )
            WHERE "uptime_s" IS NOT NULL
            GROUP BY uptime_s
          ),
          elapsed_disk_rates AS (
            SELECT
              read_bps,
              write_bps,
              coalesce(uptime_s - lag(uptime_s) OVER (ORDER BY uptime_s), 0) AS elapsed_s
            FROM disk_rates
          )
          -- Output: sample_count, total_read_bytes, total_write_bytes
          SELECT
            'disk_total_activity' AS query_id,
            count(*) AS sample_count,
            CAST(round(coalesce(sum(read_bps * elapsed_s) FILTER (WHERE elapsed_s >= 0), 0), 0) AS BIGINT) AS total_read_bytes,
            CAST(round(coalesce(sum(write_bps * elapsed_s) FILTER (WHERE elapsed_s >= 0), 0), 0) AS BIGINT) AS total_write_bytes
          FROM elapsed_disk_rates
        `),
      },
      iops: {
        title: 'Disk IOPS',
        description: [
          'Average and p95 operations per second across all storage devices.',
        ],
        unit: 'IOPS',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: read_iops_* and write_iops_* columns, uptime_s
          WITH disk_rates AS (
            SELECT
              uptime_s,
              coalesce(sum(iops) FILTER (WHERE metric LIKE 'read_iops_%'), 0) AS read_iops,
              coalesce(sum(iops) FILTER (WHERE metric LIKE 'write_iops_%'), 0) AS write_iops
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            UNPIVOT (
              iops FOR metric IN (COLUMNS('^(read|write)_iops_.*$'))
            )
            WHERE "uptime_s" IS NOT NULL
            GROUP BY uptime_s
          )
          -- Output: sample_count, avg_read_iops, avg_write_iops, p95_read_iops, p95_write_iops
          SELECT
            'disk_iops' AS query_id,
            count(*) AS sample_count,
            round(avg(read_iops), 2) AS avg_read_iops,
            round(avg(write_iops), 2) AS avg_write_iops,
            round(quantile_cont(read_iops, 0.95), 2) AS p95_read_iops,
            round(quantile_cont(write_iops, 0.95), 2) AS p95_write_iops
          FROM disk_rates
        `),
      },
      average_io_size: {
        title: 'Average Disk I/O Size',
        description: [
          'Approximate bytes per operation across all storage devices.',
        ],
        unit: 'bytes/op',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: read_bps_*, write_bps_*, read_iops_*, write_iops_* columns, uptime_s
          WITH disk_rates AS (
            SELECT
              uptime_s,
              coalesce(sum(value) FILTER (WHERE metric LIKE 'read_bps_%'), 0) AS read_bps,
              coalesce(sum(value) FILTER (WHERE metric LIKE 'write_bps_%'), 0) AS write_bps,
              coalesce(sum(value) FILTER (WHERE metric LIKE 'read_iops_%'), 0) AS read_iops,
              coalesce(sum(value) FILTER (WHERE metric LIKE 'write_iops_%'), 0) AS write_iops
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            UNPIVOT (
              value FOR metric IN (COLUMNS('^(read|write)_(bps|iops)_.*$'))
            )
            WHERE "uptime_s" IS NOT NULL
            GROUP BY uptime_s
          )
          -- Output: sample_count, avg_read_io_size_bytes, avg_write_io_size_bytes, avg_read_iops, avg_write_iops
          SELECT
            'disk_average_io_size' AS query_id,
            count(*) AS sample_count,
            round(avg(read_bps) / nullif(avg(read_iops), 0), 2) AS avg_read_io_size_bytes,
            round(avg(write_bps) / nullif(avg(write_iops), 0), 2) AS avg_write_io_size_bytes,
            round(avg(read_iops), 2) AS avg_read_iops,
            round(avg(write_iops), 2) AS avg_write_iops
          FROM disk_rates
        `),
      },
    },
    network: {
      usage: {
        title: 'Network Usage',
        description: [
          'Estimated total bytes received and transmitted across all network interfaces.',
        ],
        unit: 'bytes',
        summaryQuery: makeTimelineQueryConfig(`
          -- Input: rx_bps_* and tx_bps_* columns, uptime_s
          WITH network_rates AS (
            SELECT
              uptime_s,
              coalesce(sum(bps) FILTER (WHERE metric LIKE 'rx_bps_%'), 0) AS received_bps,
              coalesce(sum(bps) FILTER (WHERE metric LIKE 'tx_bps_%'), 0) AS transmitted_bps
            FROM ${TIMELINE_TABLE_PLACEHOLDER}
            UNPIVOT (
              bps FOR metric IN (COLUMNS('^(rx|tx)_bps_.*$'))
            )
            WHERE "uptime_s" IS NOT NULL
            GROUP BY uptime_s
          ),
          elapsed_network_rates AS (
            SELECT
              received_bps,
              transmitted_bps,
              coalesce(uptime_s - lag(uptime_s) OVER (ORDER BY uptime_s), 0) AS elapsed_s
            FROM network_rates
          )
          -- Output: sample_count, total_received_bytes, total_transmitted_bytes
          SELECT
            'network_total_activity' AS query_id,
            count(*) AS sample_count,
            CAST(round(coalesce(sum(received_bps * elapsed_s) FILTER (WHERE elapsed_s >= 0), 0), 0) AS BIGINT) AS total_received_bytes,
            CAST(round(coalesce(sum(transmitted_bps * elapsed_s) FILTER (WHERE elapsed_s >= 0), 0), 0) AS BIGINT) AS total_transmitted_bytes
          FROM elapsed_network_rates
        `),
      },
    },
  },
  statusLabels: {
    cpu: {
      label: 'CPU pressure',
      severity: 'warning',
    },
    memory: {
      label: 'Memory pressure',
      severity: 'warning',
    },
    disk: {
      label: 'Storage pressure',
      severity: 'warning',
    },
    network: {
      label: 'Network pressure',
      severity: 'warning',
    },
  },
  deepDive: [
    {
      key: 'cpu',
      label: 'CPU',
      metrics: [
        createDeepDiveRow({
          key: 'cpu-utilization',
          label: 'Effective CPU utilization',
          description:
            'Total CPU use as the equivalent number of fully busy cores. Use this metric to understand how much CPU capacity the workload used.',
          summaryValueUnit: 'cores',
          columnName: 'cpu_total_percent',
          summaryQuery: makeSummaryQuery({
            deriveUpperBoundSql: CPU_UTILIZATION_UPPER_BOUND_SQL,
            sampleValueSql:
              'try_cast("cpu_total_percent" AS DOUBLE) * core_count / 100.0',
          }),
          histogramQuery: makeBoundedLinearHistogramQuery({
            bucketWidth: 1,
            deriveUpperBoundSql: CPU_UTILIZATION_UPPER_BOUND_SQL,
            sampleValueSql:
              'try_cast("cpu_total_percent" AS DOUBLE) * core_count / 100.0',
          }),
          triggersStatusLabelFor: 'cpu',
          timelineGroups: ['total_cpu_mem', 'per_core_cpu'],
        }),
        createDeepDiveRow({
          key: 'cpu-saturation',
          label: 'CPU saturation',
          description:
            'The number of CPU cores at or above 80% utilization. High values can indicate sustained CPU pressure.',
          summaryValueUnit: 'cores',
          columnName: 'saturated_cores',
          summaryQuery: makeCpuSaturationSummaryQuery(),
          histogramQuery: makeCpuSaturationHistogramQuery(),
          triggersStatusLabelFor: 'cpu',
          timelineGroups: ['cpu_saturation', 'per_core_cpu'],
        }),
        createCpuRelativeBucketedDeepDiveRow({
          key: 'load-average-1m',
          label: 'Load average (1m)',
          description:
            'The average number of tasks running or waiting for CPU time over 1 minute. Values near or above the CPU core count can indicate high CPU demand.',
          summaryValueUnit: 'load',
          columnName: 'load1',
          bucketMultipliers: CPU_RELATIVE_LOAD_BUCKET_MULTIPLIERS,
          triggersStatusLabelFor: 'cpu',
          timelineGroups: ['load_runqueue'],
        }),
        createPercentDeepDiveRow({
          key: 'io-wait',
          label: 'IO wait (%)',
          description:
            'The percentage of CPU time spent waiting for I/O operations to complete. High values can indicate that disk or other I/O activity is limiting execution speed.',
          summaryValueUnit: '%',
          columnName: 'iowait_percent',
          timelineGroups: ['iowait'],
        }),
      ],
    },
    {
      key: 'memory',
      label: 'Memory',
      metrics: [
        createPercentDeepDiveRow({
          key: 'memory-used-percent',
          label: 'Memory used (%)',
          description:
            'Memory in use as a percentage of total memory. High values can indicate that the system has limited spare memory.',
          summaryValueUnit: '%',
          columnName: 'mem_used_percent',
          triggersStatusLabelFor: 'memory',
          timelineGroups: ['total_cpu_mem', 'memory_kb'],
        }),
        createBucketedDeepDiveRow({
          key: 'swap-used',
          label: 'Swap used',
          description:
            'How much swap space is in use. Swap usage can indicate that the system has limited physical memory available.',
          summaryValueUnit: 'kB',
          columnName: 'swap_used_kb',
          bucketStarts: DISK_BANDWIDTH_BUCKET_STARTS,
          triggersStatusLabelFor: 'memory',
          timelineGroups: ['swap_kb'],
        }),
        createBucketedDeepDiveRow({
          key: 'swap-occupancy',
          label: 'Swap occupancy (%)',
          description:
            'How much of the configured swap space is in use as a percentage of the total configured swap. Swap usage can indicate that the system has limited physical memory available.',
          summaryValueUnit: '%',
          columnName: 'swap_occupancy_percent',
          sampleValueSql: SWAP_OCCUPANCY_PERCENT_SQL,
          bucketStarts: PERCENT_BUCKET_STARTS,
          triggersStatusLabelFor: 'memory',
          timelineGroups: ['swap_kb'],
        }),
        createPercentDeepDiveRow({
          key: 'numa-remote-memory',
          label: 'NUMA remote memory (%)',
          description:
            'The percentage of memory pages placed on a different NUMA node from the running process. High values can indicate poor memory locality, which can make memory access slower.',
          summaryValueUnit: '%',
          columnName: 'numa_remote_percent',
          triggersStatusLabelFor: 'memory',
          timelineGroups: ['numa_locality'],
        }),
        ...[
          {
            key: 'numa-preferred-node-misses',
            label: 'NUMA preferred-node misses / s',
            description:
              'How often memory pages were allocated from a NUMA node other than the preferred node. High values can indicate poor memory locality, which can make memory access slower.',
            summaryValueUnit: 'pages/s',
            columnName: 'numa_miss_per_s',
            bucketStarts: LARGE_RATE_BUCKET_STARTS,
            triggersStatusLabelFor: 'memory',
            timelineGroups: ['numa_events'],
          },
          {
            key: 'numa-remote-node-pages',
            label: 'NUMA remote-node pages / s',
            description:
              'How often memory pages were allocated from a NUMA node different from the one running the process. High values can indicate poor memory locality, which can make memory access slower.',
            summaryValueUnit: 'pages/s',
            columnName: 'numa_other_node_per_s',
            bucketStarts: LARGE_RATE_BUCKET_STARTS,
            triggersStatusLabelFor: 'memory',
            timelineGroups: ['numa_events'],
          },
          {
            key: 'page-faults',
            label: 'Page faults / s',
            description:
              'How often the system resolved memory page faults, including pages loaded from disk or swap. High values can indicate frequent access to new memory pages or memory pressure',
            summaryValueUnit: 'faults/s',
            columnName: 'page_faults_per_s',
            bucketStarts: LARGE_RATE_BUCKET_STARTS,
            triggersStatusLabelFor: 'memory',
            timelineGroups: ['procs_threads_ctx_irq_faults'],
          },
          {
            key: 'major-page-faults',
            label: 'Major page faults / s',
            description:
              'The rate of major page faults, where memory pages must be loaded from disk or swap. High values can indicate memory pressure.',
            summaryValueUnit: 'faults/s',
            columnName: 'pgmajfaults_per_s',
            bucketStarts: MAJOR_FAULT_BUCKET_STARTS,
            triggersStatusLabelFor: 'memory',
            timelineGroups: ['procs_threads_ctx_irq_faults'],
          },
        ].map(createBucketedDeepDiveRow),
      ],
    },
    {
      key: 'io-devices',
      label: 'I/O Devices',
      metrics: [
        {
          key: 'disk-read-iops',
          label: 'Disk read IOPS',
          description:
            'Disk read operations per second across storage devices. High values can indicate I/O pressure.',
          summaryValueUnit: 'IOPS',
          columnName: 'read_iops',
          sampleValueSql: DISK_READ_IOPS_SQL,
          bucketStarts: DISK_IOPS_BUCKET_STARTS,
          triggersStatusLabelFor: 'disk',
          timelineGroups: ['disk_iops'],
        },
        {
          key: 'disk-write-iops',
          label: 'Disk write IOPS',
          description:
            'Disk write operations per second across storage devices. High values can indicate I/O pressure.',
          summaryValueUnit: 'IOPS',
          columnName: 'write_iops',
          sampleValueSql: DISK_WRITE_IOPS_SQL,
          bucketStarts: DISK_IOPS_BUCKET_STARTS,
          triggersStatusLabelFor: 'disk',
          timelineGroups: ['disk_iops'],
        },
        {
          key: 'disk-read-bandwidth',
          label: 'Disk read bandwidth',
          description:
            'The rate of data read from storage devices. High values can indicate I/O pressure.',
          summaryValueUnit: 'bytes/s',
          columnName: 'read_bps',
          sampleValueSql: DISK_READ_BPS_SQL,
          bucketStarts: DISK_BANDWIDTH_BUCKET_STARTS,
          triggersStatusLabelFor: 'disk',
          timelineGroups: ['disk_bw'],
        },
        {
          key: 'disk-write-bandwidth',
          label: 'Disk write bandwidth',
          description:
            'The rate of data written to storage devices. High values can indicate I/O pressure.',
          summaryValueUnit: 'bytes/s',
          columnName: 'write_bps',
          sampleValueSql: DISK_WRITE_BPS_SQL,
          bucketStarts: DISK_BANDWIDTH_BUCKET_STARTS,
          triggersStatusLabelFor: 'disk',
          timelineGroups: ['disk_bw'],
        },
        {
          key: 'network-rx-bandwidth',
          label: 'Network RX bandwidth',
          description:
            'The rate of data received across network interfaces. High values can indicate heavy inbound network traffic.',
          summaryValueUnit: 'bytes/s',
          columnName: 'rx_bps',
          sampleValueSql: NETWORK_RX_BPS_SQL,
          bucketStarts: DISK_BANDWIDTH_BUCKET_STARTS,
          triggersStatusLabelFor: 'network',
          timelineGroups: ['net_bw'],
        },
        {
          key: 'network-tx-bandwidth',
          label: 'Network TX bandwidth',
          description:
            'The rate of data transmitted across network interfaces. High values can indicate heavy outbound network traffic.',
          summaryValueUnit: 'bytes/s',
          columnName: 'tx_bps',
          sampleValueSql: NETWORK_TX_BPS_SQL,
          bucketStarts: DISK_BANDWIDTH_BUCKET_STARTS,
          triggersStatusLabelFor: 'network',
          timelineGroups: ['net_bw'],
        },
      ].map(createBucketedDeepDiveRow),
    },
    {
      key: 'kernel',
      label: 'Kernel',
      metrics: [
        createCpuRelativeBucketedDeepDiveRow({
          key: 'running-processes',
          label: 'Running processes',
          description:
            'The number of processes running or waiting to run on the CPU. High values can indicate high CPU demand.',
          summaryValueUnit: 'processes',
          columnName: 'procs_running',
          sampleValueSql: 'try_cast("procs_running" AS DOUBLE)',
          bucketMultipliers: RUNNING_PROCESS_BUCKET_MULTIPLIERS,
          timelineGroups: ['load_runqueue'],
        }),
        ...[
          {
            key: 'blocked-processes',
            label: 'Blocked processes',
            description:
              'The number of processes waiting for I/O or another system event. High values can indicate that work is stalled rather than running.',
            summaryValueUnit: 'processes',
            columnName: 'procs_blocked',
            sampleValueSql: 'try_cast("procs_blocked" AS DOUBLE)',
            bucketStarts: BLOCKED_PROCESS_BUCKET_STARTS,
            timelineGroups: ['load_runqueue'],
          },
        ].map(createBucketedDeepDiveRow),
        createHotStartRelativeBucketedDeepDiveRow({
          key: 'threads',
          label: 'Total threads',
          description:
            'The total number of threads on the system. High values can indicate high concurrency or scheduling overhead.',
          summaryValueUnit: 'threads',
          columnName: 'threads_total',
          bucketMultipliers: TOTAL_COUNT_BUCKET_MULTIPLIERS,
          hotStartSql: TOTAL_THREAD_COUNT_HOT_START_SQL,
          timelineGroups: ['procs_threads_ctx_irq_faults'],
        }),
        createHotStartRelativeBucketedDeepDiveRow({
          key: 'total-processes',
          label: 'Total processes',
          description:
            'The total number of processes on the system. Use this metric to understand overall process activity during the run.',
          summaryValueUnit: 'processes',
          columnName: 'procs_total',
          sampleValueSql: 'try_cast("procs_total" AS DOUBLE)',
          bucketMultipliers: TOTAL_COUNT_BUCKET_MULTIPLIERS,
          hotStartSql: TOTAL_PROCESS_COUNT_HOT_START_SQL,
          timelineGroups: ['procs_threads_ctx_irq_faults'],
        }),
        createCpuRelativeBucketedDeepDiveRow({
          key: 'context-switches',
          label: 'Context switches / s',
          description:
            'How often the scheduler switches between threads. High values can indicate scheduling overhead or frequent task switching.',
          summaryValueUnit: 'switches/s',
          columnName: 'ctxt_per_s',
          bucketMultipliers: CONTEXT_SWITCH_RATE_PER_CORE_BUCKET_MULTIPLIERS,
          timelineGroups: ['procs_threads_ctx_irq_faults'],
        }),
        createCpuRelativeBucketedDeepDiveRow({
          key: 'irqs',
          label: 'IRQs / s',
          description:
            'How often hardware devices interrupt the CPU. High values can indicate heavy disk, network, or other device activity.',
          summaryValueUnit: 'IRQs/s',
          columnName: 'intr_per_s',
          bucketMultipliers: IRQ_RATE_PER_CORE_BUCKET_MULTIPLIERS,
          timelineGroups: ['procs_threads_ctx_irq_faults', 'per_core_irq'],
        }),
      ],
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 */
function renderSystemUtilization(context) {
  const TIMELINE_CSV_COMPONENT = 'tool/sysutil-timeline/0/timeline.csv';

  const renderers = [
    {
      type: 'CSV',
      id: 'timeline_csv',
      config: {
        component: TIMELINE_CSV_COMPONENT,
      },
    },
  ];

  const visualizations = [
    {
      type: 'system_utilization_summary',
      id: 'system_utilization_summary',
      rendererId: 'timeline_csv',
      title: 'Summary',
      description: '',
      config: systemUtilizationSummaryConfig,
    },
    {
      type: 'timeline',
      id: 'timeline',
      rendererId: 'timeline_csv',
      title: 'Timeline',
      description: '',
      config: {
        xAxisUnit: 's',
        data_source: {
          tables: {
            total_cpu_mem: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            per_core_cpu: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            cpu_saturation: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            iowait: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            memory_kb: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            swap_kb: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            numa_locality: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            numa_events: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            load_runqueue: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            procs_threads_ctx_irq_faults: [
              { renderer_id: 'timeline_csv', output: 'csv' },
            ],
            per_core_irq: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            disk_iops: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            disk_bw: [{ renderer_id: 'timeline_csv', output: 'csv' }],
            net_bw: [{ renderer_id: 'timeline_csv', output: 'csv' }],
          },
        },
        groups: {
          total_cpu_mem: {
            title: 'Total CPU and Memory Usage',
            type: 'line',
            index: 0,
            description:
              'CPU Use (%):\n' +
              'Overall CPU busy time across all cores. 100% means all CPU cores are fully busy.\n\n' +
              'Mem Use (%):\n' +
              'Memory in use as a percentage of total memory.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'Utilization (%)',
              yAxisDisplayRange: {
                min: 0,
                max: 100,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  'try_cast("cpu_total_percent" AS DOUBLE) AS "cpu_total_percent", ' +
                  'try_cast("mem_used_percent" AS DOUBLE) AS "mem_used_percent" ' +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'single',
                  name: 'CPU Use (%)',
                  xColumn: 'uptime_s',
                  yColumn: 'cpu_total_percent',
                },
                {
                  type: 'single',
                  name: 'Mem Use (%)',
                  xColumn: 'uptime_s',
                  yColumn: 'mem_used_percent',
                },
              ],
            },
          },
          per_core_cpu: SUMMARY_TIMELINE_GROUPS.per_core_cpu,
          cpu_saturation: {
            title: 'CPU Saturation',
            type: 'line',
            index: 2,
            description:
              'Saturated cores (count):\n' +
              `Number of CPU cores with utilization at or above ${CPU_SATURATION_THRESHOLD_PERCENT}%.`,
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'Saturated cores',
              yAxisDisplayRange: {
                min: 0,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query: CPU_SATURATION_TIMELINE_QUERY,
              },
              series: [
                {
                  type: 'single',
                  name: 'Saturated cores (count)',
                  xColumn: 'uptime_s',
                  yColumn: 'saturated_cores',
                },
              ],
            },
          },
          iowait: {
            title: 'IO Wait',
            type: 'line',
            index: 3,
            description:
              'IO Wait (%):\n' +
              'CPU time spent waiting for I/O operations to complete.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'CPU Time (%)',
              yAxisDisplayRange: {
                min: 0,
                max: 100,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  'try_cast("iowait_percent" AS DOUBLE) AS "iowait_percent" ' +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'single',
                  name: 'IO Wait (%)',
                  xColumn: 'uptime_s',
                  yColumn: 'iowait_percent',
                },
              ],
            },
          },
          memory_kb: SUMMARY_TIMELINE_GROUPS.memory_kb,
          swap_kb: {
            title: 'Swap Usage',
            type: 'line',
            index: 5,
            description:
              'Swap Total:\n' +
              'Total configured swap space.\n\n' +
              'Swap Used:\n' +
              'Swap space in use.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'kB',
              yAxisUnit: 'kibibyte',
              yAxisDisplayRange: {
                min: 0,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  'try_cast("swap_total_kb" AS DOUBLE) AS "swap_total_kb", ' +
                  'try_cast("swap_used_kb" AS DOUBLE) AS "swap_used_kb" ' +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'single',
                  name: 'Swap Total',
                  xColumn: 'uptime_s',
                  yColumn: 'swap_total_kb',
                },
                {
                  type: 'single',
                  name: 'Swap Used',
                  xColumn: 'uptime_s',
                  yColumn: 'swap_used_kb',
                },
              ],
            },
          },
          numa_locality: {
            title: 'NUMA Remote Memory',
            type: 'line',
            index: 6,
            description:
              'Remote Node (%):\n' +
              'Percentage of sampled memory pages allocated from a different NUMA node than the running process.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'Remote (%)',
              yAxisDisplayRange: {
                min: 0,
                max: 100,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  'try_cast("numa_remote_percent" AS DOUBLE) AS "numa_remote_percent" ' +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'single',
                  name: 'Remote Node (%)',
                  xColumn: 'uptime_s',
                  yColumn: 'numa_remote_percent',
                },
              ],
            },
          },
          numa_events: {
            title: 'NUMA Page Rates',
            type: 'line',
            index: 7,
            description:
              'Preferred Node Missed Pages/s:\n' +
              'Rate of memory pages allocated from a NUMA node other than the preferred NUMA node, summed across all NUMA nodes.\n\n' +
              'Remote Node Pages/s:\n' +
              'Rate of memory pages allocated from a different NUMA node than the running process.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'Pages/s',
              yAxisDisplayRange: {
                min: 0,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  'try_cast("numa_miss_per_s" AS DOUBLE) AS "numa_miss_per_s", ' +
                  'try_cast("numa_other_node_per_s" AS DOUBLE) AS "numa_other_node_per_s" ' +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'single',
                  name: 'Preferred Node Missed Pages/s',
                  xColumn: 'uptime_s',
                  yColumn: 'numa_miss_per_s',
                },
                {
                  type: 'single',
                  name: 'Remote Node Pages/s',
                  xColumn: 'uptime_s',
                  yColumn: 'numa_other_node_per_s',
                },
              ],
            },
          },
          load_runqueue: {
            title: 'System Load and Runqueue',
            type: 'line',
            index: 8,
            description:
              'Load 1m / 5m / 15m:\n' +
              'System load averages over the last 1, 5, and 15 minutes.\n' +
              'A value of 1.0 means one task (process or thread) was runnable or waiting for the CPU on average. On a machine with N CPU cores, a load near N suggests the system is fully utilized; values significantly above N indicate resource contention.\n\n' +
              'Tasks Running (count):\n' +
              'Tasks (processes or threads) ready to run on the CPU.\n\n' +
              'Tasks Blocked (count):\n' +
              'Tasks (processes or threads) waiting on I/O or another uninterruptible event.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'Value',
              yAxisDisplayRange: {
                min: 0,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  'try_cast("load1" AS DOUBLE) AS "load1", ' +
                  'try_cast("load5" AS DOUBLE) AS "load5", ' +
                  'try_cast("load15" AS DOUBLE) AS "load15", ' +
                  'try_cast("procs_running" AS DOUBLE) AS "procs_running", ' +
                  'try_cast("procs_blocked" AS DOUBLE) AS "procs_blocked" ' +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'single',
                  name: 'Load 1m',
                  xColumn: 'uptime_s',
                  yColumn: 'load1',
                },
                {
                  type: 'single',
                  name: 'Load 5m',
                  xColumn: 'uptime_s',
                  yColumn: 'load5',
                },
                {
                  type: 'single',
                  name: 'Load 15m',
                  xColumn: 'uptime_s',
                  yColumn: 'load15',
                },
                {
                  type: 'single',
                  name: 'Tasks Running (count)',
                  xColumn: 'uptime_s',
                  yColumn: 'procs_running',
                },
                {
                  type: 'single',
                  name: 'Tasks Blocked (count)',
                  xColumn: 'uptime_s',
                  yColumn: 'procs_blocked',
                },
              ],
            },
          },
          procs_threads_ctx_irq_faults: {
            title: 'Tasks, Interrupts and Faults',
            type: 'line',
            index: 9,
            description:
              'Processes (count):\n' +
              'Total number of processes on the system.\n\n' +
              'Threads (count):\n' +
              'Total number of threads on the system.\n\n' +
              'Context Switches/s:\n' +
              'Rate at which the scheduler switches between threads.\n\n' +
              'IRQs/s:\n' +
              'Rate of hardware interrupts.\n\n' +
              'Page Faults/s:\n' +
              'Rate of all page faults, which occur when a process accesses a memory page that is not currently mapped as needed. This includes faults that can be satisfied from memory without disk I/O (minor faults) and faults that require fetching data from disk or swap (major faults).\n\n' +
              'Major Faults/s:\n' +
              'Rate of the subset of page faults that required disk or swap I/O before execution could continue. These are typically more expensive than other page faults.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'Rate / Count',
              yAxisDisplayRange: {
                min: 0,
              },
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  'try_cast("procs_total" AS DOUBLE) AS "procs_total", ' +
                  'try_cast("threads_total" AS DOUBLE) AS "threads_total", ' +
                  'try_cast("ctxt_per_s" AS DOUBLE) AS "ctxt_per_s", ' +
                  'try_cast("intr_per_s" AS DOUBLE) AS "intr_per_s", ' +
                  'try_cast("page_faults_per_s" AS DOUBLE) AS "page_faults_per_s", ' +
                  'try_cast("pgmajfaults_per_s" AS DOUBLE) AS "pgmajfaults_per_s" ' +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'single',
                  name: 'Processes (count)',
                  xColumn: 'uptime_s',
                  yColumn: 'procs_total',
                },
                {
                  type: 'single',
                  name: 'Threads (count)',
                  xColumn: 'uptime_s',
                  yColumn: 'threads_total',
                },
                {
                  type: 'single',
                  name: 'Context Switches/s',
                  xColumn: 'uptime_s',
                  yColumn: 'ctxt_per_s',
                },
                {
                  type: 'single',
                  name: 'IRQs/s',
                  xColumn: 'uptime_s',
                  yColumn: 'intr_per_s',
                },
                {
                  type: 'single',
                  name: 'Page Faults/s',
                  xColumn: 'uptime_s',
                  yColumn: 'page_faults_per_s',
                },
                {
                  type: 'single',
                  name: 'Major Faults/s',
                  xColumn: 'uptime_s',
                  yColumn: 'pgmajfaults_per_s',
                },
              ],
            },
          },
          per_core_irq: {
            title: 'Per-Core IRQ Rate',
            type: 'heatmap',
            index: 10,
            description:
              'Core N IRQs/s:\n' +
              'Hardware interrupt rate for each individual core, summed across all IRQ sources.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'Per-Core IRQ Rate',
              yAxisUnit: 'IRQs/s',
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  "try_cast(COLUMNS('^irq_cpu\\d+_per_s$') AS DOUBLE) " +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'pattern',
                  xColumn: 'uptime_s',
                  yColumn: { pattern: '^irq_cpu(\\d+)_per_s$' },
                  name: { template: 'Core {y1}' },
                },
              ],
            },
          },
          disk_iops: {
            title: 'Disk IOPS',
            type: 'heatmap',
            index: 11,
            description:
              'Read IOPS:\n' +
              'Read operations per second for each storage device, shown separately.\n\n' +
              'Write IOPS:\n' +
              'Write operations per second for each storage device, shown separately.',
            config: {
              xAxisTitle: 'Time (s)',
              yAxisTitle: 'IOPS',
              customQuery: {
                tableNamePlaceholder: '{table}',
                query:
                  'SELECT ' +
                  'try_cast("uptime_s" AS DOUBLE) AS "uptime_s", ' +
                  "try_cast(COLUMNS('^read_iops_.*$') AS DOUBLE), " +
                  "try_cast(COLUMNS('^write_iops_.*$') AS DOUBLE) " +
                  'FROM {table} ORDER BY "uptime_s"',
              },
              series: [
                {
                  type: 'pattern',
                  xColumn: 'uptime_s',
                  yColumn: { pattern: '^read_iops_(.+)$' },
                  name: { template: 'Read IOPS {y1}' },
                },
                {
                  type: 'pattern',
                  xColumn: 'uptime_s',
                  yColumn: { pattern: '^write_iops_(.+)$' },
                  name: { template: 'Write IOPS {y1}' },
                },
              ],
            },
          },
          disk_bw: SUMMARY_TIMELINE_GROUPS.disk_bw,
          net_bw: SUMMARY_TIMELINE_GROUPS.net_bw,
        },
      },
    },
  ];

  return { renderers, visualizations };
}
