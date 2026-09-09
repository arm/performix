<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 55: ASCT Peak Bandwidth

## Problem Summary
- Insight target: characterize aggregate peak-bandwidth results and qualify
  ASCT-provided percentages without inventing a defect.

## ID
- `test_case_55`

## Public Intent (safe summary)
- Explain the peak-bandwidth benchmark results for each reported traffic type.

## What The LLM Should Report
- Report concrete bandwidth in GB/s for the available traffic types and include
  each row's ASCT-provided `% of Peak Theoretical` value when present. A relative
  difference from the All Reads row is not a substitute for that percentage.
- Preserve traffic ordering and distinguish aggregate peak bandwidth from the
  single-core bandwidth sweep.
- Apply the query guide's below-60% investigation threshold only to a provided
  percentage and do not call a healthy result faulty without evidence.
- Treat configuration, frequency, memory population and background load as
  possible checks rather than established causes.

## Expected Profiling Characteristics
- The healthy default-benchmark fixture contains aggregate peak-bandwidth rows
  and ASCT-provided percentages.
- Recorded system information and related ASCT benchmark tables may provide
  additional evidence. Their availability and values depend on the run; facts
  queried from those sources are not inventions when clearly attributed.

## Scoring Guidance
- Pass:
  - Reports concrete values, units and traffic types for the available rows.
    Reporting one or more ASCT-provided percentages is sufficient when they
    support the overall interpretation; omitting percentages for otherwise
    accurately reported secondary rows is a minor omission.
  - May supplement an accurately reported absolute row value with a derived
    difference phrased as that row being `X GB/s below` another row. Interpret
    `X GB/s` as the difference in that construction, not as the row's absolute
    bandwidth.
  - May report a theoretical reference, system-configuration detail or related
    benchmark cross-check when it is attributed to recorded information from
    the run.
  - Correctly qualifies any threshold interpretation. It only needs to
    distinguish aggregate peak bandwidth from a single-core sweep when both are
    discussed or compared.
  - Avoids unsupported fault and application-bottleneck claims.
- Fail:
  - Invents or reconstructs percentages or theoretical bandwidth that ASCT did
    not provide through benchmark results or recorded system information.
  - Compares aggregate peak and single-core sweep results as equivalent.
  - Claims a proven defect without supporting evidence.
  - Do not fail a response solely because a correctly calculated difference is
    phrased as one row being `X GB/s below` another row, when the absolute row
    values are otherwise reported correctly and the response does not
    contradict them.
  - Do not fail solely because some traffic rows omit their provided percentage,
    or because the response does not mention an otherwise unused single-core
    sweep.
