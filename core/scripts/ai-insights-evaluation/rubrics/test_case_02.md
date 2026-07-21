<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 02: Lock Contention in Hot Loop

## Problem Summary
- Insight target: lock contention from per-element synchronization in a threaded loop.

## ID
- `test_case_02`

## Public Intent (safe summary)
- Run a parallel shared-aggregate integer kernel over a large buffer.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- Each element update takes a mutex lock (`std::lock_guard`) around shared total update.
- Synchronization frequency is proportional to element count, creating extreme lock pressure.
- Threads spend significant time contending on one shared critical section.

## What The LLM Should Suggest
- Explicitly identify lock/mutex contention as the primary issue.
- Suggest per-thread local accumulation with a final combine/reduction step.
- Suggest reducing synchronization granularity (chunk-level combine, lock-free reduction alternatives where appropriate).

## Expected Profiling Characteristics
- Most samples should land in synchronization/locking and the core accumulation loop.
- Secondary samples may appear in thread startup/teardown paths.

## Scoring Guidance
- Pass:
  - Identifies synchronization/lock-heavy behavior as primary issue and gives concrete contention-reduction strategies.
- Fail:
  - Notes the hotspot but gives generic suggestions without addressing synchronization cost.
  - Or misses the synchronization bottleneck or suggests an unrelated main fix.
