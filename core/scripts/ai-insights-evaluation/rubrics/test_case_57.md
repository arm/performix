<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 57: ASCT Latency Sweep

## Problem Summary
- Insight target: characterize cache-to-DRAM latency transitions using the
  measured sizes and latency units.

## ID
- `test_case_57`

## Public Intent (safe summary)
- Explain the measured cache-to-DRAM latency transitions.

## What The LLM Should Report
- Use enough measured sizes and latency values to explain the main behaviour of
  the sweep. Covering every named region is not required.
- Describe transitions from the available measurements rather than relying
  only on generic cache expectations.
- Treat cache boundaries as approximate and avoid inferring an application
  bottleneck or a configuration fault from the microbenchmark alone.
- Distinguish ASCT's representative working-set points from hardware cache
  capacities. A representative point lying within a cache region is not a claim
  that the point equals that cache's capacity or exact boundary.

## Expected Profiling Characteristics
- The default-benchmark fixture contains latency-sweep summary and detailed
  measurements spanning the available cache levels and DRAM.
- System information separately reports hardware cache capacities. Those
  capacities are valid configuration context even when they differ from ASCT's
  representative working-set points.

## Scoring Guidance
- Pass:
  - Uses measured evidence to explain the sweep and qualifies conclusions.
    A high-level progression using ASCT's cache-level labels and representative
    latency values is sufficient even when working-set sizes are omitted.
  - May cite separately reported hardware cache capacities or use approximate
    terms such as cache-sized or LLC-sized as contextual shorthand.
- Fail:
  - Explicitly equates a representative working-set point with an exact hardware
    capacity or boundary, invents a material working-set range, materially mixes
    units or ignores the measured curve.
  - Working-set sizes are required only when the response claims a transition's
    location, size range or exact boundary. Do not fail a useful level-by-level
    latency summary solely because it omits sizes.
  - Do not fail because a clearly labelled representative point differs from a
    separately reported hardware capacity, or because approximate cache-region
    wording is imprecise but does not drive a false diagnosis.
  - Diagnoses an application bottleneck from the microbenchmark.
