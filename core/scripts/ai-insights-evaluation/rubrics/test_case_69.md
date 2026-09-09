<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 69: Combined Dependent And Page-Granular Lookups

## Problem Summary
- Two distinct application kernels run sequentially: a dependent oversized-node
  traversal has material LLC/DRAM latency, while a sparse page-granular lookup
  has material TLB-walk overhead.

## ID
- `test_case_69`

## Public Intent (safe summary)
- Repeatedly run a dependent score-chain phase followed by an independent
  shuffled page-lookup phase in one thread.

## What's Wrong In Current Implementation
- `lookup_nodes` serializes accesses through a large shuffled node
  table whose oversized nodes contain little hot data.
- `lookup_paged_values` reuses about 1 MiB of accessed cache lines across 16K
  4 KiB pages for 128 rounds. This keeps the data cached while exceeding TLB
  coverage.
- These causes have different evidence and require separately scoped remedies.

## What The LLM Should Suggest
- Attribute material effective load latency and LLC/DRAM contribution to
  `lookup_nodes` and connect its visible dependent traversal to limited memory
  access overlap. Recommend reducing the effective memory latency or dependency
  when semantics permit. A layout change is acceptable when qualified by the
  available evidence.
- Attribute material TLB-walk activity to `lookup_paged_values` and recommend
  reducing its translation footprint. Compact storage or huge pages are
  acceptable qualified options.
- Keep the diagnoses and recommendations separate. Do not add the latency
  potential and TLB Walk Score or treat them as a combined elapsed-time cost.

## Expected Profiling Characteristics
- `lookup_nodes` has diagnosis-worthy sample and load coverage. Its
  average effective load latency is substantially above ideal L1 latency, LLC
  and DRAM make a meaningful contribution, and its potential improvement
  estimate is material. Its TLB-walk evidence is insignificant relative to
  its accesses.
- `lookup_paged_values` has diagnosis-worthy sample, load and TLB-access
  coverage. A substantial fraction of its TLB accesses require walks, producing
  material translation overhead.
- Differences in sample volume between the functions do not invalidate either
  diagnosis when each independently has material coverage and evidence.

## Scoring Guidance
- Pass:
  - Identifies both material problems, attributes each to the correct function
    and evidence set, and proposes an appropriately scoped remedy for each.
- Fail:
  - Reports only one of the two diagnosis-worthy problems.
  - Swaps the latency and TLB attribution or applies one generic recommendation
    to both functions.
  - Treats the dependent function's insignificant walk count as a material TLB
    issue.
  - Adds either renderer's estimated scores together or converts them into an
    unsupported elapsed-time impact or guaranteed speedup.
