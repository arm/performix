<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 12: Missed Optimized Library Path

## Problem Summary
- Insight target: dense matrix projection where optimized math library usage is the key opportunity.

## ID
- `test_case_12`

## Public Intent (safe summary)
- Run a dense feature-projection kernel (equivalent to GEMM-style multiply).
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- Matrix projection is implemented as direct triple nested loops.
- No blocked/tiled algorithm and no optimized BLAS-style library path is used.
- Kernel leaves substantial performance on the table on modern server CPUs.

## What The LLM Should Suggest
- Identify matrix multiply hotspot and algorithmic/library opportunity.
- Suggest evaluating optimized BLAS/library path (and candidate Arm-optimized library when available).
- Suggest benchmarking against the naive baseline with equivalent correctness checks.

## Expected Profiling Characteristics
- Most samples should land in `run_dense_projection` nested loops.
- Hotspot should reflect heavy arithmetic throughput demand.

## Scoring Guidance
- Pass:
  - Recognizes GEMM-style hotspot and suggests concrete optimized-library or blocked-kernel direction.
- Fail:
  - Identifies the hotspot but gives generic tuning-only advice.
  - Or misses the primary issue or suggests an unrelated main fix.
