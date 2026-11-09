# CPU Microarchitecture Query Guide

Use `run_query` to inspect CPU Microarchitecture evidence for the supplied run.
Start with Topdown Level 1 and inspect secondary measurements for each material
category. Distinguish relevant instruction-fetch/frontend, data-access/backend
and speculation effects at the functions or call paths under investigation;
a run-wide dominant category is insufficient. Use flat functions for self time
and the call tree for inclusive paths. Hotspots and source/disassembly alone
do not establish cache, memory or branch-prediction bottlenecks; use source or
disassembly to verify implementation claims.

## Tables

`drilldown` contains flat functions and `drilldown_1` contains the run root and
call tree. Join each to its adjacent `_measurements` table.

## Level 1 overview

Begin with the four Topdown categories, IPC and sample count:

```sql
SELECT
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.frontend_bound.percent.total') AS frontend_bound_pct,
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.backend_bound.percent.total') AS backend_bound_pct,
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.retiring.percent.total') AS retiring_pct,
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.bad_speculation.percent.total') AS bad_speculation_pct,
  MAX(d.measurement_value) FILTER
    (WHERE m.identifier = 'instr.ipc.total') AS ipc,
  MAX(d.measurement_value) FILTER
    (WHERE m.identifier = 'samples.count.total') AS samples
FROM drilldown_1 AS d
JOIN drilldown_measurements_1 AS m USING (measurement_id)
WHERE d.call_tree_id = 0
```

A category is material when it is large enough to affect the conclusion, not
merely because it is the largest of four small values.

## Secondary measurements

For each material category, use this row-form query with the relevant
identifiers below. It returns the names, short descriptions and units used by
the recipe UI:

```sql
SELECT
  m.identifier,
  m.name,
  m.short_description,
  m.units,
  d.measurement_value
FROM drilldown_1 AS d
JOIN drilldown_measurements_1 AS m USING (measurement_id)
WHERE d.call_tree_id = 0
  AND m.identifier IN ('<identifier>', '<identifier>')
ORDER BY m.identifier
```

Query `description` only when the formula or underlying events are needed; it
can be much larger. Expand each shorthand value between `<...>` into a separate
identifier before using it in SQL.

### Frontend Bound

Frontend Bound means instructions were not supplied to the execution pipeline.
Inspect:

```text
tlb.instruction.<l1.miss.percent|l1.mpki|walk.percent|mpki>.total
cache.instruction.l1.<miss.percent|mpki>.total
```

MPKI reports misses per thousand instructions and is more comparable across
code paths than miss percentage. High L1I MPKI with low instruction-TLB walk
evidence supports an instruction-cache problem. L1 instruction-TLB misses with
few walks may be handled by a lower-level TLB. Attribute branch recovery to Bad
Speculation, not Frontend Bound.

### Backend Bound

Backend Bound means execution resources or data were not ready. Inspect:

```text
tlb.data.<l1.miss.percent|l1.mpki|walk.percent|mpki>.total
tlb.l2.<miss.percent|mpki>.total
cache.data.l1.<miss.percent|mpki>.total
cache.l2.<miss.percent|mpki>.total
cache.ll.read.<miss.percent|mpki>.total
ops.mix.<load|store>.percent.total
```

Compare L1D, L2 and last-level MPKI to see how far misses continue. An L1
data-TLB miss is not necessarily a page walk; compare it with walk percentage
and DTLB MPKI. Load and store percentages describe the work but do not prove a
memory bottleneck.

### Retiring

Retiring means operations completed and were not discarded. Inspect:

```text
instr.ipc.total
ops.mix.<integer|load|store|branch|fp|simd|sve|crypto|barrier>.percent.total
```

High Retiring is not inherently a defect, and operation mix does not show that
work is avoidable. Existing fixed-width vectors on a wider-vector target do not
prove that wider vectors or more unrolling will help. Do not recommend wider
vectors or native intrinsics when Retiring and IPC are high, stalls are low and
disassembly confirms vectorisation. First establish a measured limit or reduce
the work.

### Bad Speculation

Bad Speculation means operations were executed and then discarded, commonly
after a branch misprediction. Inspect:

```text
branch.mispredict.<mpki|percent>.total
ops.mix.branch.percent.total
```

Misprediction percentage says how often branches were wrong; Branch MPKI says
how often that happens per thousand instructions. Use both with source or
disassembly. Classify an unpredictable jump by its measured Bad Speculation,
not as Frontend Bound.

## Call-tree attribution

Use the call tree to find material application paths. Inclusive `total`
measurements preserve evidence when one logical path is split across many
specialised functions, while `self` measurements describe the node itself:

```sql
WITH root AS (
  SELECT d.measurement_value AS total_samples
  FROM drilldown_1 AS d
  JOIN drilldown_measurements_1 AS m USING (measurement_id)
  WHERE d.call_tree_id = 0 AND m.identifier = 'samples.count.total'
), nodes AS (
  SELECT
    d.call_tree_id,
    d.call_tree_parent_id,
    d.symbol_id,
    MAX(d.measurement_value) FILTER
      (WHERE m.identifier = 'samples.count.self') AS self_samples,
    MAX(d.measurement_value) FILTER
      (WHERE m.identifier = 'samples.count.total') AS inclusive_samples,
    MAX(d.measurement_value) FILTER (WHERE m.identifier =
      'pipeline.topdown.frontend_bound.percent.total') AS frontend_pct,
    MAX(d.measurement_value) FILTER (WHERE m.identifier =
      'pipeline.topdown.backend_bound.percent.total') AS backend_pct,
    MAX(d.measurement_value) FILTER (WHERE m.identifier =
      'pipeline.topdown.retiring.percent.total') AS retiring_pct,
    MAX(d.measurement_value) FILTER (WHERE m.identifier =
      'pipeline.topdown.bad_speculation.percent.total') AS bad_spec_pct,
    MAX(d.measurement_value) FILTER
      (WHERE m.identifier = 'instr.ipc.total') AS ipc,
    MAX(d.measurement_value) FILTER
      (WHERE m.identifier = '<secondary_identifier>') AS secondary_value
  FROM drilldown_1 AS d
  JOIN drilldown_measurements_1 AS m USING (measurement_id)
  WHERE d.node_type = 'function' AND d.call_tree_parent_id >= 0
  GROUP BY d.call_tree_id, d.call_tree_parent_id, d.symbol_id
)
SELECT
  n.call_tree_id,
  CASE WHEN length(ps.name) > 131
    THEN left(ps.name, 64) || '...' || right(ps.name, 64)
    ELSE ps.name END AS parent_function,
  CASE WHEN length(s.name) > 131
    THEN left(s.name, 64) || '...' || right(s.name, 64)
    ELSE s.name END AS function_name,
  ROUND(100 * n.self_samples / NULLIF(r.total_samples, 0), 2) AS self_pct,
  ROUND(100 * n.inclusive_samples / NULLIF(r.total_samples, 0), 2) AS inclusive_pct,
  n.frontend_pct,
  n.backend_pct,
  n.retiring_pct,
  n.bad_spec_pct,
  n.ipc,
  n.secondary_value
FROM nodes AS n
CROSS JOIN root AS r
LEFT JOIN symbols AS s ON s.symbol_id = n.symbol_id
LEFT JOIN nodes AS pn ON pn.call_tree_id = n.call_tree_parent_id
LEFT JOIN symbols AS ps ON ps.symbol_id = pn.symbol_id
WHERE n.self_samples >= 0.01 * r.total_samples
   OR n.inclusive_samples >= 0.01 * r.total_samples
ORDER BY n.inclusive_samples DESC, n.self_samples DESC
LIMIT 40
```

Replace or repeat `secondary_value` for only the material secondary
measurements.

## Flat functions

Use flat functions when self time identifies the implementation more clearly
than a calling path. Do not give a negligible function's percentages the same
weight as a hot function:

```sql
SELECT
  CASE WHEN length(s.name) > 131
    THEN left(s.name, 64) || '...' || right(s.name, 64)
    ELSE s.name END AS function_name,
  i.image_name,
  MAX(d.measurement_value) FILTER
    (WHERE m.identifier = 'samples.count.self') AS samples,
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.frontend_bound.percent.self') AS frontend_pct,
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.backend_bound.percent.self') AS backend_pct,
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.retiring.percent.self') AS retiring_pct,
  MAX(d.measurement_value) FILTER (WHERE m.identifier =
    'pipeline.topdown.bad_speculation.percent.self') AS bad_spec_pct,
  MAX(d.measurement_value) FILTER
    (WHERE m.identifier = 'instr.ipc.self') AS ipc
FROM drilldown AS d
JOIN drilldown_measurements AS m USING (measurement_id)
LEFT JOIN symbols AS s USING (symbol_id)
LEFT JOIN images AS i USING (image_id)
WHERE d.node_type = 'function'
GROUP BY d.symbol_id, s.name, i.image_name
HAVING samples > 0
ORDER BY samples DESC
LIMIT 40
```

For secondary function evidence, repeat stable function and image names and use
the `.self` form of the relevant identifiers. Before an exact lookup, narrow the
function query and return the full `s.name`; an abbreviated name will not match
an equality predicate. To combine template specialisations into a family, pivot
per symbol in a CTE and then group the CTE.

## Source

Find hot source lines:

```sql
SELECT
  ps.source_file_id,
  sf.target_location,
  ps.line_no,
  ps.function,
  SUM(ps.periodic_samples) AS samples
FROM periodic_samples AS ps
LEFT JOIN source_files AS sf USING (source_file_id)
WHERE ps.periodic_samples > 0
GROUP BY ps.source_file_id, sf.target_location, ps.line_no, ps.function
ORDER BY samples DESC
LIMIT 40
```

Fetch a selected application file by reusing an exact hot `target_location`
returned above:

```sql
SELECT
  sf.target_location,
  loaded.content,
  loaded.failure_reasons
FROM source_files AS sf
CROSS JOIN LATERAL load_source_contents(
  '<run_id>',
  [sf.source_file_id]
) AS loaded
WHERE sf.target_location = '<hot_target_location>'
ORDER BY sf.source_file_id
LIMIT 1
```

Prefer material application files over runtime, standard-library and header
sources. Use the sampled line numbers to focus on the relevant part of the
returned file. If `failure_reasons` is non-empty, use disassembly and reduce
confidence where necessary.

## Disassembly

Select disassembly by symbol IDs from this session, or full function and image
names:

```sql
SELECT
  d."offset" AS instruction_offset,
  d.instruction,
  d.arguments,
  d.periodic_samples,
  d.line_no
FROM disassembly AS d
JOIN symbols AS s USING (symbol_id)
LEFT JOIN images AS i USING (image_id)
WHERE s.name = '<full_function_name>'
  AND i.image_name = '<image_name>'
  AND d.periodic_samples > 0
ORDER BY d.periodic_samples DESC, d.address
LIMIT 80
```

Use source and disassembly together before recommending architecture-specific
instructions, data-layout or control-flow changes, or weaker memory ordering.
Do not recommend an optimisation already present without evidence that it is
incomplete. Atomic instructions or runtime helpers and cache or load/store
evidence may support a coherence diagnosis.
Do not describe coherence traffic as directly measured.
