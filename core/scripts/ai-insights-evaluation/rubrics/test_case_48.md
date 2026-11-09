<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 48: Efficient Vectorised Aggregation

## Problem Summary
- CPU category and cause: Retiring — useful vector instructions. The .NET
  aggregation loop has a high Retiring measurement because the CPU is
  completing useful vector work rather than waiting or discarding work. The
  recording shows no hardware bottleneck.

## ID
- `test_case_48`

## Public Intent (safe summary)
- Repeatedly sum an array that fits in cache, using two independent
  `Vector<ulong>` accumulators. These vectors let one instruction process
  several values.
- Record managed stacks so the hot path can be attributed to application code.

## What's Wrong In Current Implementation
- The recording does not show a CPU performance problem. A high Retiring value
  is expected because this loop is completing useful vector instructions.
- A hot function alone is not evidence that its implementation should change.

## What The LLM Should Suggest
- Do not present converting the loop to vector instructions, using wider
  vectors, loading data early or changing its layout as changes supported or
  prioritised by this recording. The response may state that a different
  implementation would need a separate experiment, but must not describe one
  as the likely fix.
- If more throughput is required, first measure the application requirement or
  look for a way to reduce the amount of aggregation work.

## Expected Profiling Characteristics
- Retiring is consistently the largest top-level CPU category. The CPU
  completes many instructions per cycle (high IPC). Frontend Bound (waiting for
  instructions), Backend Bound (waiting for data or an execution unit) and Bad
  Speculation (discarded work) remain low.
- The instruction mix or assembly code shows that the hot loop already uses
  vector instructions.
- Managed stack, source or assembly evidence connects the samples to
  `AggregateBatch`.
- Code Hotspots can establish the last two observations, but it cannot establish
  that the CPU spends little time waiting. The conclusion that no change is
  justified must use the CPU measurements, rather than the absence of another
  visible hot function.

## Scoring Guidance
- A response cannot pass without measured evidence that distinguishes useful
  completed work from time spent waiting or discarding work. Identifying the hot
  vector loop from source or assembly is not enough.
- High Retiring and IPC, supported by low bad speculation or other relevant
  counters, can justify the cautious conclusion that no loop change is supported.
  Explicit Frontend Bound and Backend Bound values are not mandatory. Do not
  require every counter, or accept claims that cache misses alone rule out stalls.
- Accept clearly qualified experiments with unproven benefits. Judge the
  substance without requiring specific wording or ordering.
- Pass:
  - Identifies high Retiring as useful completed work rather than waiting.
  - Confirms that the sampled aggregation loop already uses vector operations.
  - Uses measurements of completed work, waiting and discarded work to assess
    whether a CPU-specific change is justified.
- Fail:
  - Treats high Retiring as a performance fault by itself.
  - Uses only hotspot, source or assembly evidence to claim that the loop is
    efficient, without evidence that the CPU spends little time waiting or
    discarding work.
  - Presents vectorising the loop, using wider vectors or using manual
    intrinsics as a likely or prioritised fix when the loop already uses vector
    instructions and the CPU measurements do not support that conclusion.
  - Claims that instruction fetching, data access or branch prediction is a
    bottleneck without supporting measurements.
