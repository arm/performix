<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 07: Call-Return Overhead in Hot Loop

## Problem Summary
- Insight target: excessive call-return overhead from tiny helper calls inside a hot loop.

## ID
- `test_case_07`

## Public Intent (safe summary)
- Run a two-stage scalar transform loop over a large float buffer.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- Tiny helper function calls are out-of-line in the hot path (implemented in a separate translation unit).
- Each element performs multiple function calls with very small per-call work.
- Call overhead becomes a meaningful fraction of runtime.

## What The LLM Should Suggest
- Identify helper call overhead in the hot loop as the main issue.
- Suggest reducing call frequency (inline/merge helper logic, restructure loop body) as candidate action.
- Suggest confirming benefit with before/after profile comparison.

## Expected Profiling Characteristics
- Most samples should land in `kernel_8` and `helper_8`.
- Call-heavy hotspot should be visible in call stacks/source attribution.

## Scoring Guidance
- Pass:
  - Correctly attributes hotspot to call-heavy loop shape and suggests concrete call-overhead reductions.
- Fail:
  - Finds the hotspot but gives only generic suggestions.
  - Or misses the primary issue or suggests an unrelated main fix.
