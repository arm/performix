<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 27: Orchard CMS Under .NET HTTP Load

## Problem Summary
- Insight target: validate AI Insights on a realistic managed .NET web workload where the dominant bottlenecks may come from Orchard, ASP.NET Core, or the .NET runtime under sustained HTTP traffic.
- This testcase is intentionally broad in v1: it is meant to exercise evidence-driven diagnosis on a real application stack rather than encode one narrowly predefined bug.

## ID
- `test_case_27`

## Public Intent (safe summary)
- Build and run Orchard CMS from a pinned upstream revision using a pinned .NET SDK/runtime.
- Start the Orchard app as the profiled workload.
- Drive sustained HTTP load against the `/about` endpoint using a separate load generator process outside the profiled workload tree.

## What The LLM Should Suggest
- Identify the dominant measured hotspots and connect them to the application/runtime/framework path actually shown by the profile.
- Distinguish between:
  - Orchard application/framework work,
  - ASP.NET Core / Kestrel / middleware overhead,
  - .NET runtime/JIT/GC/runtime-library cost,
  - and external benchmark/setup noise.
- Recommend only evidence-driven next steps. It is valid to conclude:
  - the hotspot is expected for this workload,
  - a runtime/framework issue dominates rather than Orchard-specific code,
  - or the benchmark/setup shape should be adjusted before drawing stronger conclusions.
- Avoid generic .NET optimization advice unless it clearly matches the observed hotspots.

## Expected Profiling Characteristics
- Representative runs may show a mix of:
  - Orchard CMS / OrchardCore symbols,
  - ASP.NET Core / Kestrel / routing / middleware symbols,
  - managed .NET runtime/JIT/GC/runtime-library cost,
  - and some HTTP load / startup / autosetup effects.
- The exact dominant hotspot is not fixed in advance for v1; the score should follow the observed profile, not a hidden preconceived answer.

## Scoring Guidance
- Pass:
  - Correctly identifies the dominant measured hotspots.
  - Connects those hotspots to the right layer (application, framework, runtime, or benchmark/setup).
  - Recommends realistic next steps that follow directly from the profile.
  - Avoids overclaiming or forcing an Orchard-specific diagnosis when the runtime/framework dominates.
- Fail:
  - Gives generic .NET or web-performance advice without tying it to the measured evidence.
  - Focuses on clearly secondary noise while missing the dominant hotspot.
  - Recommends changes contradicted by the profile.
  - Treats the load generator or external sidecar process as part of the profiled Orchard workload.
