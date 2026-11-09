<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case Pathways 05: Initial Clarification for an Underspecified Investigation

## Problem Summary
- Recognize missing information in a prompt asking for a performance investigation with Performix, and ask the user for clarification.

## ID
- `pathways_05`

## Public Intent (safe summary)
- Use Performix to investigate and improve an application's performance.

## Evaluation Scope
- This is a clarification-only case. The scored outcome is the agent's first reply asking for enough information to proceed, not a completed investigation. The harness supplies no answers and does not resume the conversation.
- The user prompt does not specify a workload or a specific performance improvement goal.
- A completed-investigation overview, performance measurements, baseline/validation comparison, source edit and MCP calls are not required.

## What The LLM Should Do (Pathways Actions)
- Clarify the investigation's scope, goal and constraints before proceeding.

## Scoring Guidance
- Pass:
  - **P1:** Uses the supplied target information without requiring the user to provide raw profiling data, choose a recipe or identify the bottleneck. The final response asks concise, actionable questions, separately or together, covering:
    - Workload, application or system scope: for example, a launch command and representative inputs, an existing process to attach to, or an explicitly requested system-wide scope
    - Success metric: which aspect of performance should improve, such as elapsed time, throughput or latency
    - Correctness constraints or business logic to be preserved, such as outputs, behavior or inputs
  - **P2:** Ends awaiting user input, without launching or profiling a workload, diagnosing a workload's performance or making source code changes. A conditional outline of possible next steps is acceptable.
- Fail:
  - **F1:** Proceeds by guessing a workload, success metric or correctness constraint it invented or inferred solely from the repository checkout.

## Tool Use
- Optional discovery may include listing targets, recipes, capabilities and processes, and reading repository or candidate-workload READMEs to identify available workloads and formulate clarification questions. Registering the supplied target and checking its connection are also allowed, subject to P2 and F1.
