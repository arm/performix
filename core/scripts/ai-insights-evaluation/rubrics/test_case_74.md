<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 74: Java CRC32 Intrinsics Disabled

## Problem Summary
- CRC32 calculations take a large share of CPU time. A JVM flag disables its
  built-in CRC32 optimisation on a target where enabling it makes the workload
  faster.

## ID
- `test_case_74`

## Public Intent
- Repeatedly calculate CRC32 checksums over the same fixed byte buffers.
- Report processed bytes, elapsed time and a repeatable checksum.

## What's Wrong In Current Configuration
- The application already uses `java.util.zip.CRC32`, but the JVM is launched
  with `-XX:+UnlockDiagnosticVMOptions -XX:-UseCRC32Intrinsics`.
- The recorded `UseCRC32Intrinsics=false` setting confirms that the JVM's
  built-in CRC32 optimisation is disabled. Suggesting the same CRC32 class
  again does not fix that setting.
- The buffers and CRC32 object are reused. The issue is the cost of calculating
  checksums, not repeatedly creating objects or keeping unneeded data.

## What The LLM Should Suggest
- Use CPU samples to show that CRC32 calculations take a large share of CPU
  time. Use source, call stacks or disassembly to connect the application's
  CRC32 calls to where time is spent. Exact JVM function names are not required.
- Use the recorded `UseCRC32Intrinsics=false` setting to support the
  recommendation. Quoting the captured value or its `Origin` is optional.
- Make enabling `-XX:+UseCRC32Intrinsics` or removing the disabling option the
  main recommendation. Explain that this allows the JVM to use its built-in
  CRC32 optimisation. Other CRC32 code may have its own optimisations even
  when this flag is disabled.

## Expected Profiling Characteristics
- CPU samples should point to CRC32 calculations in Java or library functions.
  The function names can vary between JDKs.
- Before using a recording for this test, confirm that it contains
  `UseCRC32Intrinsics=false` and its origin. Run the workload with the flag
  enabled on the same target to check that it is supported and improves
  processing speed while producing the same checksum.

## Scoring Guidance
- Pass:
  - Connects the measured CRC32 cost to the disabled flag and makes
    enabling it or removing the disabling option the main recommendation.
    Quoting the captured value or its `Origin` is optional.
- Fail:
  - Omits the flag change, gives only general loop or vectorisation advice,
    suggests switching to the CRC32 class already in use, or replaces the flag
    change with unsupported heap, GC or upgrade advice.
  - Recommends CRC32C instead of CRC32, which produces a different checksum.
  - Invents the flag value or how it was set, or uses settings from another JVM.
    Omitting the captured value or `Origin` alone is not a failure.
  - Claims the flag is enabled, assumes all other CRC32 code is unoptimised,
    or guarantees a speedup without evidence.
