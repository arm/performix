<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 26: ASTL/ATX Re-Opens SCMI Sysfs Files

## Problem Summary
- Insight target: validate AI Insights on a real external telemetry workload where repeated file-open overhead dominates because ASTL re-opens the same SCMI sysfs files on every sample.
- This testcase is based on a real Performix investigation of ASTL/ATX telemetry collection before PR `#430` (`ea819ce`), using the pre-fix ASTL commit `24cf7e9`.

## ID
- `test_case_26`

## Public Intent (safe summary)
- Stage the real ASTL repository on the target at pre-fix commit `24cf7e9`.
- Build `atx` together with the ASTL mock SCMI sysfs generator.
- Launch `MockSysfs` and run `atx collect` against it using several mock telemetry metrics at a 5 ms interval.

## What's Wrong In Current Implementation
- In ASTL commit `24cf7e9`, `astl::FileInterface::Read` constructs a fresh `std::ifstream` on every read call.
- The SCMI collector therefore re-opens the same sysfs-backed telemetry files on every sampling interval instead of reusing already-open streams.
- Because `atx collect` polls the same small set of metrics frequently, file-open overhead becomes a first-order cost instead of one-time setup work.

## What The LLM Should Suggest
- Identify the repeated file-open path as a dominant hotspot, ideally naming `open`, `open64`, `openat`, `__libc_open64`, `__fopen_internal`, or `std::ifstream`/file-interface activity.
- Recognize that the workload is polling the same SCMI sysfs metric files over and over, so reopening them each sample is avoidable overhead.
- Suggest a realistic next step such as:
  - keeping the metric file descriptors open across samples,
  - caching/reusing file handles for stable sysfs paths,
  - or otherwise restructuring the polling loop so file-open work is not repeated every interval.
- Treat read cost and sleep cost as secondary context rather than the primary fix.

## Expected Profiling Characteristics
- Hot functions should include libc file-open and file-read activity, plus sleep.
- A representative profile should show significant time in ASTL SCMI collection code plus libc/C++ file-open and file-read symbols such as `__fopen_internal`, `open`, `openat`, `read`, and `__clock_nanosleep`.
- The source context should show ASTL file-interface read logic and SCMI collection code repeatedly reading a fixed set of metric files.

## Scoring Guidance
- Pass:
  - Identifies repeated file-open work as a dominant or primary hotspot.
  - Connects that hotspot to reopening the same telemetry/sysfs files on every sample.
  - Recommends retaining or reusing file handles/streams/descriptors across samples as the main fix.
- Fail:
  - Identifies file I/O overhead correctly, but focuses mostly on generic syscall reduction or batching without explicitly recommending keeping the file handles open.
  - Focuses mainly on sleep or generic loop overhead.
  - Misses the repeated open path.
  - Recommends unrelated algorithmic or memory optimizations.
