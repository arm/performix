# Memory Access Query Guide

Use `run_query` to inspect load-latency and address-translation evidence for
the supplied run. Establish material sample coverage first, inspect the memory
levels contributing latency, then examine TLB walks. Attribute each material
finding to code where possible and state attribution limits before recommending
a change.

## Data model

The Memory Access render exposes these tables:

- `drilldown` and `drilldown_measurements` contain load-latency evidence.
- `drilldown_1` and `drilldown_measurements_1` contain TLB evidence.

`symbols`, `images`, and `source_files` provide shared attribution.

Cache and memory measurements are included only when available for that run. Do
not interpret a missing measurement as zero.

## 1. Find functions with significant load latency

Use `cache.l1_hit_improvement_potential` to rank functions by potential
improvement. It estimates the cycles recoverable if all loads hit the ideal L1
latency. Use sample coverage, average load latency, load/store mix, and the cache
and memory breakdown in the next section to decide whether a result is
significant:

```sql
WITH per_function AS (
  SELECT
    d.symbol_id,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'spe.samples.count'
    ) AS sample_count,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'memory.load.average_latency.cycles'
    ) AS avg_load_latency,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'memory.load.instructions.percent'
    ) AS load_memory_op_pct,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'cache.l1_hit_improvement_potential'
    ) AS l1_hit_improvement_potential
  FROM drilldown AS d
  JOIN drilldown_measurements AS m USING (measurement_id)
  WHERE m.identifier IN (
    'spe.samples.count',
    'memory.load.average_latency.cycles',
    'memory.load.instructions.percent',
    'cache.l1_hit_improvement_potential'
  )
  GROUP BY d.symbol_id
), totals AS (
  SELECT SUM(sample_count) AS total_samples
  FROM per_function
)
SELECT
  s.name AS function_name,
  i.image_name,
  sf.target_location,
  s.first_source_line,
  s.last_source_line,
  ROUND(p.sample_count) AS sample_count,
  ROUND(100.0 * p.sample_count / NULLIF(t.total_samples, 0), 2)
    AS sample_pct,
  ROUND(p.avg_load_latency, 2) AS avg_load_latency,
  ROUND(p.load_memory_op_pct, 2) AS load_memory_op_pct,
  ROUND(p.l1_hit_improvement_potential)
    AS l1_hit_improvement_potential
FROM per_function AS p
JOIN symbols AS s USING (symbol_id)
LEFT JOIN images AS i USING (image_id)
LEFT JOIN source_files AS sf USING (source_file_id)
CROSS JOIN totals AS t
WHERE p.sample_count > 0
ORDER BY p.l1_hit_improvement_potential DESC NULLS LAST,
         p.avg_load_latency DESC NULLS LAST,
         p.sample_count DESC
LIMIT 20
```

Use the improvement estimate only to rank functions. It is not enough on its
own to show a problem. `memory.load.instructions.percent` is the percentage of
memory operations that are loads, not the percentage of all instructions.

## 2. Inspect the contributing memory levels

For a material row above, copy its exact function and image names into this
query. It dynamically discovers active memory-level prefixes rather than
assuming a fixed set of columns:

```sql
WITH overall AS (
  SELECT
    d.symbol_id,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'spe.samples.count'
    ) AS sample_count,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'memory.load.average_latency.cycles'
    ) AS avg_load_latency,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'cache.l1_hit_improvement_potential'
    ) AS l1_hit_improvement_potential
  FROM drilldown AS d
  JOIN drilldown_measurements AS m USING (measurement_id)
  GROUP BY d.symbol_id
), levels AS (
  SELECT
    d.symbol_id,
    split_part(m.identifier, '.load.', 1) AS memory_level,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier LIKE '%.load.percent'
    ) AS load_pct,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier LIKE '%.load.average_latency.cycles'
    ) AS avg_latency,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier LIKE '%.load.latency_contribution.cycles'
    ) AS contribution_cycles,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier LIKE '%.load.latency_contribution.percent'
    ) AS contribution_pct
  FROM drilldown AS d
  JOIN drilldown_measurements AS m USING (measurement_id)
  WHERE m.identifier LIKE '%.load.%'
    AND m.identifier NOT IN (
      'memory.load.average_latency.cycles',
      'memory.load.instructions.percent'
    )
  GROUP BY d.symbol_id, memory_level
)
SELECT
  s.name AS function_name,
  i.image_name,
  sf.target_location,
  ROUND(o.sample_count) AS sample_count,
  ROUND(o.avg_load_latency, 2) AS avg_load_latency,
  ROUND(o.l1_hit_improvement_potential)
    AS l1_hit_improvement_potential,
  l.memory_level,
  ROUND(l.load_pct, 2) AS load_pct,
  ROUND(l.avg_latency, 2) AS level_avg_latency,
  ROUND(l.contribution_cycles, 2) AS contribution_cycles,
  ROUND(l.contribution_pct, 2) AS contribution_pct
FROM overall AS o
JOIN levels AS l USING (symbol_id)
JOIN symbols AS s USING (symbol_id)
LEFT JOIN images AS i USING (image_id)
LEFT JOIN source_files AS sf USING (source_file_id)
WHERE s.name = '<exact_function_name>'
  AND i.image_name = '<exact_image_name>'
ORDER BY l.contribution_cycles DESC NULLS LAST, l.memory_level
LIMIT 20
```

Current level prefixes can include `cache.l1`, `cache.l2`, `cache.ll`,
`memory.peer`, `memory.local_cluster`, `memory.peer_cluster`, `memory.remote`,
and `memory.dram`. Use load percentage to understand where sampled loads were
served and contribution cycles or percentage to understand which levels drive
the effective latency. Neither one alone establishes a bottleneck.

## 3. Establish TLB-walk materiality

Use TLB walk score to rank functions when it is available, then assess the
underlying access coverage, walk count, walk percentage, and average walk
latency. The latency sample columns provide additional run coverage, while
`tlb_access_pct` covers a function that appears only in the TLB drilldown:

```sql
WITH latency_samples AS (
  SELECT
    d.symbol_id,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'spe.samples.count'
    ) AS sample_count
  FROM drilldown AS d
  JOIN drilldown_measurements AS m USING (measurement_id)
  GROUP BY d.symbol_id
), sample_total AS (
  SELECT SUM(sample_count) AS total_samples
  FROM latency_samples
), tlb_metrics AS (
  SELECT
    d.symbol_id,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'tlb.access.count'
    ) AS tlb_accesses,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'tlb.walk.count'
    ) AS tlb_walks,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'tlb.walk.percent'
    ) AS tlb_walk_pct,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'tlb.walk.average_latency'
    ) AS tlb_walk_avg_latency,
    MAX(d.measurement_value) FILTER (
      WHERE m.identifier = 'tlb.walk.score'
    ) AS tlb_walk_score
  FROM drilldown_1 AS d
  JOIN drilldown_measurements_1 AS m USING (measurement_id)
  GROUP BY d.symbol_id
), tlb_total AS (
  SELECT SUM(tlb_accesses) AS total_tlb_accesses
  FROM tlb_metrics
)
SELECT
  s.name AS function_name,
  i.image_name,
  sf.target_location,
  s.first_source_line,
  s.last_source_line,
  ROUND(ls.sample_count) AS sample_count,
  ROUND(100.0 * ls.sample_count / NULLIF(st.total_samples, 0), 2)
    AS sample_pct,
  ROUND(t.tlb_accesses) AS tlb_accesses,
  ROUND(100.0 * t.tlb_accesses / NULLIF(tt.total_tlb_accesses, 0), 2)
    AS tlb_access_pct,
  ROUND(t.tlb_walks) AS tlb_walks,
  ROUND(t.tlb_walk_pct, 2) AS tlb_walk_pct,
  ROUND(t.tlb_walk_avg_latency, 2) AS tlb_walk_avg_latency,
  ROUND(t.tlb_walk_score) AS tlb_walk_score
FROM tlb_metrics AS t
JOIN symbols AS s USING (symbol_id)
LEFT JOIN images AS i USING (image_id)
LEFT JOIN source_files AS sf USING (source_file_id)
LEFT JOIN latency_samples AS ls USING (symbol_id)
CROSS JOIN sample_total AS st
CROSS JOIN tlb_total AS tt
WHERE COALESCE(t.tlb_accesses, 0) > 0
ORDER BY t.tlb_walk_score DESC NULLS LAST,
         t.tlb_walk_avg_latency DESC NULLS LAST,
         t.tlb_walks DESC
LIMIT 20
```

Zero walks with a null average walk latency and score is valid evidence of no
sampled walks.

## 4. Compare function attribution

Compare the bounded latency and TLB results by stable function and image names,
using source paths when available. Keep each finding attached to its responsible
function and treat the two evidence sets independently. Numeric symbol IDs are
stable within the render session and can be used to correlate results across
`run_query` calls.

## 5. Inspect source when useful

For a material row with source attribution, copy its exact source path and load
a small range around the reported lines:

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
lower confidence, and do not infer a data layout or traversal from a function
name alone.

## Interpretation

- Require meaningful sample or TLB-access coverage and consistent measurements
  before diagnosing a function. If coverage is sparse or measurements conflict,
  report insufficient evidence.
- Consider load distribution and latency contribution together. Neither shows a
  significant issue on its own.
- Distinguish application, library, and unknown code. Do not make
  application-specific recommendations for library or unresolved addresses.
- These drilldowns show flat functions. Do not infer caller paths or inclusive
  costs.
- Inspect application source before recommending code or data-layout changes.
  If source is unavailable, state the attribution limitation.
- Recommend huge pages only when supported by TLB-walk evidence and the memory
  does not already use them. Note platform and memory trade-offs.
- Treat latency metrics and scores as estimates, not exact performance impact.
  Do not add or compare the potential improvement estimate and TLB walk score
  because their evidence may overlap.
- A TLB miss or walk does not imply a page fault or storage access.

Stop when the materiality, attribution, and available source support the
requested conclusion. State any remaining limitation and the evidence needed
to resolve it.
