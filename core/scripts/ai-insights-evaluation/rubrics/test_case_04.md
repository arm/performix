<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 04: Multithreaded Allocator Churn

## Problem Summary
- Insight target: allocator/runtime overhead from per-request temporary buffer churn under worker concurrency.

## ID
- `test_case_04`

## Public Intent (safe summary)
- Run a multithreaded request-staging loop over a fixed request schedule.
- Measure runtime and checksum deterministically.
- Use one long measured pass so thread startup/teardown is secondary to worker-side allocator churn.

## What's Wrong In Current Implementation
- Each request allocates fresh temporary staging/storage objects even though the request sizes come from a small repeated template set.
- Request sizes are intentionally small enough that the testcase is meant to surface per-request allocation/runtime overhead rather than large-payload memcpy bandwidth.
- The hot loop performs this allocation/free pattern concurrently across worker threads, increasing allocator/runtime overhead.
- The static code shows temporary objects, but profiling is needed to establish that allocator/runtime paths dominate useful work rather than the small per-request transform logic.

## What The LLM Should Suggest
- Identify allocator/runtime overhead (malloc/free and related allocator internals) as the main issue when supported by the profile.
- Suggest reducing per-request ownership churn using thread-local scratch buffers, object pools, reserved/reused staging buffers, or request batching.
- It is acceptable to mention allocator choice/tuning as a secondary action, but the primary recommendation should be reuse/pooling in the application.

## Expected Profiling Characteristics
- Significant samples should land in allocator/runtime paths and in `run_request_stage_4` worker code.
- The small transform logic should be visible but secondary to allocation/runtime overhead.
- Thread lifecycle paths may still appear, but they should be secondary to allocator/runtime cost rather than the primary diagnosis.

## Scoring Guidance
- Pass:
  - Identifies temporary allocation churn / allocator overhead under concurrency as the primary issue and gives concrete reuse/pooling guidance.
- Fail:
  - Finds the hot worker loop but gives generic optimization advice, or focuses only on memcpy/string copy without connecting it to per-request allocation churn.
  - Or misses the allocator/runtime bottleneck or suggests an unrelated primary fix.
