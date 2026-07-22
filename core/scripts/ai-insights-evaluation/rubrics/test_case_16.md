<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 16: Java CRC32C Path Selection

## Problem Summary
- Insight target: checksum hot path uses a scalar bit-at-a-time CRC32C implementation instead of optimized runtime/library paths.

## ID
- `test_case_16`

## Public Intent
- Compute a deterministic checksum over a large byte stream.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The loop updates CRC32C one bit at a time for every byte.
- This keeps execution in scalar Java arithmetic for a workload that has optimized alternatives.
- The implementation does not use built-in CRC facilities that can map to optimized code paths.

## What The LLM Should Suggest
- Identify the checksum loop as the dominant hotspot.
- Suggest replacing scalar bitwise CRC with `java.util.zip.CRC32C` or an equivalent optimized API/provider path.
- Suggest validating runtime/provider behavior on the target JDK and architecture.

## Expected Profiling Characteristics
- Most samples should land in `runChecksum`/`updateChecksum`.
- The inner bit loop should dominate source attribution.

## Scoring Guidance
- Pass:
  - Identifies scalar CRC loop and recommends `CRC32C`/optimized checksum path.
- Fail:
  - Identifies the hotspot but suggests only generic loop tuning.
  - Or misses the checksum-path issue or suggests an unrelated primary fix.
