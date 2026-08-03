<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Timeline SQL Renderer Multi-LoD Design

## Context

We want a timeline recipe shape that:

- uses `SQLRenderer` and static SQL today
- loads timeline data into the render session using hive-partitioned parquet
- remains performant for large datasets
- keeps the source data compressed rather than materializing a dense full-rank table
- allows recipe-defined aggregation and derived series now
- leaves a clean path to future time-range filtering, bin-duration selection, and expression support

The current GUI timeline visualization can consume static query results only. It cannot yet:

- switch LoD dynamically
- inject time-range filters
- inject selected series or aggregation parameters
- request ad hoc expressions

This design is therefore a POC-oriented static view topology plus visualization-owned final chart SQL that keeps those future extensions possible without changing the base data model.

For this POC, the recipe used to prove the design must be `code_hotspots`.

For automated tests, this design assumes the current binned counter parquet contract exercised by the branch tests. That contract is sufficient for fixture-driven tests of the renderer SQL and final chart SQL, but it is not treated here as a final universal schema for every future timeline data source.

## Goals

- Minimize render-session initialization cost.
- Minimize query latency for current timeline usage.
- Keep parquet access pushdown-friendly.
- Keep the engine and timeline view generic enough that recipe authors can define performant SQL for their own data shape.
- Support recipe-defined derived series and cross-series expressions at a single selected `bin_duration`.
- Prepare for future GUI-driven LoD switching and time-range pushdown.
- Add automated tests for the recipe-authored timeline SQL and view topology without depending on a real recipe run.

## Non-Goals

- Materializing dense full-rank timeline tables in memory.
- Requiring render-param-driven re-rendering for LoD changes.
- Designing a universal long-form timeline schema for every future data source.
- Supporting GUI-authored ad hoc expressions in this iteration.
- Requiring automated tests to run a real `code_hotspots` capture or recipe execution.

## Constraints

- Full data sets may be large.
- We must avoid eager expansion of compressed bins across the full dataset.
- We must still produce correct per-bin sample semantics for the selected `bin_duration`.
- We want metadata-level loading of the data sources, not full data materialization.
- Static SQL is acceptable for the POC.
- We are allowed to make recipe authors responsible for writing performant SQL.
- Generic timeline query templates are desirable later, but not at the cost of current latency.
- Automated tests should generate parquet fixtures using DuckDB SQL rather than large Go-side parquet schema structs.

## Recommendation

Use a shared recipe-side timeline SQL utility plus a static multi-LoD view family built with `SQLRenderer`:

1. one shared JS utility that returns renderer config for timeline parquet inputs
2. one base scan view per `bin_duration`
3. one expanded-source view per source per `bin_duration`
4. one final chart SQL definition per chart per `bin_duration` in the visualization config
5. optional small metadata views only where needed

`code_hotspots` should call the shared utility from its render stage rather than embedding the base/expansion SQL inline.

The timeline visualization should consume the expanded-source views through its final chart SQL directly. Later, the GUI can select among LoD-specific sources by zoom level and wrap the chart SQL with templated time-range predicates.

## Rejected Approach

Do not create a single dense full-rank view that expands every compressed interval across all bins and all `bin_duration` values.

That shape:

- inflates row count substantially
- defeats pruning in common query paths
- adds render-time and query-time cost
- makes partition-filtered queries much slower

The compressed source shape must remain the base representation. That does not remove the need to expand compressed samples when producing chart-ready data for one selected `bin_duration`. The rejected shape is a global dense table, not bounded LoD-local expansion.

## Architecture

### 0. Shared Timeline SQL Utility

Create a shared recipe-side utility that owns the timeline `SQLRenderer` configuration and returns the renderer definitions for a given timeline parquet input root.

That utility should:

- be called by `code_hotspots` during `render()`
- be reusable by future recipes that adopt the same timeline parquet model
- centralize the complex static SQL/view-family definition
- accept a small input contract such as parquet path/glob, chart identifiers, and other static chart wiring inputs

That utility is responsible only for producing renderer config and stable expanded-source bindings. It is not responsible for generating parquet from APC, invoking external executables, or orchestrating capture-time data production.

### 0.1. Test Fixture Input Contract

Automated tests should generate parquet fixtures that match the current binned counter contract only.

Tests should expose binned counter parquet files through the current manifest path contract:

- `tool/neoprof/0/output/parquet/timeline/series_id=<series_id>/bin_duration=<bin_duration>/counter.parquet`

Each binned counter parquet file should contain:

- `start_timestamp` - int64
- `end_timestamp` - int64
- `device_no` - uint32
- `thread` - uint32
- `value` - float64

Fixture semantics are:

- rows describe half-open intervals `[start_timestamp, end_timestamp)`
- `series_id` and `bin_duration` come from the hive-partitioned path
- `thread = 2^32 - 1` represents `system_wide`
- `bin_duration` uses the same units as the capture timestamps for that dataset

Fixture generation is intentionally scoped to these counter parquet tables. Tests in this iteration do not need to generate non-counter timeline regions or any other future timeline parquet layout.

### 1. Base Scan Views

Create one base view per `bin_duration`.

Each base view:

- reads the hive-partitioned parquet for one LoD only
- preserves the native compressed source columns
- performs no eager dense expansion
- is the only layer that touches parquet directly

This makes `bin_duration` a first-class dimension in the view topology rather than something hidden inside one generic abstraction.

### 2. Expanded Source Views

Create one expanded-source view per source per `bin_duration`.

These views may:

- expand compressed rows spanning multiple bins into one logical sample per covered bin for that `bin_duration`
- preserve source identity
- preserve raw dimensions needed for later chart SQL, such as `series_id`, `device_no`, and `thread`
- normalize source details into SQL-friendly shapes for downstream chart queries

These views may use source-specific internal columns such as `series_id`, `device_no`, and `thread`. Those details should not be the final chart contract, but they are valid inputs to the final chart SQL.

Bounded expansion belongs here. Base scan views stay compressed, but expanded-source views for a single `bin_duration` may expand half-open intervals into one row per visible bin so downstream chart SQL operates on correct per-bin samples. This expansion must remain confined to one LoD family and must not create a global dense table spanning all LoDs.

### 3. Final Chart SQL In Visualization Config

Create one final chart SQL definition per chart per `bin_duration` in the visualization config rather than as a separate SQLRenderer output view.

Each final chart SQL definition should be wide, not long. It should expose:

- one x-axis column, typically `x_start`
- one or more y-columns per plotted series or recipe-defined expression

Each final chart SQL definition must already be semantically chart-ready for its selected `bin_duration`. In particular, each output row must represent a real visible bin in that LoD, and any compressed source row spanning multiple bins must contribute to every covered bin before final aggregation into wide output columns. Because the selected `bin_duration` is known from the source family itself, `x_end` is not part of the default display contract; it is derivable as `x_start + bin_duration` when needed.

For this POC, the display contract is intentionally allowed to diverge from the source-loading and expansion contract. Source-loading and expanded-source views preserve gaps by omitting uncovered bins. The final presentation query may then choose how to render those gaps. `code_hotspots` currently zero-fills uncovered bins in its display SQL to match Streamline output, even though a future revision may switch that presentation layer back to `NULL` gaps.

This matches the current timeline visualization, which already supports arbitrary x/y column selection plus custom SQL in the visualization config.

Wide output is preferred because it:

- reduces row count compared with long `(series_key, value)` output
- avoids repeating a per-row series discriminator
- avoids metadata joins for normal rendering
- reduces transport cost from engine to GUI

### 4. Metadata

Metadata should not be duplicated into every sample row.

If needed, use small separate metadata views for chart- or series-level information. For the POC, static labels and units can remain in recipe visualization config when that is simpler.

## Query Contract

The final chart SQL contract is intentionally narrow:

- `x_start`
- y-series columns

The timeline visualization config maps those columns to plotted lines.

Internal aggregation and derivation belong in the final chart SQL, not in the timeline widget. If a chart means "sum over thread" or "sum over device and thread", the final chart SQL should already present that result.

Compressed source rows must not be interpreted directly by the timeline widget. If a source row spans multiple bins, the expansion layer must expand it within the selected `bin_duration` family so the final chart SQL reflects one row per actual bin, not one row per compressed interval start.

The final chart contract is dense over the visible bins for one selected `bin_duration`, even though the base representation remains compressed. Gaps remain absent in the source-loading and expansion layers. In the current POC, the final chart SQL for `code_hotspots` may render those uncovered bins as zero-valued display samples to match Streamline. That zero-fill behavior is presentation-only and must not be treated as changing the semantics of the underlying expanded-source data.

Recipe-defined expressions should appear as additional y-columns in the final chart SQL result. They are not dynamic GUI expressions in this design.

## Performance Rules

1. Never build a dense full-rank view at render time.
2. Treat `bin_duration` as a first-class part of the static view topology.
3. Keep parquet access in the base scan views only.
4. Keep base scan views compressed, but allow bounded expansion inside expanded-source views for the selected `bin_duration`.
5. Prefer wide chart outputs over long row-oriented series output.
6. Perform expensive aggregation once in final chart SQL, not in the timeline widget.
7. Keep explicit `x_start` time columns in final chart SQL results so a future templated wrapper can add visible-range predicates cleanly.
8. Keep expressions within a single `bin_duration` family so no hidden resampling is required.

## POC Shape

For the POC:

- all `bin_duration` families are initialized up front
- each chart uses one static final chart SQL definition in visualization config
- no GUI-driven LoD switching is required yet
- no GUI-driven time-range pushdown is required yet
- no render-param-driven re-rendering is required
- `code_hotspots` uses the shared timeline SQL utility to point at timeline parquet

This keeps the proof of concept aligned with the current timeline visualization while avoiding the wrong architectural dependency on minimal re-rendering.

## Future Extension Path

### Future LoD Selection

Later, the timeline config should be able to declare, per chart, the available data source/query pair for each `bin_duration`.

The GUI can then select the appropriate LoD-specific chart SQL automatically based on zoom level without rebuilding the whole render session.

### Future Time-Range Filtering

Later, the timeline query path should wrap the selected chart SQL in a templated query that applies a visible-range predicate.

Because the base representation stays compressed and LoD-specific, that filter can be pushed into the chosen view family instead of being hidden behind a dense abstraction. Once time-range templating exists, bounded expansion should also be limited to the visible range where possible.

### Future Series Filters

Later, where beneficial, the timeline layer may add template parameters for selected series or type-based filtering. That is optional and should be driven by measured value rather than assumed up front.

### Future Expressions

Recipe-defined expressions already fit this model as additional y-columns or helper query fragments at a single `bin_duration`.

If a future version needs more expression flexibility, it should compose from sibling expanded-source inputs within the same LoD family rather than reaching back into raw parquet ad hoc.

## Testing

The implementation should verify:

- the shared timeline SQL utility generates the expected renderer/view-family configuration for a supplied parquet path contract
- `SQLRenderer` initialization remains acceptable when all LoD view families are registered
- final chart SQL queries only access parquet partitions for their selected `bin_duration`
- compressed rows spanning multiple bins are expanded correctly within one selected `bin_duration` family
- expanded-source views do not invent uncovered bins within one selected `bin_duration` family
- final chart SQL applies the intended display-gap policy for the recipe, which is currently zero-fill for `code_hotspots`
- common chart shapes such as sum-over-thread and sum-over-device-and-thread remain performant on representative large runs
- recipe-defined derived columns align correctly within a single `bin_duration`
- final chart SQL results expose only the intended transport shape

Automated tests should be fixture-driven:

- generate synthetic parquet inputs with DuckDB SQL
- encode edge cases such as compressed spans, gaps, overlapping source rows, and derived-series inputs
- invoke the shared recipe-side utility or a minimal test recipe that wraps it
- initialize the real renderers and query the expanded-source tables or resolved final chart SQL

At minimum, the fixture set should include:

- a single-bin row for one series, one device, one thread
- a compressed multi-bin interval spanning more than one visible bin
- a gap between bins that should remain absent in expanded-source outputs and follow the recipe's display-gap policy in final chart SQL
- multiple rows for the same series/device/thread that require aggregation across adjacent bins
- multiple series participating in one derived-series or cross-series expression
- at least one `system_wide` row using `thread = 2^32 - 1`

Automated tests in this iteration should not depend on:

- running a real `code_hotspots` recipe
- invoking the APC-to-timeline-parquet executable
- validating end-to-end capture-time production of timeline parquet

## Open Decisions Deferred

- the future templating syntax for time-range and other query parameters
- the exact GUI policy for zoom-to-LoD selection
- whether any metadata should move from visualization config into separate metadata views
- whether some future data shapes justify a more generic timeline query abstraction
- the exact integration contract for running the external APC-to-timeline-parquet executable during `code_hotspots` execution

## Summary

The POC should use static multi-LoD `SQLRenderer` view families over compressed hive-partitioned parquet:

- implemented behind a shared recipe-side timeline SQL utility that `code_hotspots` calls
- base scan views per `bin_duration`
- expanded-source views per source per `bin_duration`, including bounded per-bin expansion where required for correct sample semantics
- final chart SQL per chart per `bin_duration` in visualization configs
- automated SQL tests driven by synthetic DuckDB-authored parquet fixtures rather than real recipe runs

This design prioritizes latency and large-data performance now, avoids global dense materialization, preserves filter pushdown, requires correct per-bin chart semantics within each selected LoD family, and leaves a clean path for future LoD switching, templated time-range querying, and later APC-to-timeline-parquet tool integration.
