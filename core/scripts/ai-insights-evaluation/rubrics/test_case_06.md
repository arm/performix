<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 06: Stereo Envelope Follower Dependency Chain

## Problem Summary
- Insight target: loop-carried dependency that limits vectorization/parallel throughput.
- The kernel is framed as a stereo envelope follower (left/right channels), where each channel uses EMA-style smoothing.

## ID
- `test_case_06`

## Public Intent (safe summary)
- Run a stereo envelope follower over large left/right float buffers.
- Measure runtime and checksum deterministically.
- Select `envelope_alpha` once at runtime from a realistic range and reuse it for all measured runs.

## Code Structure
- Main measured path is `run_envelope_follower(...)`.
- Per-channel recurrence is implemented in `apply_envelope_follower(...)`.
- Signal setup uses `fill_signal(...)`; output validation uses `checksum_abs(...)`.

## EMA Filter
- EMA (Exponential Moving Average) is a first-order IIR smoothing filter: `y[n] = (1 - alpha) * y[n-1] + alpha * x[n]`.
- Small `alpha` gives stronger smoothing and slower response; larger `alpha` tracks input more quickly.
- Typical uses include audio envelope following (for compressors/limiters), sensor smoothing, and control loops.

## What's Wrong In Current Implementation
- Within each channel, each sample depends on prior EMA state in the same loop.
- The implementation processes left and right channels sequentially, leaving cross-channel parallelism unused.
- Per-channel SIMD/vectorization potential is constrained by the dependency chain.

## What The LLM Should Suggest
- Explicitly identify recurrence/loop-carried dependency as the core issue.
- Note that left/right channels are independent and can be processed concurrently (multi-threading and/or SIMD across channels).
- Suggest algorithmic reformulation only if semantics permit (for example blocked/parallel-prefix style alternatives) as candidate action.
- Avoid overclaiming simple SIMD as a direct fix without addressing dependency.

## Expected Profiling Characteristics
- Most samples should land in `run_envelope_follower(...)` / `apply_envelope_follower(...)`.
- Hot loop should show strong serial dependency shape.

## Scoring Guidance
- Pass:
  - Identifies hotspot and explicitly states the EMA recurrence is loop-carried/serial within each channel.
  - States that naive per-sample SIMD/autovectorization is not a direct fix without changing algorithm/semantics.
  - Recommends channel-level parallelism (left/right or chunked threading with state carry) and/or carefully qualified reformulation.
- Fail:
  - Identifies the hotspot and gives some useful actions, but the dependency/semantic constraint is weak, implied, or omitted.
  - Or mentions SIMD as a candidate but with caveats missing or incomplete.
  - Or misses the primary hotspot.
  - Or recommends straightforward SIMD/autovectorization of the per-sample EMA loop as primary fix without acknowledging loop-carried dependency/semantic tradeoff.
  - Or focuses mainly on secondary setup costs (RNG/memset) as the main issue.
