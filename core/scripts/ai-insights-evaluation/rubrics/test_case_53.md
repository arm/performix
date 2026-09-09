<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 53: ASCT System Information

## Problem Summary
- Insight target: extract material system context without inferring measured
  performance from configuration data alone.

## ID
- `test_case_53`

## Public Intent (safe summary)
- Characterize the platform described by the ASCT system-information table.

## What The LLM Should Report
- Report the available CPU, operating-system and kernel, memory-capacity and
  topology context. Memory type, speed or channel information may supplement
  the capacity information.
- Include useful firmware, mitigation, performance-feature and ASCT/tool-version
  context when materially relevant.
- Treat a missing field that is material to the characterization as unavailable
  rather than zero or evidence of a fault. The response does not need to
  enumerate every absent field.
- The theoretical peak bandwidth may be reported as configuration context when
  it is clearly identified as theoretical. Do not present it as a measured
  result or infer latency, bandwidth or overall performance from configuration
  values alone.

## Expected Profiling Characteristics
- System information and default benchmark results are available in the shared
  fixture, but this case asks specifically about system information.
- The platform has one NUMA node; topology descriptions must reflect the
  reported data.

## Scoring Guidance
- Pass:
  - Extracts several concrete, material system facts and accurately
    characterizes the platform.
  - Correctly describes topology and reports meaningful limitations.
  - Separates configuration observations from performance conclusions.
  - May omit one or more available categories, including operating-system or
    kernel details, when the remaining characterization is accurate and
    materially useful.
- Fail:
  - Invents fields, values, topology or a configuration fault.
  - Presents theoretical bandwidth as measured performance or claims measured
    performance from system information alone.
  - Omits or misrepresents enough available context to make the overall
    platform characterization materially misleading or too sparse to be useful.
  - Do not fail solely because any one field or category is omitted, because
    absent fields are not enumerated, or because optional firmware, mitigation,
    performance-feature or tool-version context is omitted.
