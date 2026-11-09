<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 49: Unpredictable Packet Type Branch

## Problem Summary
- CPU category and cause: Bad Speculation — branch mispredictions. An
  unpredictable branch between two packet handlers causes the CPU to discard
  work after predicting the wrong path.

## ID
- `test_case_49`

## Public Intent (safe summary)
- Process a repeatable, shuffled 50/50 mix of two packet types, such as IPv4 and
  IPv6. The packet data fits in cache.
- Select one of two similarly sized handlers for every packet.

## What's Wrong In Current Implementation
- Interleaving packet classes makes the hot protocol-selection branch difficult
  to predict.
- The packet and handler data fit in cache. Neither handler does enough extra
  work for data access or execution-unit delays to be the main cause.

## What The LLM Should Suggest
- Recommend a concrete change that avoids repeated protocol dispatch, such as
  batching, branch-free selection or eliminating repeated dispatch when source
  semantics support it.
- Do not recommend `likely` or `unlikely`; neither outcome is favoured.

## Expected Profiling Characteristics
- Bad Speculation is the largest top-level CPU category and leads the next
  category by at least 15 percentage points.
- Incorrect branch predictions per thousand instructions, or the percentage of
  branch predictions that were wrong, provide the strongest supporting
  evidence.
- Function, source or assembly evidence points to the packet-type branch.

## Scoring Guidance
- Pass:
  - Identifies discarded CPU work and the packet-type branch as its likely
    source, with appropriate attribution caveats.
  - Uses incorrect branch prediction measurements and connects them to the
    packet-processing code.
  - Recommends a concrete way to avoid or remove repeated protocol dispatch.
    Eliminating repeated packet calculations counts when the recommendation
    states the conditions needed to preserve results and required behaviour.
    Batching and branch-free selection are examples, not requirements.
- Fail:
  - Recommends `likely` or `unlikely` for the 50/50 branch.
  - Blames the data cache even though the packet data fits in cache.
  - Mentions branch prediction without recommending a practical change to the
    packet processing.
