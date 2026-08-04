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

## Required For Pass
A response must:
- Identify the scalar, bit-at-a-time checksum loop as the dominant hotspot.
- Recommend replacing it with an optimized CRC32C implementation. Acceptable
  recommendations include an existing library API, a hardware-accelerated
  CRC32C path, or a table-driven/slicing implementation.

The following are useful additions, but are not required for a pass:
- Validating that the chosen .NET/runtime path maps to optimized code on the
  target architecture.
- Adding compatibility tests or benchmarks for the replacement.

## Expected Profiling Characteristics
- Most samples should land in `RunChecksum`/`UpdateChecksum`.
- The inner bit loop should dominate source attribution.

## Scoring Guidance
- Pass with high confidence:
  - Meets both requirements in `Required For Pass`.
  - Recommends a concrete, existing high-level library API which accepts a
    buffer or supports incremental checksum input and handles CRC32C with
    compatible parameters. Valid .NET examples are the static, destination
    buffer, and incremental `System.IO.Hashing.Crc32` APIs with
    `Crc32ParameterSet.Crc32C`. This example is not exhaustive; accept another
    valid high-level CRC32C library API.
- Pass with medium confidence:
  - Meets both requirements in `Required For Pass`, but recommends only a
    generic hardware-accelerated or table-driven/slicing implementation without
    identifying an existing CRC32C library API.
  - `System.Numerics.BitOperations.Crc32C` is medium rather than high because it
    processes at most one `ulong` per call, leaving the application to iterate
    over the buffer and handle the initial value and final XOR.
  - Direct use of architecture-specific hardware intrinsics is medium rather
    than high because the application must still provide fallback and checksum
    orchestration logic.
  - Do not fail solely because the response omits one of the useful additions.
- Fail:
  - Identifies the hotspot but suggests only generic loop tuning.
  - Or misses the checksum-path issue or suggests an unrelated primary fix.
  - Or recommends an incompatible CRC variant. In particular, the default
    `System.IO.Hashing.Crc32` APIs use the IEEE CRC-32 polynomial; a response
    must specify `Crc32ParameterSet.Crc32C` when recommending this class.
