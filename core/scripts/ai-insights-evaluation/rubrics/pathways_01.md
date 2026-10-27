<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case Pathways 01: Suspected CPU Hotspot and Controlled Source Fix

## Problem Summary
- A suspected CPU-side bottleneck, which should be fixed without altering the workload's "business logic" or functionality (its checksum).

## ID
- `pathways_01`

## Public Intent (safe summary)
- Use Performix to investigate and improve the performance of a workload reported as too slow on the supplied target, without changing the workload's "business logic" or functionality (its checksum).

## What's Wrong In Current Implementation
- Each element update takes a mutex lock (`std::lock_guard`) around the shared total update.
- Synchronization frequency is proportional to element count, creating extreme lock pressure.
- Threads spend significant time contending on one shared critical section in `kernel_2`.

## What The LLM Should Do (Pathways Actions)
- Treat the initial workload checkout and supplied run as provided context for the investigation.
- Add the supplied target through the configured Performix MCP server and verify the connection is successful.
- Establish a suitable baseline Performix run and optionally also a separate workload runtime measurement, representing the performance characteristics of the workload before source code edits are made. The supplied initial run is acceptable only if its target, workload inputs, thread count and build configuration support a valid comparison against fresh Performix runs the agent is able to gather live to validate source code edits. A matching testcase name or the mere existence of a supplied run are insufficient reasons to claim that the initial run is acceptable to use as the baseline. Otherwise, the agent should obtain a fresh baseline Performix run through MCP to capture performance characteristics of the initial state of the workload source code. Reconstructing and measuring the original source using Performix after edits were made is a valid alternative way of obtaining a baseline.
- Select and use a Performix recipe (or recipes) appropriate to the suspected CPU bottleneck, and explain the reasons for choosing any recipes used.
- Generate dynamic insights from Performix runs relevant to the investigation. This should reveal the synchronization in `kernel_2` as the root cause of the bottleneck.
- Inspect the workload source code and make one controlled change at a time, such as per-thread accumulation followed by a bounded final reduction, while preserving inputs, work performed and checksum semantics. Other fixes are acceptable when the evidence supports them.
- Validate the performance effect of each source code edit before making the next. This should involve rebuilding the changed workload and obtaining a fresh successful Performix recipe run through MCP. Relevant data from Performix should be compared with matching workload inputs, thread count, build mode and relevant profiling settings on the same target. The validation should account for noise or inconsistent results before claiming an improvement (for example, by running Performix and/or the workload more than once and aggregating the results).
- Report baseline and validation Performix run IDs, baseline and validation workload runtime measurements, the percentage runtime reduction, unchanged checksum values and a summary of source edits made.

## Scoring Guidance
- Pass:
  - Adds the target to Performix via MCP using the details provided in the original prompt.
  - Establishes a suitable baseline Performix run (and optionally also a workload runtime measurement) representing the performance characteristics of the workload to compare against validation evidence after the source code edit.
  - Links recorded performance evidence to `kernel_2` synchronization and makes one controlled source change at a time, followed by rebuilding the workload, reprofiling with Performix and remeasuring the workload runtime.
  - Demonstrates at least a 50% runtime reduction while preserving checksum values for equivalent workload inputs.
  - Reports all baseline and validation measurements and run IDs, with conclusions supported by the transcript and any measurement limitations acknowledged.
- Fail:
  - Omits successful MCP target creation
  - Claims validation that source code edits improved the performance without supporting evidence from Performix recipe runs and/or workload runtime measurements. This should include both a baseline representing the state of the workload before the edit, and a validation for afterwards.
  - Fails to justify why a Performix recipe was selected.
  - Compares materially different source versions, targets, inputs, thread counts, build modes or profiling settings without establishing a valid comparison.
  - Combines unrelated source code edits before validation
  - Profiles a stale binary
  - Reduces the work performed, i.e. altering the workload's "business logic" or functionality (its checksum)
  - Gives only generic advice, or leaves the investigation without a validated improvement meeting the requested threshold. An honest report of an unsuccessful or inconclusive optimization is preferable to a fabricated success, but does not satisfy this scenario's success criterion.
