# Instruction Mix Query Guide

Use `run_query` to inspect the supplied run. Start with the bounded templates
below, then narrow them to the evidence needed for the conclusion.

Instruction Mix can expose dynamic evidence, static evidence or both. Dynamic
evidence can include symbol, source and disassembly data for follow-up.

## Approach by mode

- For `mode=dynamic`:
  - Query `drilldown_1` with `drilldown_measurements_1` for top-level sampled
    composition and `drilldown` with `drilldown_measurements` for function-level
    attribution. These queries are independent and can run concurrently.
  - Resolve `measurement_id` through the corresponding measurements table and
    select only the needed metrics. Use `total` percentages for the top-level
    mix and `self` percentages for function attribution.
  - Dynamic percentages are not a closed partition and may not sum to 100%; do
    not normalize them.
  - Treat absent or null metrics as unavailable, not zero. `flat_table` will
    be absent.
- For `mode=static`:
  - Use `flat_table` for instruction groups, counts and percentages; quote the
    `"group"` column.
  - Dynamic drilldown, source and disassembly are unavailable; do not call
    `load_source_contents`.
- For `mode=both`:
  - Treat dynamic top-level composition and function attribution as primary.
  - Query `flat_table` only when the user explicitly needs whole-binary or
    static composition, or when dynamic evidence is unavailable.
  - If static evidence is used, interpret it separately from dynamic evidence;
    differences are not problems by themselves.

Briefly summarize the dominant and other material categories in the evidence
used. Do not characterize a mix solely by its largest category when other
categories materially affect its interpretation. If both streams are used,
distinguish dynamic operation mix from static binary composition.

If a listed table is missing, inspect
`render_session.visualization_resolved_tables`. If a column is missing, inspect
the catalogue once before retrying.

If composition and function attribution support the conclusion, do not query
source or disassembly. If a specific unresolved question remains, use source
for code semantics or disassembly for generated instructions and
instruction-level attribution. Use both only when the conclusion depends on
relating them. Once a result answers the question, do not query that evidence
type again. Treat unavailable source or absent data as unavailable evidence,
not a reason to retry.

## Dynamic top-level composition

Query the root dynamic drilldown for the sampled runtime mix:

```sql
SELECT
  m.identifier,
  d.measurement_value
FROM drilldown_1 AS d
JOIN drilldown_measurements_1 AS m
  ON m.measurement_id = d.measurement_id
WHERE d.call_tree_id = 0
  AND (
    regexp_full_match(
      m.identifier,
      'ops\.mix\.[^.]+\.percent\.total'
    )
    OR m.identifier IN ('instr.ipc.total', 'samples.count.total')
  )
ORDER BY m.identifier
```

## Dynamic function attribution

Identify functions with meaningful sample coverage and their local mix. Return
functions through 95% cumulative self-sample coverage, capped at 50 rows.
`drilldown` has one row per `(symbol_id, measurement_id)`; the query returns
available top-level operation-mix percentages and IPC in a `self_metrics` map:

```sql
WITH function_samples AS (
  SELECT
    d.symbol_id,
    d.measurement_value AS self_samples
  FROM drilldown AS d
  JOIN drilldown_measurements AS m
    ON m.measurement_id = d.measurement_id
  WHERE m.identifier = 'samples.count.self'
    AND d.measurement_value > 0
),
function_metrics AS (
  SELECT
    d.symbol_id,
    map(
      list(m.identifier ORDER BY m.identifier),
      list(d.measurement_value ORDER BY m.identifier)
    ) AS self_metrics
  FROM drilldown AS d
  JOIN drilldown_measurements AS m
    ON m.measurement_id = d.measurement_id
  WHERE regexp_full_match(
    m.identifier,
    'ops\.mix\.[^.]+\.percent\.self'
  )
    OR m.identifier = 'instr.ipc.self'
  GROUP BY d.symbol_id
),
function_mix AS (
  SELECT
    fs.symbol_id,
    s.name AS function_name,
    s.source_file_id,
    sf.target_location,
    sf.host_location,
    s.first_source_line,
    s.last_source_line,
    fs.self_samples,
    fm.self_metrics
  FROM function_samples AS fs
  LEFT JOIN function_metrics AS fm
    ON fm.symbol_id = fs.symbol_id
  LEFT JOIN symbols AS s
    ON s.symbol_id = fs.symbol_id
  LEFT JOIN source_files AS sf
    ON sf.source_file_id = s.source_file_id
),
ranked AS (
  SELECT
    *,
    SUM(self_samples) OVER () AS total_self_samples,
    SUM(self_samples) OVER (
      ORDER BY self_samples DESC, function_name, symbol_id
      ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
    ) AS cumulative_self_samples,
    ROW_NUMBER() OVER (
      ORDER BY self_samples DESC, function_name, symbol_id
    ) AS sample_rank
  FROM function_mix
)
SELECT
  symbol_id,
  function_name,
  source_file_id,
  target_location,
  host_location,
  first_source_line,
  last_source_line,
  self_samples,
  ROUND(100.0 * cumulative_self_samples / NULLIF(total_self_samples, 0), 2)
    AS cumulative_sample_pct,
  self_metrics
FROM ranked
WHERE sample_rank <= 50
  AND cumulative_self_samples - self_samples < total_self_samples * 0.95
ORDER BY self_samples DESC, function_name
```

## Source follow-up

Prefer relevant application files; omit runtime, standard-library and header
sources unless central to the diagnosis. Fetch only bounded ranges, batching
selected files with a separate range for each:

```sql
WITH requested(source_file_id, first_line, last_line) AS (
  VALUES
    (<source_file_id>, <first_line>, <last_line>),
    (<source_file_id>, <first_line>, <last_line>)
),
loaded AS (
  SELECT
    l.source_file_id,
    l.content,
    l.failure_reasons,
    r.first_line,
    r.last_line
  FROM load_source_contents(
    '<run_id>',
    [<source_file_id>, <source_file_id>]
  ) AS l
  JOIN requested AS r
    ON r.source_file_id = l.source_file_id
),
source_lines AS (
  SELECT
    l.source_file_id,
    u.line_no,
    u.line,
    l.failure_reasons,
    l.first_line,
    l.last_line
  FROM loaded AS l
  LEFT JOIN LATERAL UNNEST(string_split(l.content, chr(10)))
    WITH ORDINALITY AS u(line, line_no) ON true
)
SELECT source_file_id, line_no, line, failure_reasons
FROM source_lines
WHERE line_no BETWEEN first_line AND last_line
   OR line_no IS NULL
ORDER BY source_file_id, line_no
```

For one file, use one `requested` row and a one-element source-file list. The
lateral left join preserves failed loads as null line data with non-empty
`failure_reasons`; successful rows have an empty list.

## Bounded disassembly follow-up

Batch selected functions in one query, returning a small window around the
hottest sampled instruction in each symbol:

```sql
WITH requested(symbol_id) AS (
  VALUES
    (<symbol_id>),
    (<symbol_id>)
),
ranked_hot AS (
  SELECT
    d.symbol_id,
    d.address,
    ROW_NUMBER() OVER (
      PARTITION BY d.symbol_id
      ORDER BY d.periodic_samples DESC, d.address
    ) AS hot_rank
  FROM disassembly AS d
  JOIN requested AS r
    ON r.symbol_id = d.symbol_id
  WHERE d.periodic_samples > 0
),
hot AS (
  SELECT symbol_id, address
  FROM ranked_hot
  WHERE hot_rank = 1
)
SELECT
  d.symbol_id,
  d.address,
  d."offset",
  d.instruction,
  d.arguments,
  d.opcode,
  d.periodic_samples,
  d.line_no
FROM disassembly AS d
JOIN hot AS h
  ON h.symbol_id = d.symbol_id
WHERE d.address BETWEEN h.address - 64 AND h.address + 64
ORDER BY d.symbol_id, d.address
```

For one function, use one `requested` row.

## Static composition

Use static evidence only when it materially adds to the conclusion, and keep
it separate from dynamic evidence. `All Instructions` is the aggregate total;
do not rank or sum it as another instruction group:

```sql
SELECT
  "group",
  instruction_count,
  percentage
FROM flat_table
ORDER BY percentage DESC, instruction_count DESC
```

Instruction mix describes composition, not performance impact. Do not infer a
bottleneck or optimization opportunity from the presence, absence or proportion
of an instruction category alone. Base recommendations on evidence connecting
the observation to runtime behaviour, or recommend collecting that evidence.

Stop when the evidence is sufficient.
