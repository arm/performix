<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 43: Notification Polling With Command-Worker Launches

## Problem Summary

- Insight targets: continuous short-timeout polling causes frequent wakeups, while recurring launch-and-wait bursts are a separate process-management pattern that should be reported.

## ID

- `test_case_43`

## Public Intent (safe summary)

- Monitor a notification source while periodically running command workers.

## What's Wrong In Current Implementation

- The notification thread polls every 5 ms even though no notifications arrive, causing thousands of timeout-driven calls and wakeups.
- Each unit of command work starts a new process and waits for it to exit. The trace exposes this recurring pattern, but not the source-level constraints that determine whether those launches can be avoided.

## What The LLM Should Suggest

- Identify continuous high-frequency `poll` activity and explain that its short timeout causes repeated wakeups without ready events.
- Identify recurring process-launch clusters using timeline and process or syscall-table evidence.
- Assess both patterns without treating blocking duration as CPU time. Specific mitigations, profiling approaches or source-level redesigns are useful but not required because Syscall Trace provides no source code or application constraints.

## Expected Profiling Characteristics

- `poll` is continuously present across the roughly 14-second timeline, with roughly 2,700 timeout-driven calls.
- Four process-launch clusters appear at approximately 2, 5, 8 and 11 seconds.
- Forty-eight short-lived child processes create visible fork/exec/wait and dynamic-loader activity.

## Scoring Guidance

- Pass:
  - Identifies 2 separate patterns supported by both timeline and table evidence:
    - Continuous short-timeout polling
    - Recurring short-lived process launches
  - Accurately assesses both patterns without requiring a particular mitigation, profiling approach or source-level redesign that Syscall Trace evidence alone cannot justify.
- Fail:
  - Identifies only 1 of the 2 material patterns.
  - Lists syscall counts without explaining their temporal patterns, or materially mischaracterises their measured cost.
  - Interprets the poll calls as handling a high notification rate.
