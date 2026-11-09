<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case Pathways 02: Unknown Throughput Regression and Broad-to-Narrow Investigation

## Problem Summary
- Investigate an unknown throughput problem, narrow from system evidence to actionable CPU issues, and validate the resulting optimizations.

## ID
- `pathways_02`

## Public Intent (safe summary)
- Use Performix to investigate a reported low throughput and improve completed iterations while preserving workload inputs, work per iteration, component weights, timing and result semantics.

## What's Wrong In Current Implementation
- `test_case_52` combines three independent sources of CPU pressure: a large generated request-handler code footprint, indexed record processing with poor data locality, and unpredictable packet-protocol dispatch.
- The corresponding paths are `process_generated_requests`, `process_indexed_records` and `process_packets`. Evidence may show frontend/instruction-fetch pressure, backend/data-access pressure and bad speculation respectively; their relative importance depends on the target and recording.
- The workload warms up for five seconds, then counts completed iterations during a fixed 45-second measured period.

## What The LLM Should Do (Pathways Actions)
- Investigate broadly, typically using System Utilization followed by CPU Microarchitecture, then narrow to actionable CPU bottlenecks.
- Use the findings to guide source changes and validate performance and correctness.
- Explain the diagnosis and outcome.

## Scoring Guidance
- Pass:
  - **P1 — Target and baseline:** Adds the supplied target through the configured Performix MCP server and obtains a successful original-version baseline run through MCP. Source versions may be reconstructed, rebuilt and measured in either order.
  - **P2 — Diagnosis:** Investigates instruction-fetch, data-access and speculation at the relevant functions or call paths using recorded Performix data and/or generated insights. Run-wide categories, source code, comments or documentation alone are insufficient. Links each finding to the implementation and explains which findings support optimization. A weak or absent mechanism is a valid finding when supported by that function's or call path's data. No particular component must be optimized, and no exact metric value or sample distribution is required.
  - **P3 — Comparable validation:** Rebuilds the optimized workload and obtains fresh successful Performix runs through MCP. Establishes which source and binary each run measured, matching recipe, profiling settings, target, inputs, build mode, component weights and timing for each comparison. Run IDs alone do not establish that the recorded data supports the conclusions. Any narrower recipe used to claim an effect has a matching pre-change run. Multiple source or instrumentation changes may be validated together; separate validations for individual changes are permitted, but not required.
  - **P4 — Work preservation:** Preserves inputs, required work per iteration, all component weights, the timing loop, warm-up and result semantics. Moving required measured work into setup or replacing it with precomputed results is not a valid optimization, even if outputs match. Assess the source changes in setup, warm-up and the measured loop independently of output equivalence.
  - **P5 — Correctness:** Runs a separate test comparing original and modified code on identical inputs and the same amount of work. Keep correctness-checking work out of the timed workload.
    - Assess the final test actually run, including any fixes. Direct output comparisons or checksums that can reveal output differences are acceptable. Check how each checksum is calculated: XORing identical results an even number of times always gives zero and proves nothing; an odd number preserves the result. An ineffective checksum does not invalidate separate, sufficient output comparisons.
    - If the test runs the workload once and repeatedly processes that output, count one workload execution, not multiple test repetitions.
    - Matching final checksums from the 45-second performance runs alone does not establish correctness: the versions may complete different numbers of iterations.
  - **P6 — Performance improvement:** Demonstrates at least a 10% increase in completed iterations for the final version versus the original over the unchanged 45-second measured period. Uses comparable repeated measurements of both versions to account for run-to-run variation. Individual changes need not each achieve 10%; short correctness tests or a faster kernel alone do not establish whole-workload improvement. Credit honest unsuccessful experiments in the rationale without treating them as validated improvements.
  - **P7 — Final response:** Gives a concise overview of the diagnosis, source edits and validation outcomes, including baseline and validation Performix run IDs, improvement measurements and correctness evidence. Distinguishes observed effects from hypotheses. Discloses limitations that weaken a stated conclusion; name the affected conclusion when judging an omission as a failure. Methods allowed by P3, P6, F1 and the Tool Use section are not themselves limitations. Build details, profiling settings and individual counter values may remain in the transcript unless needed to explain such a limitation.
- Fail:
  - **F1:** Uses a comparison that violates P3, even if the incompatibility is disclosed. A later formatting-only source change with unchanged build settings does not invalidate a profile or require another capture or a binary hash after rebuilding.

## Tool Use
- Shell commands, SSH and scripts may be used for source/binary inspection (including disassembly and compiler diagnostics), editing, file transfer, builds, equal-work functional/checksum checks and separate full-duration workload measurements. Separate shell timings are valid measurements without Performix run IDs.
