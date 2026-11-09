<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 71: Temporary Allocation and GC Pressure

## Problem Summary
- Insight target: continuous creation of temporary batches makes allocation
  and ordinary GC a significant cost while the retained heap stays bounded.

## ID
- `test_case_71`

## Public Intent (safe summary)
- Process a fixed data set for 35 seconds while continuously creating batches
  of short-lived blocks. Keep only a fixed number of recent batches reachable.
- Keep the processing checksum and completed-batch count observable.

## What's Wrong In Current Implementation
- Each batch creates new blocks even though earlier batch storage could be
  reused. Replacing an older batch makes its blocks eligible for GC.
- The fixed in-flight window bounds retention; repeated GC does not establish
  a leak.

## What The LLM Should Suggest
- Connect sampled application allocation work to JVM allocation or GC paths,
  using source to identify the repeated batch creation.
- Use selected-recording JFR events to quantify collection count and pause
  cost. Use after-GC samples if assessing retained-heap growth.
- If the measured cost is material, prioritize reducing batch allocation or
  live temporary data, for example by reusing blocks or reducing the in-flight
  window where semantics permit, then remeasure. A larger-heap experiment is
  also acceptable when occupancy and GC measurements support limited headroom
  and the response connects the pressure to repeated batch allocation.
  Bounded after-GC use does not rule out capacity pressure. Do not prescribe
  heap or GC settings without evidence that they address the cost.

## Expected Profiling Characteristics
- Sustained application allocation and JVM runtime activity should be visible
  beyond JVM startup. Ordinary collections should have a material measured
  cost in an accepted recording, while later after-GC heap use remains broadly
  bounded.
- The exact sample share, GC count, and pause time must come from the recording.

## Scoring Guidance
- Pass:
  - Connects material sampled allocation work to the batch loop, reports
    measured JFR GC cost, and recommends an evidence-backed allocation, retention
    or heap-headroom experiment as described above. Buffer reuse need not be the
    sole or first recommendation when an alternative is supported.
  - Ranks the finding according to measured cost rather than GC count alone.
- Fail:
  - Calls the bounded live set a leak or confuses temporary allocation with
    continued retained growth.
  - Gives only generic Java or GC tuning advice, omits sampled code or JFR
    evidence, or invents a pause total or expected speedup.
