<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 05: Scalar Arithmetic SIMD Candidate

## Problem Summary
- Insight target: scalar arithmetic loop that is a clean SIMD candidate (no branch/control-flow confounders).

## ID
- `test_case_05`

## Public Intent (safe summary)
- Run a dense arithmetic transform over two float arrays.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The hot loop is scalar despite regular branch-free arithmetic structure.
- No explicit strategy is used to ensure high SIMD utilization.
- Per-element math dominates runtime in one contiguous loop.
- The kernel implementation translation unit is compiled with vectorization disabled (`-fno-tree-vectorize`) to keep this behavior stable across toolchains.

## What The LLM Should Suggest
- Identify arithmetic hot loop as primary optimization target.
- Suggest SIMD/vectorization-focused improvements appropriate for regular arithmetic loops.
- Suggest candidate compiler/target tuning and verification steps (vectorization diagnostics/asm) with evidence labeling.

## Expected Profiling Characteristics
- Most samples should land in `kernel_5`.
- Hot region should appear as arithmetic-heavy, uniform loop body.

## Scoring Guidance
- Pass:
  - Identifies the arithmetic loop as dominant and suggests SIMD-friendly/per-core improvements.
- Fail:
  - Identifies the hotspot but gives only generic advice.
  - Or misses the hotspot or suggests an unrelated main fix.
