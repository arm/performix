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
- Identify `run_feature_transform` as the primary bottleneck and explain why it is slower than `run_feature_transform_2` despite similar structure.
- Accept either of these root-cause formulations:
  - Build/configuration issue (file-level optimization suppression such as `-O0` on hot CU), or
  - Codegen outcome mismatch (hot transform remains scalar / not vectorized while the twin transform is vectorized).
- Recommend concrete remediation:
  - restore optimized codegen for the hot CU, and/or
  - make the hot transform generate vectorized code (compiler settings and loop-shape changes).
- Suggest validating impact by comparing profile/runtime before and after the change.

## Expected Profiling Characteristics
- Samples should skew heavily toward `run_feature_transform` (first half path) rather than `run_feature_transform_2` (second half path), despite near-identical source structure.
- Loop body should show dense scalar instruction activity with limited optimization effects.

## Scoring Guidance
- Pass:
  - Correctly identifies that the hot transform is materially under-optimized relative to its twin and gives a concrete fix path (restore optimization flags and/or achieve comparable vectorized codegen).
- Fail:
  - Finds the hotspot and suggests SIMD/perf improvements but does not clearly connect the mismatch between transform 1 vs transform 2, or gives vague or non-prioritized fixes.
  - Or misses the primary issue or suggests an unrelated main fix.
