<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 61: ASCT Low Peak Bandwidth

## Problem Summary
- Insight target: identify low aggregate peak bandwidth while keeping diagnosis
  appropriately qualified.

## ID
- `test_case_61`

## Public Intent (safe summary)
- Explain whether the aggregate peak-bandwidth results warrant investigation.

## What The LLM Should Report
- Quantify the peak-bandwidth traffic rows in GB/s and their ASCT-provided
  percentages; identify that the generated maximum is 50%.
- Apply the below-60% guidance as a reason to investigate, not proof of a defect.
- Keep aggregate peak bandwidth distinct from the unchanged single-core
  bandwidth sweep.
- Present configuration, frequency, memory population and background load only
  as possible checks, and recommend a controlled repeat.

## Expected Profiling Characteristics
- The modified fixture contains aggregate peak-bandwidth rows whose generated
  maximum is 50% of peak theoretical bandwidth.
- The single-core bandwidth sweep is unchanged.

## Scoring Guidance
- Pass:
  - Finds and quantifies the low result and recommends investigation.
  - Separates observations from possible causes.
- Fail:
  - Misses the low percentage or conflates aggregate and single-core bandwidth.
  - Invents a root cause or claims a hardware or application defect.
