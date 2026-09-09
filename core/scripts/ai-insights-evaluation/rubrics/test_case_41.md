<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 41: Periodic Resource Catalogue Discovery

## Problem Summary

- Insight target: repeated filesystem catalogue scans and probes for missing candidates create recurring bursts of avoidable metadata syscalls and predictable `ENOENT` failures.

## ID

- `test_case_41`

## Public Intent (safe summary)

- Discover entries and configured candidates in a local resource catalogue while performing other processing between discovery periods.

## What's Wrong In Current Implementation

- The same stable directory is reopened and scanned many times instead of reusing discovery results between refreshes.
- Metadata is requested repeatedly for the same existing entries.
- The same absent candidates are retried on every pass, producing a large volume of expected `openat` failures with `ENOENT`.

## What The LLM Should Suggest

- Identify the recurring syscall-dense discovery intervals and the quieter processing intervals from the timeline rather than treating the activity as a steady run-wide rate or one-time startup work.
- Connect the high `newfstatat` count to repeated metadata inspection and the failed `openat` count to repeated probes for paths that do not exist.
- Recommend caching catalogue and negative-lookup results between refreshes, reducing the scan cadence, or refreshing from a change notification or explicit configuration event.

## Expected Profiling Characteristics

- Four dense filesystem-activity bands follow an initial quiet interval and are separated by similarly quiet processing intervals.
- `newfstatat` is a leading syscall by count, with approximately 12,000 calls attributable to repeated metadata inspection.
- Failed `openat` calls are prominent, with approximately 8,000 `ENOENT` results from repeated missing-candidate probes.
- Directory `openat`, `getdents64` and `close` activity recurs alongside the metadata and failed-open calls.

## Scoring Guidance

- Pass:
  - Identifies the recurring discovery bursts and quieter gaps using timeline evidence.
  - Identifies both repeated metadata scanning and repeated `ENOENT` probes as avoidable work, supported by syscall counts or failure data.
  - Recommends a concrete reuse, caching, cadence or change-driven refresh strategy.
- Fail:
  - Gives only generic advice to reduce filesystem calls without explaining the repeated scan and missing-path pattern.
  - Describes the syscall activity as steady or only startup-related and does not use the recurring timeline pattern.
  - Treats the expected `ENOENT` results as a storage or permissions problem.
