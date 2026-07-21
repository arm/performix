<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 28: C# CRC32C Path Selection

## Problem Summary
- Insight target: checksum hot path uses a scalar bit-at-a-time CRC32C implementation instead of optimized runtime/library paths.

## ID
- `test_case_28`

## Public Intent
- Compute a deterministic checksum over a large byte stream.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The loop updates CRC32C one bit at a time for every byte.
- This keeps execution in scalar managed arithmetic for a workload that has optimized alternatives.
- The implementation does not use built-in CRC facilities that can map to optimized runtime or library code paths.

## What The LLM Should Suggest
- Identify the checksum loop as the dominant hotspot.
- Suggest replacing the scalar bitwise CRC with an optimized API such as `System.IO.Hashing.Crc32C` or another runtime/library-provided checksum path.
- Suggest validating that the chosen .NET/runtime path maps to optimized code on the target architecture.

## Expected Profiling Characteristics
- Most samples should land in `RunChecksum`/`UpdateChecksum`.
- The inner bit loop should dominate source attribution.

## Scoring Guidance
- Pass:
  - Identifies the scalar CRC loop and recommends an optimized `CRC32C` path or equivalent built-in checksum implementation.
- Fail:
  - Identifies the hotspot but suggests only generic loop tuning.
  - Or misses the checksum-path issue or suggests an unrelated primary fix.
