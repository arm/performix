<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 20: Java Heap-Constrained Cache Churn

## Problem Summary
- Insight target: the workload runs with a fixed 2 GB heap on a machine with
  substantially more memory available. Under that constrained heap, the decode
  cache cannot retain enough of the hot working set, so the application spends
  much more time rebuilding decoded chunks and in associated GC/runtime work.

## ID
- `test_case_20`

## Public Intent (safe summary)
- Decode a large logical set of chunks from a byte stream.
- Reuse decoded chunks through an in-process cache.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The cache capacity is derived from the effective JVM max heap, so a small
  heap gives the application a much smaller effective cache.
- The access pattern repeatedly revisits a hot chunk set larger than the cache
  that fits under the constrained heap setting.
- With `-Xms2g -Xmx2g`, the workload rebuilds many decoded chunks instead of
  reusing them, which amplifies both application-side recomputation and
  GC/runtime overhead.
- The target machine has materially more RAM available, so increasing the heap
  would allow the cache to retain more of the hot set and avoid much of that
  churn.

## What The LLM Should Suggest
- Identify the application-level cache churn / chunk rebuild path as the main
  hotspot.
- Recommend validating a larger heap on the same workload and hardware
  (for example `-Xmx4g` or higher, and likely increasing `-Xms` as well).
- Optionally suggest secondary mitigations such as reducing rebuild cost,
  shrinking the hot working set, or improving cache effectiveness.

## Expected Profiling Characteristics
- Significant samples in `Main::runPass` and `Main::buildChunk`.
- Under the constrained heap, visible time in GC/runtime worker paths is
  expected because the application is allocating and discarding many decoded
  chunks.
- When the heap is increased, runtime should drop substantially and the amount
  of rebuild/GC pressure should reduce.

## Scoring Guidance
- Pass:
  - Identifies that the constrained heap leaves the cache too small for the
    hot working set and recommends validating a larger heap.
- Fail:
  - Identifies the rebuild hotspot but frames it only as generic allocation or
    compute overhead, without the heap-sizing recommendation.
  - Or misses the cache/rebuild issue, or suggests an unrelated primary fix.
