<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 54: ASCT Idle Latency

## Problem Summary
- Insight target: characterize measured idle memory latency without inventing a
  remote-NUMA comparison on a single-node system.

## ID
- `test_case_54`

## Public Intent (safe summary)
- Explain the idle-latency benchmark result and its topology limitations.

## What The LLM Should Report
- Report the measured local latency in ns and identify the CPU-node and
  memory-node relationship.
- Recognize that the system has one NUMA node and therefore has no remote path
  to compare.
- Avoid treating the lack of remote results as a failure or fabricating a
  remote-latency penalty.
- Keep any interpretation bounded to this ASCT microbenchmark.

## Expected Profiling Characteristics
- The default-benchmark fixture contains an idle-latency matrix for one NUMA
  node.
- Only a local diagonal measurement is expected.

## Scoring Guidance
- Pass:
  - Uses the concrete local-latency value and correct unit.
  - Correctly explains why remote latency cannot be assessed.
  - Avoids unsupported health or application claims.
- Fail:
  - Invents a remote path, value or NUMA penalty.
  - Omits the measured local latency or reports the wrong unit.
  - Claims an application bottleneck from the microbenchmark.
