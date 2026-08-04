<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 13: Hot Function Built Without Release Optimization

## Problem Summary
- Insight target: hot-path function compiled without optimization due to target-specific build flags, despite overall Release-mode workflow.

## ID
- `test_case_13`

## Public Intent (safe summary)
- Run a telemetry feature-transform pass over a large float buffer.
- Use one shared output buffer; apply transform A to the first half and transform B to the second half each measured run.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- Two structurally equivalent transform functions are built from separate translation units.
- Both functions run every measured pass on disjoint halves of the same input/output buffers, so workload structure looks symmetric at source level.
- `feature_transform.cpp` is compiled with debug-style flags (`-O0 -g`) via CMake source-file properties, while `feature_transform_2.cpp` uses normal Release optimization.
- Main/test harness setup remains optimized; only one hot half-loop body is intentionally under-optimized.
- Runtime is higher than expected for equivalent code under normal Release optimization.

## What The LLM Should Suggest
- Identify `run_feature_transform` as the primary bottleneck and explain that
  it is slower than `run_feature_transform_2` despite the two functions having
  similar source structure and processing comparable inputs.
- Accept either of these root-cause formulations:
  - Build/configuration issue (file-level optimization suppression such as `-O0` on hot CU), or
  - Codegen outcome mismatch (hot transform remains scalar / not vectorized while the twin transform is vectorized).
- Recommend investigating or correcting how the hot translation unit is
  compiled, for example by checking its compiler options or vectorization
  report and restoring optimized/vectorized code generation.
- Establishing this source/codegen mismatch and recommending investigation or
  correction of compilation is sufficient for the expected result.

## Expected Profiling Characteristics
- Samples should skew heavily toward `run_feature_transform` (first half path) rather than `run_feature_transform_2` (second half path), despite near-identical source structure.
- Loop body should show dense scalar instruction activity with limited optimization effects.

## Scoring Guidance
- Pass with high confidence:
  - Establishes that the two functions have the same or structurally
    equivalent source-level operation over comparable inputs, rather than
    merely assuming that they might be equivalent.
  - Contrasts the scalar code generated for `run_feature_transform` with the
    vectorized code generated for `run_feature_transform_2` and recommends
    investigating or correcting how the hot function is compiled.
  - Statements that the functions have a similar structure and perform the
    same kind of per-element transform count as an explicit source-level
    comparison.
  - Meeting these criteria is sufficient for high confidence; additional
    remediation or validation detail does not affect the grade.
- Pass with medium confidence:
  - Correctly identifies and prioritizes the scalar-versus-vectorized codegen
    mismatch and gives a concrete fix, but leaves unestablished that the two
    functions perform structurally equivalent work.
  - Conditional wording such as "if they are mathematically equivalent"
    belongs in this grade because the source relationship remains unconfirmed.
  - A comparison of the generated instructions, or advice to apply the second
    function's vectorized structure, belongs in this grade when the response
    leaves the source relationship unconfirmed.
- Fail:
  - Finds the hotspot but gives generic SIMD or performance advice without
    connecting the codegen mismatch between the two transforms, or gives only
    vague or non-prioritized fixes.
  - Or misses the primary issue or suggests an unrelated main fix.
