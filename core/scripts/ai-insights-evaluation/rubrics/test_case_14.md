<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 14: Java 11 Runtime Baseline on AArch64

## Problem Summary
- Insight target: workload is constrained to OpenJDK 11 runtime baseline, which may miss newer AArch64 JIT/vectorization improvements available in newer JDKs.

## ID
- `test_case_14`

## Public Intent (safe summary)
- Run a dense arithmetic transform over two float arrays in Java.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The benchmark launcher pins execution to Java 11 runtime.
- The hot loop is a straightforward arithmetic loop where newer JDK releases may generate better AArch64 code.
- Keeping Java 11 baseline can leave target-specific JIT/codegen improvements unused.

## What The LLM Should Suggest
- Identify the transform loop as the dominant hotspot.
- Suggest validating performance on newer JDK baselines (for example 17/21+) on the same hardware.
- Suggest comparing generated code/profile before and after JDK upgrade, while preserving correctness.

## Expected Profiling Characteristics
- Most samples should land in `runTransform` and JIT-generated arithmetic hot paths.
- The arithmetic loop body should dominate source attribution.

## Current Profiling Limitation
- Performix does not currently expose enough JVM runtime metadata in the profiling context to let the LLM reliably infer the pinned Java runtime version from the run alone.
- In particular, the current profile/context does not surface the full runtime paths needed to identify the exact JDK in use.
- This limitation means the expected JDK-baseline insight is not fully grounded in currently exposed profile evidence.
- Jira: `APAP-4118` tracks exposing the full paths needed to support this diagnosis.

## Scoring Guidance
- Pass:
  - Identifies runtime/JDK-baseline limitation and recommends validating newer JDK on same workload/target.
- Fail:
  - Identifies the hotspot but gives generic Java tuning without the JDK-baseline upgrade insight.
  - Or misses the primary issue or suggests an unrelated main fix.
