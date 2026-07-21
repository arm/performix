<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 11: Fixed-Width SIMD on Wider-Capability Targets

## Problem Summary
- Insight target: fixed-width vector implementation that may underuse wider vector capability.

## ID
- `test_case_11`

## Public Intent (safe summary)
- Run a 16-bit image normalization pass (gain + black-level offset) with widening output.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- AArch64 path uses fixed-lane NEON vectors.
- The implementation does not scale vector width to target capability.
- On targets with wider-vector support (for example SVE-capable systems), throughput opportunity can be missed.

## What The LLM Should Suggest
- Identify fixed-width SIMD hotspot and potential width underuse.
- Suggest evaluating wider/scalable vector path as candidate action when target supports it.
- Suggest target-capability-aware validation rather than unconditional recommendation.
- It is acceptable to also suggest thread-level parallelism on multicore targets as a secondary optimization.

## Expected Profiling Characteristics
- Samples should land primarily in `run_image_normalization` transform and reduction loops.
- SIMD-style loop body should dominate source attribution.

## Scoring Guidance
- Pass:
  - Identifies hotspot and appropriately recommends width-aware vector-path evaluation.
  - May include threading/memory-throughput suggestions as secondary items without penalty.
- Fail:
  - Notes the hotspot but gives generic SIMD advice without width-awareness.
  - Or gives good secondary suggestions (for example threading) but weak width-awareness.
  - Or misses the primary issue or suggests an unrelated main fix.
