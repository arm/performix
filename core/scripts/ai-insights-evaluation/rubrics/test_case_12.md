<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 12: Unoptimized Dense Matrix Multiplication

## Problem Summary
- Insight target: unoptimized dense matrix multiplication in the projection kernel.

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
- Suggest a concrete optimized implementation, such as a BLAS library, tiling
  or register blocking that reuses accumulators across inner-loop iterations.
  These are examples, not required names; generic SIMD advice is insufficient.
- Benchmarking against the naive baseline is useful validation advice, not a
  mandatory phrase when the proposed optimization is concrete and qualified.

## Expected Profiling Characteristics
- Most samples should land in `run_dense_projection` nested loops.
- Hotspot should reflect heavy arithmetic throughput demand.

## Scoring Guidance
- Explicit correctness-check advice is useful but not required. Accept recommendations
  conditional on preserving the required numerical semantics.
- Pass:
  - Recognizes the matrix-multiplication hotspot and recommends a concrete
    optimized implementation or experiment, without claiming an unmeasured gain
    as established.
- Fail:
  - Identifies the hotspot but gives generic tuning-only advice.
  - Or misses the primary issue or suggests an unrelated main fix.
  - Or recommends materially changing required output semantics without qualification.
