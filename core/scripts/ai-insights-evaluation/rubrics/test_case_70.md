<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 70: Compact Value Lookup Control

## Problem Summary
- Negative Memory Access control: a well-sampled compact lookup has near-ideal
  L1 load latency and no material TLB-walk evidence.

## ID
- `test_case_70`

## Public Intent (safe summary)
- Repeatedly look up a bounded set of values in a deterministic shuffled order
  using a compact values-and-index working set sized to fit in L1.

## What's Wrong In Current Implementation
- The accepted fixture contains no diagnosis-worthy application memory-access
  problem. A hot function, many loads or a nonzero cumulative ranking estimate
  does not prove otherwise.

## What The LLM Should Suggest
- State that the latency and TLB evidence do not support a material Memory
  Access diagnosis for `lookup_values`.
- Avoid recommending packing, cache blocking, prefetching or huge pages because
  the observed latency, cache-level and TLB evidence do not support those
  changes.
- If more confidence is required for a broader application conclusion,
  recommend profiling a representative workload rather than inventing a cause.
  Do not claim that this run proves the program is globally optimal.

## Expected Profiling Characteristics
- `lookup_values` has enough SPE samples and sampled loads that the
  negative conclusion is not caused by insufficient coverage.
- Average effective load latency is at or near the run's ideal L1 latency, and
  sampled loads are overwhelmingly served by L1 with no material higher-level
  contribution.
- There is no material TLB-walk evidence relative to sampled TLB accesses.
- The potential improvement estimate may be nonzero because a tiny
  difference from ideal latency is multiplied by many loads. In the context of
  the latency distribution, it is not evidence of a material optimisation
  opportunity.

## Scoring Guidance
- Pass:
  - Reports that there is no supported material latency or TLB diagnosis and
    avoids an unnecessary memory-layout recommendation.
- Fail:
  - Diagnoses a memory problem from function rank, sample volume, load count or
    the nonzero potential score while ignoring the near-ideal latency and L1
    distribution.
  - Recommends huge pages, packing, prefetching or cache blocking as the main
    fix without supporting evidence.
  - Claims that zero sampled walks or low observed latency proves zero overhead
    in all executions or guarantees globally optimal performance.
