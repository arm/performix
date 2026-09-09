<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 62: ASCT Directional Core-to-Core Latency Asymmetry

## Problem Summary
- Insight target: identify and correctly qualify a material directional
  core-to-core latency asymmetry in an otherwise valid ASCT run.

## ID
- `test_case_62`

## Public Intent (safe summary)
- Explain any material directional core-to-core latency asymmetry and its
  limitations.

## What The LLM Should Report
- Identify the affected CPU pair and both measured latencies, quantifying that
  the lower-numbered source-CPU direction is about six times its reverse.
- Establish that source-node and memory-binding values are comparable in both
  directions before treating the asymmetry as meaningful.
- Describe the result as worth investigating, not proof of hardware failure or
  a particular root cause.
- Do not treat `MEMBIND_NODE` as the node of `CPUB` or label the pair intra- or
  inter-NUMA without a target-CPU node mapping.

## Expected Profiling Characteristics
- The modified fixture contains one comparable CPU pair whose lower-numbered
  source-CPU direction is approximately six times its reverse.
- Both directions use comparable source-node and memory-binding values.

## Scoring Guidance
- Pass:
  - Finds and quantifies the directional pair and verifies comparable binding.
  - Handles topology fields correctly and qualifies possible causes.
- Fail:
  - Omits the asymmetry, invents pair locality or treats memory binding as the
    target CPU's node.
  - Claims a proven fault or application bottleneck.
