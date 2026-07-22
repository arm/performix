<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 15: Java Vector Width Portability

## Problem Summary
- Insight target: Java Vector API implementation hard-codes fixed vector width assumptions that can underuse Arm scalable/vector-capable targets.

## ID
- `test_case_15`

## Public Intent (safe summary)
- Run a dense arithmetic transform over two float arrays in Java.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The hot loop uses fixed `FloatVector.SPECIES_128`.
- The implementation does not adapt lane count to target preferred species.
- On Arm targets where preferred vector shape differs, this can leave throughput on the table.

## What The LLM Should Suggest
- Identify the transform loop as dominant hotspot.
- Suggest preferred-species (`FloatVector.SPECIES_PREFERRED`) and species-agnostic loop structure.
- Suggest validating vector codegen/performance on target JDK + architecture combinations.

## Expected Profiling Characteristics
- Most samples should land in `runTransform` and in Vector API/JIT-generated hot paths.
- Arithmetic loop body should dominate source attribution.

## Scoring Guidance
- Pass:
  - Identifies fixed-width vector strategy and recommends preferred-species or width-portable vector approach.
- Fail:
  - Identifies the hotspot but gives generic tuning suggestions without the width-portability insight.
  - Or misses the primary issue or suggests an unrelated main fix.
