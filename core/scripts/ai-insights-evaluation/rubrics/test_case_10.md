<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 10: Overly Strong Atomic Ordering

## Problem Summary
- Insight target: expensive per-packet updates to one shared atomic counter,
  where update frequency and stronger-than-required ordering add overhead.

## ID
- `test_case_10`

## Public Intent (safe summary)
- Run a multi-threaded telemetry packet-processing loop with a shared processed counter.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- Worker threads update one shared counter with `fetch_add(..., memory_order_seq_cst)` for each packet.
- The shared read-modify-write serialises updates from otherwise independent
  workers and creates contention in the hot loop.
- Atomics are legitimate in this multi-threaded scenario, but strict global
  ordering is stronger than needed for this counter.
- Both the update frequency and ordering are avoidable sources of overhead.

## What The LLM Should Suggest
- Identify the per-packet shared atomic update as the primary issue.
- Suggest a semantics-safe way to reduce its cost, such as local thread
  counters with a final reduction, batched periodic updates, and/or weaker
  memory ordering where appropriate.
- Emphasise correctness constraints before removing the atomic or changing its
  ordering semantics.
- Do not require a weaker-memory-ordering recommendation when the proposed fix
  removes the shared atomic from the hot loop or substantially reduces its
  frequency.

## Expected Profiling Characteristics
- Samples should concentrate in `run_telemetry_counter` with significant atomic instruction cost.
- Hot path is dominated by per-packet atomic updates in worker threads.

## Scoring Guidance
- Pass:
  - Identifies the hot shared atomic update and proposes a concrete,
    semantics-safe way to remove it, reduce its frequency, and/or weaken its
    ordering.
  - A response can pass by recommending per-thread counters and reduction even
    if it does not also recommend `memory_order_relaxed`.
- Fail:
  - Finds the hotspot but gives generic advice without addressing the shared
    atomic update cost.
  - Or misses the primary issue or suggests an unrelated main fix.
