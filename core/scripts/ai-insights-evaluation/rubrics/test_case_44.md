<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 44: Sparse Blocking Timer-Driven Event-Loop Control

## Problem Summary

- Control target: long cumulative `poll` duration represents intentional waiting for sparse timer events and is not a performance problem by itself.

## ID

- `test_case_44`

## Public Intent (safe summary)

- Run a timer-driven event loop that processes periodic events.

## What's Wrong In Current Implementation

- No syscall performance problem is intentionally present; `poll` accounts for most traced time because the application is efficiently blocked while it has no event to process.

## What The LLM Should Suggest

- Use count and timeline evidence to recognize a low number of long `poll` calls as sparse event-driven waiting rather than busy polling.
- State that cumulative blocking duration alone does not establish a bottleneck.
- Avoid optimization advice unless tied to evidence of unmet latency or throughput requirements.

## Expected Profiling Characteristics

- Approximately 12 `poll` calls each block for about one second and account for most traced syscall duration.
- Each wakeup is followed by a small `read`; the timeline is sparse and regular rather than continuously dense.

## Scoring Guidance

- Pass:
  - Recognizes the sparse wait-and-read pattern and explains that the long `poll` duration is legitimate blocking.
  - Avoids unnecessary remediation.
- Fail:
  - Treats `poll` as a bottleneck merely because it dominates traced time.
  - Confuses the long blocking calls with frequent timeout polling or recommends increasing polling frequency.
