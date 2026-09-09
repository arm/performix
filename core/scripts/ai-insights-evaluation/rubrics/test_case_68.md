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
  Each value occupies a separate normal page and huge pages are explicitly
  disabled.

## What's Wrong In Current Implementation
- `lookup_paged_values` reuses about 1 MiB of accessed cache lines across 16K
  4 KiB pages for 32 rounds. This keeps the data cached while exceeding TLB
  coverage.

## What The LLM Should Suggest
- Connect TLB access count, walk count, walk percentage and average walk latency
  to `lookup_paged_values`.
- Recommend reducing the translation footprint. More compact storage or huge
  pages are acceptable options. If huge pages are the primary action, qualify
  platform support and memory or allocation trade-offs. A brief conditional
  caveat is sufficient when they are one of several alternatives.
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
  - Identifies a material TLB-walk problem, attributes it to
    `lookup_paged_values`, distinguishes it from data-cache latency and
    proposes a supported reduction in translation footprint.
- Fail:
  - Describes the primary problem as DRAM or general cache-miss latency.
  - Presents huge pages as a guaranteed or unconditional primary fix.
  - Gives generic memory advice without using the TLB measurements and source
    attribution.
  - Presents the TLB Walk Score as measured elapsed time, exact lost cycles or
    guaranteed speedup.
