<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 20: Java Heap-Constrained Cache Churn

## Problem Summary
- Insight target: the workload runs with a fixed 2 GB heap on a machine with
  substantially more memory available. The heap-derived cache budget is too
  small for its hot working set, causing repeated chunk rebuilds and associated
  GC/runtime work alongside the measured `runPass` scan.

## ID
- `test_case_20`

## Public Intent (safe summary)
- Decode a large logical set of chunks from a byte stream.
- Reuse decoded chunks through an in-process cache.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The cache capacity is derived from the effective JVM max heap, so a small
  heap gives the application a much smaller effective cache.
- The access pattern repeatedly revisits a hot chunk set larger than the cache
  that fits under the constrained heap setting.
- With `-Xms2g -Xmx2g`, the workload rebuilds many decoded chunks instead of
  reusing them, which amplifies both application-side recomputation and
  GC/runtime overhead.
- The target machine has materially more RAM available. A larger heap could
  increase the derived cache budget; changing the cache budget directly may
  also work if the resulting live set fits safely in memory.

## What The LLM Should Suggest
- Identify the application-level cache churn / chunk rebuild path as material
  work while accurately ranking any stronger sampled hotspot.
- Use JFR data from the workload's Java process to show that garbage collection
  happens often or pauses the program for a meaningful amount of time. Include
  heap measurements if available. Explain how repeatedly creating chunks could
  contribute to this, without calling that pattern a memory leak.
- Suggest checking whether the cache can hold the hot set and increasing its
  effective capacity if needed, for example with a larger heap or revised cache
  budget. Comparing rebuild and GC cost and memory use before and after the
  change would help validate it, but a detailed test plan is optional.
- Optionally suggest secondary mitigations such as reducing rebuild cost,
  shrinking the hot working set, or improving cache effectiveness.

## Expected Profiling Characteristics
- Significant samples in `Main::runPass` and `Main::buildChunk`.
- Under the constrained heap, visible time in GC/runtime worker paths is
  expected because the application is allocating and discarding many decoded
  chunks.
- If the larger effective cache holds more of the working set, rebuild and GC
  pressure should fall; validate this with runtime and GC measurements.

## Scoring Guidance
- Pass:
  - Finds repeated chunk rebuilding in CPU samples and shows how it compares
    with other sampled work. Uses JFR from the same workload to report frequent
    garbage collection or meaningful pause time. Connects the two without
    claiming JFR alone proves the cache is too small.
  - Connects the cache shortfall to repeated rebuilds and suggests checking or
    increasing capacity so more of the hot set stays cached. An exploratory
    suggestion counts when it explains the current shortfall; exact heap
    settings and a before-and-after test are not required.
- Fail:
  - Omits JFR, cites only the existence of a recording, or invents GC/heap
    measurements that the recording does not support.
  - Identifies the rebuild hotspot but offers only generic allocation or
    compute advice without connecting a suggested improvement to cache capacity.
  - Or misses the cache/rebuild issue, or suggests an unrelated primary fix.
