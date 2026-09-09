<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 65: Worker And Shared Request Accounting

## Problem Summary
- Compact independent worker counters cause false sharing, while a separate
  process-wide total causes true sharing.

## ID
- `test_case_65`

## Public Intent (safe summary)
- Record each worker's completed requests while also maintaining a shared total.

## What's Wrong In Current Implementation
- `WorkerStatistics::completed` packs independent per-worker atomics into one
  cache line, while every worker updates the separate
  `RequestTotal::completed` atomic.
- These are two distinct causes and require different layout and accounting
  changes.

## What The LLM Should Suggest
- Connect the false-sharing line and its multiple offsets to
  `update_worker_statistics`. Recommend separated per-worker slots or local
  accumulation.
- Connect the true-sharing line, shared offset and evidence of multiple
  participating writers to `update_request_total`. Recommend batching,
  sharding or periodic aggregation.
- Do not claim that padding the process-wide total removes contention on the
  same logical value.

## Expected Profiling Characteristics
- The accepted recording contains two diagnosis-worthy application lines. Each
  has a meaningful `sample_count` and a non-negligible `sample_pct` in the
  overall run. Merely being top-ranked within a sparse classification does not
  qualify.
- One line is classified as `FALSE` sharing and has several implicated byte
  offsets. A separate line is classified as `TRUE` sharing and is concentrated
  at the shared total's offset.
- Drilldown and source evidence distinguish `update_worker_statistics` from
  `update_request_total`.

## Scoring Guidance
- Pass:
  - Distinguishes both causes, attributes them correctly and proposes an
    appropriate separate fix for each.
- Fail:
  - Reports only one diagnosis-worthy line when both satisfy the criteria
    above.
  - Swaps the classifications or function attribution.
  - Applies padding alone to the truly shared total or gives one generic fix for
    both lines.
