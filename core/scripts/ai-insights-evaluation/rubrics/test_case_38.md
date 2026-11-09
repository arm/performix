<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 38: Integer-Dominant Control-Flow-Heavy Scalar Loop

## Problem Summary
- Insight target: dynamic Instruction Mix run with one clearly dominant local
  hot function whose measured instruction mix is mostly scalar integer work,
  with elevated branch activity and negligible SIMD usage.

## ID
- `test_case_38`

## Public Intent (safe summary)
- Run a deterministic local C++ byte-classification loop over array-backed
  data.
- Keep the measured work in one dominant function and report stable runtime
  output.

## What's Wrong In Current Implementation
- The case is not primarily about proving a single clear implementation bug.
- Instead, it is meant to test whether the model can recognise a
  control-flow-heavy scalar hot loop from dynamic Instruction Mix evidence and
  from the sampled branch-oriented disassembly.
- The hot path should not be described as SIMD-heavy, and the response should
  not overstate branch share as the majority instruction class when the run is
  still mostly integer work.

## What The LLM Should Suggest
- Identify the dominant hot function or clearly dominant hot path from dynamic
  evidence.
- Explain how the measured integer-led mix and branch activity relate to the
  hot path's per-byte conditional classification and state updates.
- If suggesting follow-up analysis, keep it evidence-backed and lightweight,
  such as checking source or disassembly to understand the branch structure or
  considering whether data-dependent classification logic could be simplified.
- Avoid inventing an unrelated bottleneck that is not supported by the
  Instruction Mix evidence.

## Expected Profiling Characteristics
- Dynamic function-level attribution should identify one clearly dominant local
  function.
- The hot function should show high integer share, elevated branch activity,
  low load/store share relative to the arithmetic/control work, and little
  meaningful SIMD usage.
- The run should provide enough `samples.count.self` coverage for the dominant
  function to make attribution meaningful.

## Scoring Guidance
- Pass:
  - Identifies the dominant hot function or clearly dominant hot path from the
    dynamic evidence.
  - Connects the integer-led mix and branch activity to the conditional processing
    in the hot path rather than describing SIMD-heavy execution. Equivalent
    source-based explanations count without the labels "scalar", "control-flow-heavy"
    or "elevated"; an unexplained list of percentages alone is insufficient.
  - Does not require branch instructions to be the majority instruction class.
  - Keeps any follow-up recommendation cautious and evidence-backed.
- Fail:
  - Treats the run as SIMD-led or vectorized.
  - Describes the hotspot as mainly memory-load/store dominated without
    matching evidence.
  - Invents an unrelated bottleneck that is not supported by the Instruction
    Mix evidence.
  - Forces a strong branch-misprediction diagnosis without caveats or without
    tying it to the measured control-flow-heavy shape.
