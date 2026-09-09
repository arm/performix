<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 58: ASCT Bandwidth Sweep

## Problem Summary
- Insight target: characterize cache-to-DRAM bandwidth transitions while
  preserving the single-core scope of the benchmark.

## ID
- `test_case_58`

## Public Intent (safe summary)
- Explain the measured cache-to-DRAM bandwidth transitions and their scope.

## What The LLM Should Report
- Use enough measured sizes and bandwidth values to explain the main behaviour
  of the sweep. Covering every cache region is not required.
- Preserve the single-core scope. Other bandwidth results may provide context,
  but should not be used as directly comparable values without accounting for
  their different scope.
- Do not present compact-summary and detailed-curve measurements as the same
  exact sample. Repeating the source label for every value is not required when
  the reported sizes and values are supported by either ASCT output. Nearby
  samples may also round to the same human-readable working-set size without
  being contradictory. Unexplained averages over invented cache regions are not
  measured evidence.
- Avoid inferring an application bottleneck from the microbenchmark.

## Expected Profiling Characteristics
- The default-benchmark fixture contains bandwidth-sweep summary and detailed
  measurements spanning the available cache levels and DRAM.
- The bandwidth sweep is a single-core measurement.
- Compact-summary points and nearby detailed-curve samples can legitimately use
  different sizes and bandwidth values because they are distinct ASCT outputs.

## Scoring Guidance
- Pass:
  - Explains the measured trend and correctly qualifies its scope.
  - May report both compact-summary and detailed-curve values without repeatedly
    labelling their source or explaining numeric differences, provided the
    measurements correspond to queried ASCT output. Ratios derived directly
    from quoted measurements are also acceptable.
- Fail:
  - Treats the sweep as aggregate bandwidth or draws a misleading conclusion
    from unlike measurements.
  - Materially mixes units, attributes a value to the wrong source, presents
    invented region statistics as direct measurements or invents a fault.
  - Do not fail solely because summary and detailed values at nearby sizes differ,
    because nearby byte sizes round to the same human-readable size, because a
    source label is not repeated, or because that difference is not explained.
