<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 68: Page-Granular Value Lookups

## Problem Summary
- One useful value per normal page creates a translation footprint much
  larger than the TLB, causing material page-table-walk overhead despite low
  data-load latency.

## ID
- `test_case_68`

## Public Intent (safe summary)
- Repeatedly look up many values in a deterministic shuffled order.

## What's Wrong In Current Implementation
- `lookup_paged_values` reuses about 1 MiB of accessed cache lines across 16K
  4 KiB pages for 32 rounds. This keeps the data cached while exceeding TLB
  coverage.
- The setup source is intentionally unavailable. The one-value-per-page layout,
  page size and disabled huge pages are private fixture facts, not knowledge
  required of the model. Assess its recommendations against available evidence.

## What The LLM Should Suggest
- Connect TLB access count, walk count, walk percentage and average walk latency
  to `lookup_paged_values`.
- Recommend a concrete translation-footprint reduction, or check the current
  mapping/page size to choose a conditional layout, lookup-order or large-page
  experiment. Experiments should acknowledge unknown layout and measure TLB
  walks and runtime, plus memory use for large pages. Such a large-page experiment
  needs no separate platform-support or allocation caveats, even as the primary action.
- Distinguish translation overhead from an LLC or DRAM data-cache problem.
- Reporting the TLB Walk Score is optional. If included, treat it as a ranking
  estimate, not measured elapsed time or an exact speedup opportunity.

## Expected Profiling Characteristics
- `lookup_paged_values` has enough SPE samples, sampled loads and sampled TLB
  accesses to support an application-level diagnosis.
- A substantial fraction of its sampled TLB accesses require walks. The walk
  count and average walk latency establish material translation overhead
  relative to other application functions.
- Average effective data-load latency remains close to cache latency. Most
  sampled loads are served by L1 or L2, with little higher-level contribution
  and no material DRAM contribution.

## Scoring Guidance
- Pass:
  - Identifies material TLB walks, attributes them to `lookup_paged_values`,
    distinguishes them from data-cache latency and gives a next step as described
    above. Do not reject conditional reordering based on private layout facts.
- Fail:
  - Describes the primary problem as DRAM or general cache-miss latency.
  - Presents huge pages as a guaranteed or unconditional primary fix.
  - Claims reordering necessarily reduces distinct pages or guarantees a gain
    without evidence of the layout and access pattern.
  - Gives generic memory advice without using the TLB measurements and source
    attribution.
  - Presents the TLB Walk Score as measured elapsed time, exact lost cycles or
    guaranteed speedup.
