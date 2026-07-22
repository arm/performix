<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 08: Indirect Call in Hot Loop

## Problem Summary
- Insight target: indirect function dispatch inside the hottest loop.

## ID
- `test_case_08`

## Public Intent (safe summary)
- Run an indirect-dispatch transform over large float buffers.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The hot loop selects and invokes a virtual interface method per element.
- Indirect call dispatch inhibits straightforward optimization/inlining in the tight path.
- The per-element indirection adds front-end/control overhead.

## What The LLM Should Suggest
- Identify indirect call dispatch in the hot loop as the core issue.
- Suggest devirtualization/specialized loop paths (split loops by selector, direct-call fast paths) as candidate action.
- Suggest validating with profile deltas after removing indirection.

## Expected Profiling Characteristics
- Most samples should land in `run_pipeline` plus concrete kernel implementations.
- Hotspot should show repeated indirect-call pattern.

## Scoring Guidance
- Pass:
  - Flags indirect-call overhead and gives concrete specialization/deindirection options.
- Fail:
  - Finds the hotspot but gives generic advice.
  - Or misses the primary issue or suggests an unrelated main fix.
