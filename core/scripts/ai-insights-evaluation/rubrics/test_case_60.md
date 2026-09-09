<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 60: ASCT Core-to-Core Latency

## Problem Summary
- Insight target: characterize ordinary core-to-core latency spread without
  overstating the topology represented by the table.

## ID
- `test_case_60`

## Public Intent (safe summary)
- Explain the measured core-to-core latency spread and its topology limitations.

## What The LLM Should Report
- Use measured latency evidence to characterize the observed spread.
- Correctly identify `CPUA_NODE` as the source CPU's node and `MEMBIND_NODE` as
  the benchmark memory-binding node.
- Do not treat `MEMBIND_NODE` as the node of `CPUB`. Any statement about
  `CPUB` locality should be consistent with separate topology evidence.
- Treat differences as microbenchmark observations, not proof of a root cause
  or application bottleneck.

## Expected Profiling Characteristics
- The default-benchmark fixture contains ordinary core-to-core latency
  measurements with source-node and memory-binding fields.
- The core-to-core table does not independently identify the NUMA node of
  `CPUB`; separate system information may provide that mapping.
- This fixture's system information maps all CPUs, including the measured CPUs
  94 and 95, to its single NUMA node 0.

## Scoring Guidance
- Pass:
  - Characterizes the measured spread and does not misuse the topology fields.
  - May state that both measured CPUs are on node 0 based on this fixture's
    known topology; citing the exact system-information key is not required.
- Fail:
  - Explicitly treats memory binding as the target CPU's node, misstates the
    roles of `CPUA_NODE` or `MEMBIND_NODE`, or invents topology inconsistent
    with the fixture.
  - Do not fail solely because the response omits the exact topology key used to
    support a correct node-0 assignment for CPUs 94 and 95.
  - Turns ordinary variation into an unsupported fault.
