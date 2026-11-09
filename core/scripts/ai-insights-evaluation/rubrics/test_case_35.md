<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 35: Transient CPU Spike Control

## Problem Summary
- Insight target: one short CPU spike followed by recovery does not establish a
  sustained resource bottleneck.

## ID
- `test_case_35`

## Public Intent
- Record an otherwise idle system with one short period of CPU activity.
- Test whether analysis distinguishes a transient maximum from sustained
  pressure.

## Private Workload Design
- The launcher idles for 20 seconds, runs stress-ng matrix multiplication on
  every CPU for 3 seconds, then idles for another 30 seconds.
- This is a negative control for sustained-pressure diagnosis: the spike is
  visible at one-second sampling intervals but does not establish a sustained
  CPU bottleneck. The stress-ng version is pinned to `V0.21.04`.

## What's Wrong In Current Implementation
- No sustained performance problem is present in the recording.
- Treating the isolated CPU spike as a capacity problem would overstate the
  available evidence.

## What The LLM Should Suggest
- Quantify the short CPU peak.
- Distinguish the isolated spike from sustained pressure and do not present it
  as a demonstrated bottleneck.
- Conditional diagnostic profiling is acceptable. Keep any optimisation
  suggestion conditional on measured workload impact.

## Expected Profiling Characteristics
- An extended idle baseline precedes a short system-wide CPU peak.
- CPU utilisation returns to idle for the remainder of the recording.
- Memory, storage and network metrics do not show sustained pressure.

## Scoring Guidance
- Pass:
  - Identifies and quantifies the short CPU peak; exact recovery timings are optional.
  - Distinguishes the isolated peak from sustained pressure and does not
    present it as a demonstrated bottleneck.
  - Keeps any follow-up profiling or optimisation conditional on measured
    workload impact.
  - Avoids unnecessary optimisation advice.
- Fail:
  - Invents a bottleneck from the transient maximum.
  - Recommends CPU, storage or memory changes without supporting evidence.
  - Identifies or guesses the hidden workload implementation.
