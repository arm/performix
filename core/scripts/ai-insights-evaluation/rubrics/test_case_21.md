<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 21: RocksDB CRC32C db_bench (negative)

## Problem Summary
- Insight target: validate AI Insights on a larger external workload where the dominant hotspot is real, obvious, and already specialized.
- This is intentionally a negative testcase for "recommend the thing that is already present".

## ID
- `test_case_21`

## Public Intent (safe summary)
- Build RocksDB on the target and run `db_bench` with `--benchmarks=crc32c,crc32c,crc32c,crc32c,crc32c --block_size=500000000 --threads=8`.
- Use this testcase to exercise larger-codebase and external-source workflow support.

## What's Wrong In Current Implementation
- The dominant hotspot is expected benchmark work: CRC32C over a very large buffer.
- The hot CRC path already appears to be Arm-specialized and substantially optimized:
  - RocksDB uses `crc32c_arm64.cc`
  - hot source includes `arm_acle.h`
  - hot disassembly includes `crc32cx`
  - the source shows a 3-way parallel / folded CRC structure

## What The LLM Should Suggest
- Explicitly acknowledge that the CRC32C path is hot because the benchmark is intentionally exercising it.
- Explicitly note that the CRC implementation already appears specialized / optimized for Arm, rather than recommending "use hardware CRC", "use intrinsics", or similar advice that is already present.
- Focus any recommendations on:
  - benchmark design / whether this hotspot is expected,
  - surrounding overheads such as buffer setup, memset, allocation, or syscall churn,
  - measurement follow-up to confirm whether there is meaningful headroom left in the CRC path.
- It is acceptable to say that this hotspot may already be close to the intended implementation, and that the more useful action is to optimize or control the secondary costs around it.

## Expected Profiling Characteristics
- The CRC path should dominate the profile.
- Hot source and disassembly should clearly show Arm-specific CRC instructions and intrinsics.
- Secondary costs may include large-buffer memset, allocation/unmap, and benchmark harness overhead.

## Scoring Guidance
- Pass:
  - Recognizes that the benchmark is intentionally CRC-heavy.
  - Recognizes that the hot CRC path already appears specialized / optimized.
  - Avoids recommending the same class of optimization that is already visible in the profile.
  - Shifts attention to secondary overheads, benchmark interpretation, or realistic follow-up validation.
- Fail:
  - Correctly identifies the CRC hotspot and some real secondary costs, but still gives redundant advice about optimizing the CRC implementation further without enough evidence.
  - Treats the result as a generic "CRC is hot, therefore use CRC hardware / intrinsics / Arm-specific implementation" case.
  - Misses the fact that the implementation already appears specialized.
