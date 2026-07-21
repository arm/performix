<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 01: Missing Vectorization

## Problem Summary
- Insight target: hot loop with branch/control-flow and scalar accumulation shape that should trigger SIMD/vectorization-oriented guidance.

## ID
- `test_case_01`

## Public Intent (safe summary)
- Run one element-wise transform over a large `float` array.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The hot loop performs per-element control-flow selection (`x > 0`) in the main transform path.
- The loop accumulates using `std::abs(y)`, introducing per-iteration scalar dependency.
- The implementation is single-threaded and dominated by one large loop.

## What The LLM Should Suggest
- Identify the dominant loop hotspot and tie it to the transform body.
- Suggest branch-friendly/SIMD-friendly reformulation of the transform path.
- Optionally suggest candidate build/target tuning and verification steps (for example vectorization report/asm check), labeled as candidate actions.

## Expected Profiling Characteristics
- Most samples should land in `kernel_1`.
- One dominant loop hotspot should be visible.

## Scoring Guidance
- Pass:
  - Diagnoses vectorization/SIMD opportunity in the hot loop and gives concrete code-level suggestions.
- Fail:
  - Notes the hotspot but gives only generic optimization advice.
  - Or misidentifies the primary bottleneck or suggests an unrelated main fix.
