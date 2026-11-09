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
- Investigate the suspected CPU bottleneck using the supplied checkout and run as context.
- Use profiling evidence to guide a source fix, then compare performance and correctness.
- Explain the investigation's outcome.

## Scoring Guidance
The judge must cite run IDs and `session_line` evidence for the original and optimized profiles required by P2 and P3, or explain which is missing. This does not add reporting requirements to the agent's final answer.

- Pass:
  - **P1:** Adds the target to Performix via MCP using the details provided in the original prompt.
  - **P2:** Establishes an original-source Performix profile that meets F1's comparison rules. A supplied profile may qualify; otherwise, obtains a fresh original-source profile through MCP. The original source may be reconstructed and measured after optimization. Separate runtime measurements cannot replace this profile.
  - **P3:** Uses appropriate Performix recipes and dynamic insights to link recorded performance evidence to `kernel_2` synchronization. Validates source changes using a profile of a binary matching the optimized source, rebuilding when needed, and comparable workload runtimes. Separate timings cannot replace this profile; run IDs alone do not establish that the recorded data supports the conclusion. Assess conclusions using the source, profiling and runtime evidence together. A sparse profile alone cannot prove that a hotspot disappeared, but the combined evidence may support that conclusion. Existing validation profiles may be reused under F1's comparison rules. Any supported fix is acceptable; multiple source or instrumentation changes may be validated together.
  - **P4:** Comparable workload runtime measurements demonstrate at least a 50% runtime reduction while preserving checksum values for equivalent workload inputs. Uses repeated measurements to account for noise. Measured iterations within one program execution count as repetitions, including when reported as an average. Separate program launches, individual timings and spread statistics are not mandatory unless the evidence leaves the required 50% improvement uncertain.
  - **P5:** Gives a concise final explanation of the diagnosis, source changes and validation outcome. Run IDs, measurements, checksum values and iteration counts may remain in the transcript. An omitted detail or sampling caveat is a failure only if it makes a substantive claim misleading. Claims about the effect of an individual change need evidence for that effect.
- Fail:
  - **F1:** Compares mismatched inputs, thread counts, build modes or incompatible profiling settings. Targets may differ, provided their hardware and operating conditions are comparable. Do not fail solely for different sampling rates or sparse optimized profiles. When sampling rates differ, establish speedup using comparable unprofiled timings and assess the combined evidence under P3.
  - **F2:** Reduces the work performed or alters the workload's business logic or functionality.

## Tool Use
- Shell commands, SSH and scripts may be used for source/binary inspection (including disassembly and compiler diagnostics), editing, file transfer, builds, functional/checksum checks and separate workload runtime measurements, including full-duration runs wrapped in `/usr/bin/time`.
