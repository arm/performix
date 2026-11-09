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
- A high count of requests alone does not prove high impact. GC CPU samples,
  JFR pause durations or workload timings should support the recommendation.
  GC CPU cost and elapsed pause time are different quantities.

## What The LLM Should Suggest
- Distinguish `System.gc()`-caused events from ordinary collections in the
  selected JVM's JFR data. Report measured evidence such as collection count
  and GC CPU hotspots. Total and longest pause durations and work-unit timings
  are useful impact context, but omitting them alone is not a failure when
  other measured evidence supports the causal diagnosis and specific fix.
  Clear attribution in the explanation is sufficient; literal JFR field names
  or a separate cause statement are not required. Full GC alone does not prove
  an explicit request.
- Use source or sampled application paths to connect the per-unit call to the
  processing loop; exact method names or line numbers are not required.
  A JFR GC cause by itself does not identify the caller.
- When measured cost is material, recommend removing the per-unit explicit GC
  request and remeasuring collection cost and completed work. Avoid generic
  heap or collector tuning as the first recommendation.
- Base any JVM advice on recorded settings. Frequent GC alone does not show
  that the heap is too small or the collector is wrong.
- If the code cannot be changed, a flag workaround is acceptable. Explain how
  it could reduce the measured pauses and suggest testing it.

## Expected Profiling Characteristics
- JFR should show repeated explicitly caused collections with material pause
  cost in an accepted recording. Application samples should show the repeated
  work rather than a synthetic checksum loop dominating the entire capture.
- The fixed live set should not be described as a leak. Collection frequency,
  pause time and claims of observed throughput effects must be measured.
  Prospective benefits may be stated as expectations requiring validation.

## Scoring Guidance

- Pass:
  - Identifies explicit collections with measured evidence, connects
    the per-unit call to source or sampled application work, and recommends
    removing or justifying the call with an after-change measurement.
  - Assesses impact in proportion to the evidence. GC-heavy CPU samples can
    support removing explicit GC without pause durations. An imprecise impact
    title or incomplete quantification alone need not invalidate a supported
    diagnosis and actionable recommendation. Do not infer elapsed pause time
    from CPU sample percentages; claims of measured pause or throughput impact
    still require the corresponding measurements.
- Fail:
  - Treats explicitly caused collections as unavoidable heap pressure, invents
    a caller from JFR cause alone, or presents an unmeasured throughput gain as
    measured or guaranteed. A qualified expectation of improved throughput or
    latency with remeasurement advice is not such a claim.
  - Omits the JFR finding, calls the fixed live set a leak, or substitutes
    generic JVM tuning for the supported code change.
  - Invents settings, mixes different JVMs, or gives tuning or upgrade advice
    without supporting evidence.
