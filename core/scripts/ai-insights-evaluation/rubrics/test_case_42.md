<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 42: Batched And Per-Record Log Persistence

## Problem Summary

- Insight target: synchronizing every small log record creates a dense, expensive persistence phase, while the surrounding batched phases demonstrate a cheaper alternative.

## ID

- `test_case_42`

## Public Intent (safe summary)

- Persist fixed-size log records in several phases while performing other processing between phases.

## What's Wrong In Current Implementation

- The middle phase writes and calls `fdatasync` once for every 256-byte record, producing far more writes and synchronizations than the batched phases for the same record volume.

## What The LLM Should Suggest

- Identify the sparse batched phases on either side of the much denser per-record phase using timeline evidence.
- Use syscall counts or duration evidence to identify per-record `fdatasync` and small `write` calls as the material cost.
- Recommend batching records or otherwise reducing synchronization frequency, qualified by the application's durability requirements.

## Expected Profiling Characteristics

- The timeline shows a sparse persistence phase, a quiet gap, a much denser middle phase, another quiet gap and a final sparse phase.
- Approximately 504 `write` and 504 `fdatasync` calls occur; most are concentrated in the middle phase while each batched phase contributes about 12 of each.
- `fdatasync` is likely to contribute materially to total traced syscall time, depending on the recording storage.

## Scoring Guidance

- Pass:
  - Distinguishes the dense per-record phase from the sparse batched phases using the timeline.
  - Identifies per-record synchronization as the principal issue and recommends batching or another durability-aware reduction in synchronization frequency.
- Fail:
  - Reports write or synchronization counts without identifying the phase change.
  - Gives only generic storage advice or recommends removing durability guarantees.
