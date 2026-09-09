<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 64: Shared Request Accounting

## Problem Summary
- Every worker updates the same atomic completed-request total, causing true
  sharing.

## ID
- `test_case_64`

## Public Intent (safe summary)
- Have several pinned workers account for completed requests in one process-wide
  total.

## What's Wrong In Current Implementation
- `RequestTotals::completed` is one shared atomic counter updated in every loop
  iteration. Padding cannot remove contention on the value itself.

## What The LLM Should Suggest
- Connect the material true-sharing line, common byte offset and participating
  writers to `account_for_requests`.
- Reduce the update frequency or contention, for example with per-worker or
  sharded counters followed by periodic aggregation, or by batching increments.
- Do not present padding around the single shared counter as the primary fix.

## Expected Profiling Characteristics
- A material application cache line is classified as `TRUE` sharing.
- Accesses concentrate at one logical byte offset and show participation by
  multiple workers or writers.
- Material attribution points to `account_for_requests` and its source.

## Scoring Guidance
- Pass:
  - Gives the evidence-backed diagnosis and an appropriate sharding,
    aggregation or batching recommendation described above.
- Fail:
  - Calls the issue false sharing or recommends padding as though workers were
    modifying independent adjacent fields.
  - Gives only generic synchronization or cache advice without connecting it to
    the shared total.
  - Converts samples into unsupported latency or performance-impact claims.
