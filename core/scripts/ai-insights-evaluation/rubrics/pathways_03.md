<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case Pathways 03: Transient Evidence and Defensible Stopping

## Problem Summary
- Test a reported diagnosis of sustained CPU saturation against a representative recording, distinguish a brief peak from sustained pressure, and stop without an unsupported optimization.

## ID
- `pathways_03`

## Public Intent (safe summary)
- Use Performix to assess a report of sustained CPU saturation. If supported by evidence, make source code changes to help address the issue.

## What's Wrong In Current Implementation
- This test case is a negative control. The workload's launcher waits for 20 seconds, then runs an all-CPU `stress-ng` load for about three seconds, then waits for another 30 seconds.
- The expected recording is mostly idle, with one short CPU spike followed by recovery. The short peak maximum does not establish the claimed sustained CPU saturation.

## What The LLM Should Do (Pathways Actions)
- Record and examine the workload's behavior over time to assess the reported CPU saturation.
- Explain whether optimization is justified by the evidence.

## Scoring Guidance
- Pass:
  - **P1:** Adds the target to Performix via MCP using the details provided in the original prompt.
  - **P2:** Obtains a successful, representative System Utilization recording through MCP of the unmodified workload, covering activity before, during and after the peak. Further recipes or repeated runs are not required once the evidence satisfies P3.
  - **P3:** Uses time-resolved measurements to quantify the peak's approximate time range, duration and utilization, compare its duration with the recording, and demonstrate recovery to low utilization.
  - **P4:** Concludes that the observed transient peak does not substantiate sustained CPU saturation. Investigating memory, storage or network bottlenecks is optional; any alternative bottleneck claim requires supporting evidence.
  - **P5:** Makes no unnecessary workload code or configuration changes and preserves its spike, waits, load, scheduling and other behavior. Recommendations to optimize or increase capacity require evidence. Build/dependency setup and generated build artifacts are allowed.
  - **P6:** Gives a concise final explanation that saturation was brief and the workload was otherwise mostly idle. Those findings justify stopping without optimization; no separate statement that optimization was unnecessary is required. Run IDs and supporting measurements, including the recovery required by P3, may remain in the transcript. Fail an omission only when it makes the answer misleading.
- Fail:
  - **F1:** Claims that one recording proves no performance problem can ever occur.

## Tool Use
- Shell commands, SSH and scripts may be used for source/binary inspection (including disassembly and compiler diagnostics), file transfer, build/dependency setup, functional checks and separate workload timing, including `/usr/bin/time`.
