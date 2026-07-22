<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 17: Java Atomic Counter Contention

## Problem Summary
- Insight target: multi-threaded telemetry path spends substantial time contending on a single shared atomic counter.

## ID
- `test_case_17`

## Public Intent (safe summary)
- Simulate telemetry event processing across multiple threads.
- Measure runtime and total processed count deterministically.

## What's Wrong In Current Implementation
- All worker threads call `AtomicLong.incrementAndGet()` on one shared counter in the hot loop.
- The shared atomic update is on the per-event path, causing contention and coherence traffic.
- The code does not aggregate per-thread counters before combining.

## What The LLM Should Suggest
- Identify shared atomic increment as hotspot/limiter.
- Suggest per-thread local counters with one merge step, or `LongAdder` style striped aggregation.
- Suggest validating scaling by thread count after reducing shared-write pressure.

## Expected Profiling Characteristics
- Significant samples in atomic/CAS helpers and synchronization-related runtime paths.
- Worker threads spend notable time around shared-counter updates.

## Scoring Guidance
- Pass:
  - Identifies shared atomic contention and recommends local aggregation/striped counter approach.
- Fail:
  - Identifies the hotspot but only recommends generic threading changes.
  - Or misses the counter contention issue or suggests an unrelated primary fix.
