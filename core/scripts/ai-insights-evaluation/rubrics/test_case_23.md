<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 23: RocksDB XXH3 Hash Upgrade on Arm

## Problem Summary
- Insight target: validate AI Insights on a real RocksDB hash-performance improvement where `util/xxhash.h` was upgraded and the commit explicitly noted better performance on some Arm machines.
- This is intended as a positive Arm-specific testcase: the benchmark makes `XXH3` hashing the dominant cost, and the model should recognize that the hot loop is already vectorized but still a plausible target for a more optimized Arm implementation on an SVE-capable system.

## ID
- `test_case_23`

## Public Intent (safe summary)
- Build the historical RocksDB revision immediately before commit `fd911f965` (`Upgrade xxhash.h to latest dev`) and run the built-in `db_bench` checksum microbenchmark:
  - `--benchmarks=xxh3[X20]`
  - `--threads=1`
  - `--block_size=4096`
- This should expose an older `XXH3` implementation in `util/xxhash.h` before the upstream upgrade that reported improved performance on some Arm systems.

## What's Wrong In Current Implementation
- RocksDB vendors `xxhash.h` and uses `XXH3` in several checksum/hash paths.
- Commit `fd911f965` updated RocksDB to a newer upstream `xxhash.h` and explicitly stated that it should improve performance on some Arm machines.
- By pinning the revision immediately before that upgrade, this testcase should expose an older `XXH3` implementation whose hashing throughput is weaker on Arm than the later upgraded version.
- The expected issue is therefore not DB structure or I/O, but that the hot path is an older vendored hash implementation that can plausibly be improved either by upgrading to a newer Arm-friendlier `xxhash.h` implementation or by further Arm-specific optimization of the current code.

## What The LLM Should Suggest
- Identify `XXH3` hashing as the dominant hotspot.
- Recognize this as a native hash implementation issue rather than a storage-engine, memtable, or syscall problem.
- Notice that the hot loop is already vectorized on AArch64, so generic advice like "vectorize this" is not enough.
- If the disassembly/source suggests the current path is still using NEON/ASIMD on an SVE-capable system, treat that as a strong hint that a more optimized Arm implementation may exist or be worth evaluating.
- Recommend an Arm-aware improvement direction such as:
  - upgrade to a newer `xxhash.h` / newer RocksDB revision containing improved `XXH3`
  - validate whether a newer revision selects a better Arm implementation on the same benchmark
  - or investigate a more optimized Arm-specific implementation/build of the current code path (for example, a better SVE-capable or target-tuned implementation)

## Observed Profiling Characteristics
- The source/disassembly should expose `util/xxhash.h` and/or `XXH3_*` symbols as the main work.
- The top functions should be overwhelmingly dominated by `XXH3` routines, with little or no RocksDB storage-engine logic involved.
- The signal should look like a hash microbenchmark:
  - hot `XXH3` code
  - little filesystem, comparator, or memtable noise
- The generated code may already be vectorized with AArch64 SIMD instructions, but the target is SVE-capable. A strong answer may distinguish "already vectorized" from "already maximally optimized for this CPU."

## Scoring Guidance
- Pass:
  - Correctly identifies `XXH3` hashing as the main hotspot.
  - Treats it as a hash implementation / vendored-runtime issue rather than generic DB tuning.
  - Recognizes that the current implementation is already vectorized, so the next step is not generic SIMD advice but a more specific Arm-aware improvement.
  - Recommends a realistic Arm-aware remediation such as:
    - upgrading to a newer `xxhash.h` / RocksDB revision,
    - validating a newer Arm-optimized `XXH3` path,
    - or investigating a more optimized Arm implementation of the current code path on this SVE-capable system.
  - Explicitly naming the vendored `xxhash.h` / RocksDB upgrade is a strong answer, but it is not required if the response clearly recommends an equivalent Arm-aware implementation or validation path.
- Fail:
  - Focuses on irrelevant storage-engine concerns (memtable, compaction, I/O, locks) instead of the hash benchmark itself.
  - Gives only vague "optimize hashing" or "vectorize this" advice without acknowledging that the current path is already vectorized.
  - Claims the hash path is already fully optimized and no further Arm-aware investigation is warranted.
  - Misses the opportunity to suggest any credible Arm-aware next step, such as a newer implementation/library revision or a more optimized Arm-specific version of the current code.
