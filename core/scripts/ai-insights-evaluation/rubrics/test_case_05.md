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
- The test fixture keeps compiler vectorization disabled so this behaviour is
  stable across toolchains. This build detail is not required in the response.

## What The LLM Should Suggest
- Identify arithmetic hot loop as primary optimization target.
- Suggest an actionable SIMD/vectorization improvement appropriate for a
  regular arithmetic loop, such as enabling compiler vectorization,
  restructuring a reduction or aliasing constraint, using a SIMD directive,
  or implementing the loop with vector instructions.
- Compiler vectorization diagnostics and assembly inspection are useful
  verification steps, but exact flags or commands are not required.

## Expected Profiling Characteristics
- Most samples should land in `kernel_5`.
- Hot region should appear as arithmetic-heavy, uniform loop body.

## Scoring Guidance
- Pass:
  - Identifies the dominant scalar arithmetic loop and recommends an
    actionable SIMD/vectorization improvement.
  - The response does not need to identify the test fixture's exact compiler
    flag.
- Fail:
  - Identifies the hotspot but gives only generic advice without recommending
    SIMD/vectorization.
  - Or misses the hotspot or suggests an unrelated main fix.
