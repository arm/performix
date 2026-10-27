<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 73: Retention of Processed Data

## Problem Summary
- Insight target: the workload keeps each input block after processing it,
  although it has already added that block's value to a checksum. The stored
  blocks fill more of the heap over time and may increase GC cost.

## ID
- `test_case_73`

## Public Intent (safe summary)
- Process 4 MiB of new input each second for 30 seconds, producing a running
  checksum. Store the processed blocks in a list; by the end it holds 120 MiB.
- Continue working with a separate, fixed-size 8 MiB window for the full
  35 seconds, so GC and heap measurements are available throughout the run.

## What's Wrong In Current Implementation
- `processedBlocks` keeps every input block after its value has been used.
  Nothing reads the stored contents again; the running checksum already holds
  the result the workload needs.
- The separate working window always has the same size and replaces its old
  blocks. Its discarded blocks can be collected; the growing list is what
  causes the after-GC baseline to rise. The finite run does not prove that a
  real application would grow indefinitely.

## What The LLM Should Suggest
- Compare early and late after-GC heap use and report measured GC cost; do not
  infer cost from heap occupancy alone.
- Connect the JFR trend to source showing that processed blocks remain stored
  after processing. JFR and CPU samples alone cannot identify their owner.
- Advise reducing retention of processed blocks if it is unnecessary. A named
  list operation and remeasurement advice are helpful but optional.

## Expected Profiling Characteristics
- An accepted recording should show after-GC heap use rising across much of
  the run. Report measured GC cost; do not assume pauses dominate runtime. A
  short late plateau after the last scheduled addition does not erase the
  earlier growth.
- Exact heap levels, collection types, and pause costs must come from the
  recording.

## Scoring Guidance
- Pass:
  - Uses CPU samples or sampled call paths to establish application or GC cost
    and ranks the finding against other hotspots.
  - Uses measured late JFR heap and GC evidence to distinguish retained growth
    from temporary allocation in the working window.
  - Uses source to identify the stored processed blocks as the cause of retained
    growth and advises reducing that retention if it is unnecessary.
  - Qualifies the observed time range rather than claiming a proven indefinite
    leak or a cost that the recording does not show.
- Fail:
  - Calls a before-GC sawtooth retained growth, or treats a finite recording
    as proof of an indefinite leak.
  - Omits source or JFR evidence, invents an object owner from JFR, or offers
    only heap or GC tuning without addressing the retained processed blocks.
