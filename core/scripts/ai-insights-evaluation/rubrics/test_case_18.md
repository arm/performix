<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 18: Java Polymorphic Hot Loop Dispatch

## Problem Summary
- Insight target: hot arithmetic loop performs virtual interface dispatch per element, limiting JIT inlining and optimization.

## ID
- `test_case_18`

## Public Intent (safe summary)
- Run a dynamic transform pipeline over large arrays.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- Every element chooses a `Kernel` object and calls `kernel.apply(...)` through an interface.
- The dispatch remains in the hottest loop body and can become megamorphic/polymorphic.
- This structure can inhibit inlining and vector-friendly optimization of the arithmetic.

## What The LLM Should Suggest
- Identify per-element polymorphic dispatch as a key overhead in the hot loop.
- Suggest restructuring dispatch outside the innermost loop (group by kernel, switch-based specialization, or separate passes).
- Suggest validating JIT inlining/codegen differences after reducing virtual dispatch frequency.

## Expected Profiling Characteristics
- Significant samples in `runPipeline` plus virtual-call/runtime dispatch helpers.
- Arithmetic work appears but is intermixed with dispatch overhead.

## Scoring Guidance
- Pass:
  - Identifies hot-loop virtual dispatch issue and recommends reducing per-element polymorphic calls.
- Fail:
  - Identifies the hotspot but only gives generic compile/runtime suggestions.
  - Or misses the dispatch issue or suggests an unrelated primary fix.
