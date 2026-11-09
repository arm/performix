# Code Hotspots Query Guide

Use `run_query` to inspect Code Hotspots evidence for the supplied run. Start
with flat functions and call paths to establish where samples are spent. Then
use source and sampled disassembly to explain the bottleneck before recommending
a change.

## Tables and joins

Batch the four overview queries below: flat hotspots, call paths, sampled
source lines and disassembly.

Use `symbol_id` for symbol joins, `symbols.image_id` for images, and each
row's `source_file_id` for sources.

Run metadata gives the workload and recording times. Query process, thread
or time-limit tables only for additional detail.

`drilldown` contains flat function nodes and `drilldown_1` contains call-tree
nodes. Join each with its corresponding measurements table using
`measurement_id`. Code Hotspots periodic-sample measurements use
`unknown.measurement.periodic.samples.self` and
`unknown.measurement.periodic.samples.total`.

## Flat hotspots

Start with the functions responsible for most self samples. This distinguishes
work performed directly in a function from time in its callees.

```sql
WITH functions AS (
  SELECT
    d.measurement_value AS self_samples,
    s.symbol_id,
    s.name AS function_name,
    i.image_name,
    sf.source_file_id,
    sf.target_location,
    sf.host_location,
    s.first_source_line,
    s.last_source_line,
    SUM(d.measurement_value) OVER () AS total_self_samples
  FROM drilldown AS d
  JOIN drilldown_measurements AS m USING (measurement_id)
  LEFT JOIN symbols AS s USING (symbol_id)
  LEFT JOIN images AS i USING (image_id)
  LEFT JOIN source_files AS sf USING (source_file_id)
  WHERE d.node_type = 'function'
    AND m.identifier = 'unknown.measurement.periodic.samples.self'
    AND d.measurement_value > 0
), ranked AS (
  SELECT *, SUM(self_samples) OVER (
    ORDER BY self_samples DESC, symbol_id
    ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
  ) - self_samples AS cumulative_before
  FROM functions
)
SELECT
  function_name, image_name, source_file_id, target_location, host_location,
  first_source_line, last_source_line, self_samples, total_self_samples,
  ROUND(100.0 * self_samples / NULLIF(total_self_samples, 0), 2) AS self_pct
FROM ranked
WHERE cumulative_before < 0.99 * total_self_samples
ORDER BY self_samples DESC, function_name
LIMIT 40
```

## Call paths

Use both self and inclusive samples to identify the callers and callees that
form a material path. A function with high inclusive samples but low self
samples is mainly a container for hot callees.

```sql
WITH nodes AS (
  SELECT
    d.call_tree_id,
    d.call_tree_parent_id,
    d.symbol_id,
    SUM(CASE WHEN m.identifier =
      'unknown.measurement.periodic.samples.self'
      THEN d.measurement_value ELSE 0 END) AS self_samples,
    SUM(CASE WHEN m.identifier =
      'unknown.measurement.periodic.samples.total'
      THEN d.measurement_value ELSE 0 END) AS inclusive_samples
  FROM drilldown_1 AS d
  JOIN drilldown_measurements_1 AS m USING (measurement_id)
  WHERE d.node_type = 'function'
    AND m.identifier IN (
      'unknown.measurement.periodic.samples.self',
      'unknown.measurement.periodic.samples.total'
    )
  GROUP BY d.call_tree_id, d.call_tree_parent_id, d.symbol_id
)
SELECT
  n.call_tree_id, n.call_tree_parent_id,
  parent.name AS parent_function, child.name AS function_name,
  i.image_name, sf.source_file_id, sf.target_location, sf.host_location,
  n.self_samples, n.inclusive_samples
FROM nodes AS n
LEFT JOIN nodes AS p ON p.call_tree_id = n.call_tree_parent_id
LEFT JOIN symbols AS parent ON parent.symbol_id = p.symbol_id
LEFT JOIN symbols AS child ON child.symbol_id = n.symbol_id
LEFT JOIN images AS i ON i.image_id = child.image_id
LEFT JOIN source_files AS sf ON sf.source_file_id = child.source_file_id
WHERE n.self_samples > 0 OR n.inclusive_samples > 0
ORDER BY n.inclusive_samples DESC, n.self_samples DESC
LIMIT 40
```

## Source and disassembly

Query sampled source lines and instructions selectively for functions or files
identified above. Source attribution can include an inlined callee as well as
its caller, so use call-tree and disassembly evidence to resolve ambiguity.

```sql
SELECT
  p.source_file_id, sf.target_location, sf.host_location,
  p.line_no, p.function, p.inlined,
  SUM(p.periodic_samples) AS samples
FROM periodic_samples AS p
LEFT JOIN source_files AS sf USING (source_file_id)
WHERE p.periodic_samples > 0
GROUP BY p.source_file_id, sf.target_location, sf.host_location,
  p.line_no, p.function, p.inlined
ORDER BY samples DESC, p.source_file_id, p.line_no
LIMIT 40
```

```sql
SELECT
  d.address, s.name AS function_name, i.image_name, d."offset", d.instruction,
  d.arguments, d.opcode, d.periodic_samples, d.source_file_id,
  sf.target_location, sf.host_location, d.line_no
FROM disassembly AS d
LEFT JOIN symbols AS s USING (symbol_id)
LEFT JOIN images AS i USING (image_id)
LEFT JOIN source_files AS sf ON sf.source_file_id = d.source_file_id
WHERE d.periodic_samples > 0
ORDER BY d.periodic_samples DESC, d.address
LIMIT 40
```

Batch source-loading and disassembly queries once the overview identifies
relevant files and functions.

For the most significant sampled instructions, request nearby instructions in
the same symbol before concluding why that code is expensive. Do not recommend
vectorisation, architecture-specific instructions, changed call structure,
weaker atomics, or allocation changes unless source and generated instructions
support the recommendation. Existing SIMD instructions alone do not prove that
the implementation is using the target effectively.

Fetch only source files identified by the evidence. Prefer application source
with a non-null host path over runtime, standard-library, or header files.

Read the context around a sampled line, not an isolated line. If source is
unavailable, continue with the available profile and disassembly evidence and
state the resulting limitation when it affects the conclusion.
