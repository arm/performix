<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 72: Explicit GC in Repeated Work

## Problem Summary
- Insight target: requesting GC after every unit of work creates avoidable
  collection pauses. JFR identifies the explicit cause and measures its cost.

## ID
- `test_case_72`

## Public Intent (safe summary)
- Repeatedly process a fixed live data set and a temporary batch for 35 seconds.
- Request `System.gc()` after each unit and report completed units and their
  measured durations.

## What's Wrong In Current Implementation
- Explicit GC is on the repeated processing path, regardless of whether the
  fixed heap needs a collection. It can interrupt each unit and reduce useful
  work completed in the run.
- A high count of requests alone does not prove high impact. The JFR pause
  duration and workload timing must support the ranking.

## What The LLM Should Suggest
- Distinguish `System.gc()`-caused events from ordinary collections in the
  selected JVM's JFR data. Quantify their count, total pause, and longest pause.
- Use source or sampled application paths to identify the per-unit call site;
  a JFR GC cause by itself does not identify the caller.
- When measured cost is material, recommend removing the per-unit explicit GC
  request and remeasuring collection cost and completed work. Avoid generic
  heap or collector tuning as the first recommendation.

## Expected Profiling Characteristics
- JFR should show repeated explicitly caused collections with material pause
  cost in an accepted recording. Application samples should show the repeated
  work rather than a synthetic checksum loop dominating the entire capture.
- The fixed live set should not be described as a leak. Collection frequency,
  pause time, and any throughput effect must be measured, not assumed.

## Scoring Guidance
- Pass:
  - Identifies explicit collections, quantifies their measured cost, connects
    the per-unit call to source or sampled application work, and recommends
    removing or justifying the call with an after-change measurement.
  - Ranks the issue in proportion to the observed pause or work-unit cost.
- Fail:
  - Treats explicitly caused collections as unavoidable heap pressure, invents
    a caller from JFR cause alone, or asserts an unmeasured throughput gain.
  - Omits the JFR finding, calls the fixed live set a leak, or substitutes
    generic JVM tuning for the supported code change.
