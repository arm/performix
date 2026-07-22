<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 19: Java Parallel Task Granularity

## Problem Summary
- Insight target: fork-join decomposition uses very small chunk size, creating excessive scheduling overhead in a throughput kernel.

## ID
- `test_case_19`

## Public Intent (safe summary)
- Run a parallel array transform with fork-join decomposition.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- `CHUNK_SIZE` is fixed at a very small value (`256`) for a large data set.
- The recursion creates many tiny tasks relative to useful per-task compute.
- Scheduler/queue overhead can dominate and limit scaling.

## What The LLM Should Suggest
- Identify task granularity as the likely bottleneck.
- Suggest increasing chunk size and/or flattening decomposition strategy.
- Suggest validating speedup vs thread count/chunk size to find balanced granularity.

## Expected Profiling Characteristics
- Significant samples in fork-join framework scheduling/task machinery.
- Transform math is present but diluted by parallel runtime overhead.

## Scoring Guidance
- Pass:
  - Identifies too-fine task granularity and recommends larger chunks/coarser partitioning.
- Fail:
  - Identifies the hotspot but gives only generic parallel tuning.
  - Or misses the granularity issue or suggests an unrelated primary fix.
