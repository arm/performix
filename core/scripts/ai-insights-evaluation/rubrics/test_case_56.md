<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 56: ASCT Cross-NUMA Bandwidth

## Problem Summary
- Insight target: explain the cross-NUMA-bandwidth matrix correctly on a
  single-node system.

## ID
- `test_case_56`

## Public Intent (safe summary)
- Explain the cross-NUMA-bandwidth result and its topology limitations.

## What The LLM Should Report
- Report the concrete local bandwidth value in GB/s.
- Explain that a one-node system provides no remote NUMA path, so remote
  bandwidth and directional symmetry cannot be assessed.
- Do not fabricate node columns or treat missing remote measurements as a
  collection failure.
- Keep this aggregate multi-core result distinct from single-core bandwidth
  sweep measurements.

## Expected Profiling Characteristics
- The default-benchmark fixture contains a cross-NUMA-bandwidth matrix for one
  NUMA node.
- Only a local diagonal measurement is expected.

## Scoring Guidance
- Pass:
  - Reports the local value with the correct unit.
  - Correctly limits the NUMA conclusion and avoids unlike comparisons.
- Fail:
  - Invents remote data or claims a remote penalty.
  - Omits the available local result or treats this as application profiling.
