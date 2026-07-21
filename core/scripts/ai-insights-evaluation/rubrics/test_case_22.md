<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 22: RocksDB FastLocalBloom filter_bench on Arm

## Problem Summary
- Insight target: validate AI Insights on a real RocksDB filter-query workload where the codebase has an explicit x86 AVX2 optimization path but no equivalent Arm SIMD path.
- This should exercise a realistic architecture-gap diagnosis rather than a synthetic scalar loop.

## ID
- `test_case_22`

## Public Intent (safe summary)
- Build RocksDB on the target and run `filter_bench` with `-impl=1 -quick -m_keys_total_max=200 -use_full_block_reader -runs=3`.
- Use this testcase to exercise an external workload whose hot path is the FastLocalBloom query implementation.

## What's Wrong In Current Implementation
- `util/bloom_impl.h` explicitly documents:
  - "SIMD-optimized query performance (currently using AVX2 on Intel)"
- The FastLocalBloom query path contains AVX2-specific code under `#ifdef __AVX2__`, while the non-AVX2 path falls back to a scalar probe loop.
- On Arm, this benchmark should therefore spend time in the scalar `HashMayMatchPrepared` path for FastLocalBloom queries, even though the codebase already has an x86 SIMD implementation for the same algorithmic work.

## What The LLM Should Suggest
- Identify the Bloom/filter query path as the main hotspot, ideally naming FastLocalBloom or `bloom_impl.h`.
- Recognize that the codebase contains an x86-specific SIMD optimization path (AVX2) but no equivalent Arm-specific SIMD path is visible in the hot implementation.
- Suggest a realistic next step such as:
  - adding an Arm NEON/SVE implementation for the query path,
  - evaluating whether the filter query loop can be vectorized on Arm,
  - or assessing whether Ribbon or another filter implementation is a better fit on Arm for this workload.
- Avoid generic advice that does not engage with the architecture-specific gap.

## Expected Profiling Characteristics
- Hot functions/source/disassembly should center on RocksDB's Bloom/filter implementation.
- The source should expose `FastLocalBloomImpl` / `HashMayMatchPrepared` / related filter reader code in `util/bloom_impl.h` or `table/block_based/filter_policy.cc`.
- On Arm, the profile should show the scalar fallback path rather than any x86 AVX2 query path.

## Scoring Guidance
- Pass:
  - Identifies the Bloom/filter query path as the dominant or primary hotspot.
  - Explicitly recognizes an architecture-specific optimization gap, specifically that x86 has an AVX2 path while Arm lacks an equivalent specialized path here.
  - Recommends a concrete parity fix such as adding or porting an Arm NEON/SVE implementation for the same hot path, or clearly recommends an alternative filter strategy such as Ribbon.
- Fail:
  - Correctly identifies the filter hotspot, but gives only generic loop/vectorization advice without clearly linking it to the x86-vs-Arm implementation gap, or mentions the gap without clearly recommending either an Arm-specific implementation or an alternative filter strategy such as Ribbon.
  - Misses the filter path entirely.
  - Gives only generic benchmark/setup advice.
  - Suggests optimizations that are unrelated to the actual hot code path.
