<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 25: PyTorch bf16 Broadcast mul/div Microbench

## Problem Summary
- Insight target: validate AI Insights on a small PyTorch CPU pointwise workload where bfloat16 broadcast arithmetic dominates and the right optimization direction is better Arm dtype-specialized kernels, not Python-level changes.
- This testcase is derived from the author's `mul.py` / `div.py` benchmarks and is motivated by the same class of PyTorch Arm pointwise-kernel specialization work as PyTorch PR #177867 (`Add BF16 support for arm cpu support`, <https://github.com/pytorch/pytorch/pull/177867>).
- That PR targeted machines which support bfloat16 but do not have SVE. To highlight the same issue clearly, this testcase is best run on a BF16-capable AArch64 target without SVE so PyTorch cannot take an SVE-specialized path instead.

## ID
- `test_case_25`

## Public Intent (safe summary)
- Create a Python virtual environment on the target.
- Install PyTorch into that environment.
- Run embedded copies of the author's `common.py`, `mul.py`, and `div.py` microbench code in a combined benchmark driver.
- Benchmark `torch.mul` and `torch.div` on CPU with bf16 broadcast inputs using the default shapes `32768` and `1`.

## What's Wrong In Current Implementation
- This workload is intentionally a narrow eager CPU microbenchmark rather than a larger model.
- The measured work is expected to be dominated by PyTorch native pointwise kernels for bf16 broadcast multiply/divide, not by Python orchestration.
- The expected issue is that these hot CPU kernels may lack the best Arm-specific dtype/capability-specialized implementation, causing more generic vector/scalar fallback behavior than is ideal for bfloat16 arithmetic.
- On SVE-capable targets this issue may be partially masked by SVE-specialized kernels, so the benchmark signal is strongest on BF16-capable non-SVE hardware.
- Because the benchmark is already minimal, recommendations about data loading, allocation policy, model structure, or Python control flow are not the main fix.

## What The LLM Should Suggest
- Identify pointwise tensor arithmetic (`torch.mul` / `torch.div`) as the real hotspot.
- Recognize that the issue is in native CPU operator implementation quality for bf16 broadcast arithmetic on Arm, not in Python loop structure.
- Suggest a realistic next step such as:
  - using a PyTorch/runtime build with better Arm half-precision vector support,
  - improving or enabling dtype-specialized Arm kernels / dispatch for these operators,
  - or validating whether a newer PyTorch version has a more specialized implementation for the same hot path.
- Avoid generic advice that does not engage with the actual pointwise-kernel issue.

## Expected Profiling Characteristics
- The hot path should be dominated by native PyTorch / ATen CPU operator code.
- Significant time should land in pointwise arithmetic kernels and nearby tensor runtime helpers rather than Python interpreter frames.
- Both multiply and divide should be visible in the same run, with divide often somewhat heavier because it is more expensive arithmetic on the same tensor shapes.

## Scoring Guidance
- Pass:
  - Identifies the workload as dominated by CPU pointwise bf16 multiply/divide kernels.
  - Treats this as a native operator / Arm dtype-specialization opportunity rather than a Python-level problem.
  - Recommends a plausible runtime/kernel-path improvement such as a newer Arm-specialized PyTorch build or better dtype-specific dispatch.
- Fail:
  - Correctly identifies the hot pointwise kernels, but gives only generic "vectorize / optimize PyTorch" advice without clearly linking it to Arm bfloat16 kernel specialization.
  - Focuses mainly on Python overhead or incidental setup noise.
  - Misses the pointwise-kernel nature of the workload.
  - Recommends unrelated model or I/O changes.
