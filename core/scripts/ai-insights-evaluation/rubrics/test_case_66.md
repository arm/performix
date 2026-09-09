<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 66: Separated Per-Worker Statistics Control

## Problem Summary
- Negative Cache Sharing control: independently updated per-worker counters
  already occupy separate cache lines.

## ID
- `test_case_66`

## Public Intent (safe summary)
- Have several pinned workers update independent statistics counters whose
  storage is separated by worker.

## What's Wrong In Current Implementation
- The accepted fixture contains no diagnosis-worthy application cache-sharing
  problem. Isolated samples or hot runtime/library functions do not prove
  otherwise.

## What The LLM Should Suggest
- State that the cache-line ranking does not provide enough `sample_count` and
  `sample_pct` evidence of true or false sharing in the application.
- Avoid recommending padding or alignment because each `WorkerState` is already
  64-byte aligned and sized.
- If more confidence is needed, recommend a representative repeat or improved
  attribution rather than inventing a cause. General tuning ideas must be
  clearly labelled as unproven and secondary.

## Expected Profiling Characteristics
- No application-attributed line has both a meaningful `sample_count` and a
  non-negligible `sample_pct` in the overall run. Top-ranked but negligible
  residual lines do not justify a diagnosis.
- Any residual sampled lines are negligible or attributable to lifecycle,
  runtime or library activity rather than adjacent per-worker counters.

## Scoring Guidance
- Pass:
  - Reports the absence of a supported application diagnosis and avoids an
    unnecessary layout change.
- Fail:
  - Diagnoses false or true sharing from a hot function, a negligible line or a
    thread count alone.
  - Recommends padding or aligning the already separated per-worker counters as
    the main fix.
  - Claims that absence of a reported application line proves zero coherence
    traffic or guarantees optimal performance.
