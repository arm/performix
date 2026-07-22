<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 03: Missed CRC32C Specialization

## Problem Summary
- Insight target: scalar CRC32C bitwise loop where Arm CRC32C instruction path should be suggested.

## ID
- `test_case_03`

## Public Intent
- Run a byte-stream checksum kernel over a large buffer.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- CRC32C is implemented as scalar bit-by-bit polynomial update per byte.
- No use of architecture-native CRC32C instructions or optimized library path.
- Hot loop does high per-byte instruction work that specialized path can reduce.

## What The LLM Should Suggest
- Identify checksum loop as dominant hotspot.
- Suggest Arm CRC32C instruction-backed implementation or intrinsic/library path as candidate action.
- Suggest validating correctness and measuring before/after speedup against scalar baseline.

## Expected Profiling Characteristics
- Most samples should land in the checksum loop body and bit-update operations.
- Profile should indicate dominant time in generic byte-processing logic.

## Scoring Guidance
- Pass:
  - Recognizes checksum loop as hotspot and suggests Arm CRC32C instruction/library path as a candidate optimization.
- Fail:
  - Identifies the hotspot but gives only generic tuning suggestions.
  - Or misses the checksum hotspot or suggests an unrelated main fix.
