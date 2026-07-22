<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 10: Overly Strong Atomic Ordering

## Problem Summary
- Insight target: expensive atomic operations using stronger memory ordering than required.

## ID
- `test_case_10`

## Public Intent (safe summary)
- Run a multi-threaded telemetry packet-processing loop with a shared processed counter.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- Worker threads update one shared counter with `fetch_add(..., memory_order_seq_cst)` for each packet.
- Atomics are legitimate in this multi-threaded scenario, but strict global ordering is stronger than needed for this counter.
- Strong ordering increases overhead versus weaker ordering and/or sharded aggregation strategies.

## What The LLM Should Suggest
- Identify atomic ordering strength as a likely overhead source.
- Suggest evaluating weaker memory order and/or local thread counters with periodic merge where semantics allow.
- Emphasize correctness constraints and validation before changing ordering semantics.

## Expected Profiling Characteristics
- Samples should concentrate in `run_telemetry_counter` with significant atomic instruction cost.
- Hot path is dominated by per-packet atomic updates in worker threads.

## Scoring Guidance
- Pass:
  - Flags strong-order atomic overhead and proposes semantics-safe alternatives.
- Fail:
  - Finds the hotspot but gives generic advice without addressing ordering strength.
  - Or misses the primary issue or suggests an unrelated main fix.
