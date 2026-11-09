<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 51: Tenant Headers Spread Across Pages

## Problem Summary
- CPU category and cause: Backend Bound — DTLB misses and page-table walks. The
  CPU spends much of its time waiting for address translations because it reads
  one frequently used header from each of several thousand page-sized tenant
  blocks.

## ID
- `test_case_51`

## Public Intent (safe summary)
- Model tenant or session state stored in separate allocations. Each allocation
  has a small, frequently read header and a larger amount of rarely read data.
- Read the headers before measurement, place them at different offsets within
  their pages and ask Linux to keep the normal page size.

## What's Wrong In Current Implementation
- Putting each header on a different page means the CPU must remember many
  virtual-to-physical address translations, even though the headers themselves
  fit in the nearby Level 2 cache.
- The full tenant blocks may need separate lifetimes, but their frequently read
  headers do not need to remain spread across separate memory mappings.

## What The LLM Should Suggest
- Pack or gather the frequently read values into contiguous storage, group
  allocations to reduce the translation footprint, or propose an equivalent
  concrete layout change.
- Larger memory pages are acceptable when separate page-sized storage must
  remain.

## Expected Profiling Characteristics
- Backend Bound is the largest top-level CPU category and leads the next
  category by at least 15 percentage points.
- Data address translation cache (DTLB) misses per thousand instructions, or
  page-table lookup measurements, are significant. Level 2 and last-level data
  cache misses are less important.
- Samples point to the tenant header lookup.
- The packed-header version substantially reduces address translation misses
  and runtime.

## Scoring Guidance
- Pass:
  - Distinguishes delays translating virtual addresses from general
    main-memory or data-cache delays.
  - Connects significant DTLB walks to sparse or indirect accesses across a
    large address-translation working set.
  - Recommends contiguous storage for the frequently read values, grouped
    allocation, suitable larger memory pages or an equivalent concrete change
    that reduces the translation footprint.
- Fail:
  - Says that the data is simply too large for the data cache.
  - Recommends larger pages or generic memory tuning without identifying
    address-translation pressure.
  - Gives generic memory-layout advice without address translation evidence.
