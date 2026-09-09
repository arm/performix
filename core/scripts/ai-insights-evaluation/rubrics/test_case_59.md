<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 59: ASCT Loaded Latency

## Problem Summary
- Insight target: explain how latency changes with generated memory load without
  reversing ASCT's NOP interpretation or inventing a saturation knee.

## ID
- `test_case_59`

## Public Intent (safe summary)
- Explain how measured latency changes as generated memory load increases.

## What The LLM Should Report
- Use measured bandwidth and latency evidence from the curve.
- Interpret more injected NOPs as less background traffic and therefore treat
  the highest-NOP end of the curve as low load.
- Support any claimed saturation region with the shape of the curve. The exact
  term "knee" and an explicit rejection of one are not required.
- Treat a value from ASCT's `% of Peak Theoretical BW` column as provided
  evidence; it does not need to be reconstructed from another benchmark.
- Keep comparisons with latency-sweep DRAM approximate and avoid application
  bottleneck claims.

## Expected Profiling Characteristics
- The loaded-latency fixture contains a curve with injected-NOP, bandwidth and
  latency measurements across increasing memory load.
- The highest-NOP point represents the low-load baseline.

## Scoring Guidance
- Pass:
  - Correctly explains the load direction and the measured latency trend.
- Fail:
  - Reverses the NOP/load relationship or bases a strong saturation claim on
    an isolated point.
  - Invents percentages or diagnoses an application bottleneck.
