<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 47: Large Records for Small Lookups

## Problem Summary
- CPU category and cause: Backend Bound — D-cache misses. The CPU has the
  instructions but spends much of its time waiting for data because every
  lookup needs only eight bytes from a much larger record.

## ID
- `test_case_47`

## Public Intent (safe summary)
- Process approximately 131,072 records in a repeatable shuffled order within
  small groups.
- Each record is approximately 512 bytes, but the processing loop frequently
  reads only one eight-byte value.

## What's Wrong In Current Implementation
- Storing the frequently read value inside each large record brings nearby,
  rarely used record data into the data cache.
- The full records occupy about 64 MiB. The frequently read values need only
  about 1 MiB and fit in the CPU's nearby Level 2 cache.

## What The LLM Should Suggest
- Pack or gather the frequently read values into contiguous storage, split
  frequently used and rarely used fields, or propose an equivalent layout
  change that reduces cache traffic.
- Keep the recommendation tied to the measured indexed traversal and its cache
  behaviour.

## Expected Profiling Characteristics
- Backend Bound is the largest top-level CPU category and leads the next
  category by at least 15 percentage points.
- Data-cache miss and data-fetch measurements provide the strongest supporting
  evidence.
- Data address translation cache (DTLB) measurements are not the main evidence.
  Case 51 covers delays caused by translating virtual addresses.
- Samples point to the record-processing loop.

## Scoring Guidance
- Pass:
  - Identifies data-cache or data-fetch delays after the CPU has received the
    instructions.
  - Connects the measurements to the indexed pointer traversal and its poor
    spatial locality.
  - Recommends contiguous storage for the frequently read values, a split
    between frequently and rarely used fields, or an equivalent concrete layout
    change.
- Fail:
  - Says that virtual-address translation is the main cause.
  - Recommends loading data early without reducing the cache footprint of the
    indexed traversal.
  - Gives a generic memory answer without connecting it to the measured cache
    evidence and hot loop.
