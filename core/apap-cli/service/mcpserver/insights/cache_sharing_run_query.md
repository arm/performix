# Cache Sharing Query Guide

Use `run_query` to establish whether the run contains material cache sharing,
then attribute only the important cache lines. Inspect source when it is needed
to distinguish independent adjacent state from genuinely shared state.

## Data model

`flat_table` has one row per observed cache line:

- `sample_count` and `sample_pct` describe its sampled coverage.
- `sharing_classification` is the collector's `TRUE`- or `FALSE`-sharing
  classification.
- `distinct_sharing_offsets`, `sharing_thread_count`, and
  `writer_thread_count` are collector-provided cache-line summaries. Do not
  interpret them individually as the total unique participants across all
  offsets.

`drilldown` attributes each cache line and byte offset to symbols, images, and
source. Its measurements include total, coherence, and store samples. The
queries below resolve measurements by stable identifier rather than numeric ID.

If a named table or column is unavailable, inspect the resolved tables and
`duckdb_columns()` once before adapting the queries. Do not guess a schema.

## 1. Establish materiality

First summarize all sampled lines by classification:

```sql
WITH grouped AS (
  SELECT
    sharing_classification,
    count(*) AS cachelines,
    sum(sample_count) AS samples,
    round(sum(sample_pct), 2) AS sample_pct
  FROM flat_table
  WHERE sample_count > 0
  GROUP BY sharing_classification
)
SELECT
  g.sharing_classification,
  g.cachelines,
  g.samples,
  g.sample_pct
FROM grouped AS g
ORDER BY g.samples DESC
```

Then rank true- and false-sharing lines independently. This prevents a
dominant classification from hiding a less frequent but still material one:

```sql
WITH ranked AS (
  SELECT
    *,
    row_number() OVER (
      PARTITION BY sharing_classification
      ORDER BY sample_count DESC, cacheline_address
    ) AS classification_rank
  FROM flat_table
  WHERE sample_count > 0
)
SELECT
  cacheline_address,
  sample_count,
  sample_pct,
  sharing_classification,
  distinct_sharing_offsets,
  sharing_thread_count,
  writer_thread_count
FROM ranked
WHERE classification_rank <= 20
ORDER BY sharing_classification, sample_count DESC, cacheline_address
```

Use absolute sample count and percentage together. A classification may
contain many negligible lines, and its highest-ranked line is not necessarily
important to the run overall. An empty result means there is no sampled
cache-sharing evidence in these tables. It does not prove that no coherence
traffic occurred.

## 2. Attribute material cache lines

Copy an exact address from the ranking into this query. The pivot turns the
long-form measurements into one row per attributed access. Starting from the
measurement table also keeps the expected columns available for sparse runs.

```sql
WITH accesses AS (
  PIVOT (
    SELECT d.call_tree_id, d.symbol_id, m.identifier, d.measurement_value
    FROM drilldown_measurements AS m
    LEFT JOIN drilldown AS d USING (measurement_id)
  )
  ON identifier
  USING max(measurement_value)
  GROUP BY call_tree_id, symbol_id
)
SELECT
  printf('0x%x', CAST(a."perf.c2c.cacheline_address" AS UBIGINT))
    AS cacheline_address,
  CAST(a."perf.c2c.byte_offset" AS BIGINT) AS byte_offset,
  s.name AS function_name,
  i.image_name,
  sf.target_location,
  s.first_source_line,
  s.last_source_line,
  CAST(a."perf.c2c.samples" AS BIGINT) AS samples,
  CAST(a."perf.c2c.coherence_samples" AS BIGINT) AS coherence_samples,
  CAST(a."perf.c2c.store_samples" AS BIGINT) AS store_samples,
  CAST(a."perf.c2c.thread_count" AS BIGINT) AS thread_count,
  CAST(a."perf.c2c.writer_thread_count" AS BIGINT) AS writer_thread_count
FROM accesses AS a
LEFT JOIN symbols AS s USING (symbol_id)
LEFT JOIN images AS i USING (image_id)
LEFT JOIN source_files AS sf USING (source_file_id)
WHERE printf('0x%x', CAST(a."perf.c2c.cacheline_address" AS UBIGINT))
      = lower('<cacheline_address>')
ORDER BY coherence_samples DESC, samples DESC, byte_offset
LIMIT 80
```

Do not add thread or writer counts across attribution rows. They describe each
access group. The drilldown is flat function attribution, not a call tree. If
80 rows omit important accesses, narrow by byte offset or function instead of
removing the bound.

## 3. Inspect source when useful

For a material application row, copy its exact source path and load a small
range around the reported lines:

```sql
WITH loaded AS (
  SELECT sf.target_location, l.content, l.failure_reasons
  FROM source_files AS sf
  CROSS JOIN LATERAL load_source_contents(
    '<run_id>', [sf.source_file_id]
  ) AS l
  WHERE sf.target_location = '<exact_target_location>'
  LIMIT 1
)
SELECT target_location, u.line_no, u.line, failure_reasons
FROM loaded
LEFT JOIN LATERAL unnest(string_split(content, chr(10)))
  WITH ORDINALITY AS u(line, line_no) ON true
WHERE u.line_no BETWEEN <first_line> AND <last_line>
   OR u.line_no IS NULL
ORDER BY u.line_no
```

The left join preserves a source-loading failure as a row with
`failure_reasons`. If source is unavailable, use symbol and image evidence,
lower confidence, and do not infer a data structure from an address.

## Interpretation

- Treat `sharing_classification` as the collector's result, not a conclusion
  independently proved by the query. Corroborate it with materiality, offsets,
  threads, writers, and attributed code.
- False sharing normally has multiple threads collectively accessing
  independent state at distinct offsets in the same line. A
  `sharing_thread_count` of `1` does not by itself rule it out. Consider the
  writer count and offset-level attribution together. Source may justify
  separating, aligning, or batching those updates. Do not recommend padding
  without evidence of adjacent independently accessed fields.
- True sharing normally has multiple writers accessing the same logical state.
  Padding cannot remove contention on that value. Consider reducing, batching,
  sharding, or periodically aggregating shared updates.
- A high thread count, writer count, or hot function alone does not prove a
  material sharing problem. Prioritize meaningful sample coverage supported by
  consistent attribution.
- Sample, coherence, and store counts are sampled evidence, not elapsed time,
  stall cycles, exact cache-line transfers, or measured performance impact.
- Runtime addresses are not stable source identities. Distinguish application,
  library, and unknown attribution, and do not turn sparse or contradictory
  evidence into an application diagnosis.

Stop when the ranking, attribution, and available source support the requested
conclusion. If they do not, state the limitation and identify what additional
evidence is needed.
