<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 63: Compact Per-Worker Statistics

## Problem Summary
- Independent per-worker counters occupy different offsets in one cache line,
  causing false sharing.

## ID
- `test_case_63`

## Public Intent (safe summary)
- Have several pinned workers update their own statistics counters while doing
  deterministic request-processing work.

## What's Wrong In Current Implementation
- Each worker updates its own element of `WorkerStates::values`, but all
  elements are packed into one 64-byte `WorkerStates` object.

## What The LLM Should Suggest
- Connect the material false-sharing line, its distinct offsets and its sampled
  accesses to `update_worker_statistics` and the compact counter layout.
- Separate active worker counters onto different cache lines, for example with
  a 64-byte-aligned and 64-byte-sized per-worker slot. Accumulating locally and
  publishing less often is also a useful option.
- Treat the classification and samples as profiling evidence, not an exact
  transfer count or measured latency.

## Expected Profiling Characteristics
- A material application cache line is classified as `FALSE` sharing.
- Several distinct offsets on that line receive samples, consistent with
  independent workers updating adjacent counters.
- Material attribution points to `update_worker_statistics` and its source.

## Scoring Guidance
- Pass:
  - Gives the evidence-backed diagnosis and an appropriate layout or batching
    recommendation described above.
- Fail:
  - Calls the line true sharing or treats the counters as one logically shared
    value.
  - Suggests only generic cache tuning without connecting the issue to layout.
  - Invents elapsed-time impact, transfer counts or another unsupported cause.
