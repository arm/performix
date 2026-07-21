<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 24: PyTorch SAM-like Inference With Unoptimized Softmax on Arm

## Problem Summary
- Insight target: validate AI Insights on a real PyTorch/ExecuTorch-adjacent CV inference workload where an intentionally older PyTorch version leaves softmax under-optimized on Arm.
- This testcase is derived from Arm's SME2 + ExecuTorch writeup, but it intentionally profiles a pre-optimization PyTorch eager path rather than the later Arm-kernel-accelerated path.
- The public manifest uses the `sme-executorch-profiling` repository as the workload source, but pins an older CPU PyTorch build and runs a PyTorch eager benchmark rather than the repository's own profiling pipeline so Performix is the only profiler in the loop.

## ID
- `test_case_24`

## Public Intent (safe summary)
- Clone Arm's `sme-executorch-profiling` repository on the target.
- Reuse its `efficient_sam` model registration / model factory path.
- Run a small PyTorch eager-inference benchmark for that model on a single CPU thread.

## Why This Is The Right Proxy
- The Arm blog post and accompanying repository describe a SAM-style segmentation workload where the pre-optimized runtime is compute-dominated, especially by convolution and GEMM/iGEMM kernels, and where later Arm SME2 acceleration shifts the bottleneck toward data movement.
- The checked-in repository does not currently ship a ready-to-run in-tree SqueezeSAM workload for Linux in the same way it documents macOS/Android pipeline runs.
- It does, however, expose a ready-to-run `efficient_sam` model through the same repository and the same export/model-factory surface.
- Using that model in PyTorch eager mode with the pinned older PyTorch preserves the key diagnostic pattern we care about:
  - a segmentation-style model,
  - a realistic opportunity for an older framework/kernel stack to expose a missing Arm optimization in softmax,
  - and a realistic opportunity for the LLM to recommend a more optimized Arm execution path rather than Python-level micro-tuning.

## What's Wrong In Current Implementation
- The measured workload is intentionally the "before" state:
  - PyTorch eager inference of a SAM-like segmentation model on CPU,
  - using an older PyTorch build that lacks the newer Arm-optimized softmax path,
  - and without the later Arm-optimized ExecuTorch SME2 execution path highlighted in the blog.
- For this testcase, the expected issue is not Python source inefficiency.
- The expected issue is that softmax / exp work is disproportionately expensive on Arm in this older stack, even though GEMM remains a significant tensor-compute cost.
- The correct optimization direction is therefore an Arm-aware framework/backend/kernel improvement for the softmax-heavy path rather than tweaking benchmark scaffolding or Python glue.

## What The LLM Should Suggest
- Identify softmax / exp style tensor compute as the primary bottleneck when that is what the profile shows, ideally via ATen / libm / vector-kernel names that appear in the profile.
- Recognize that GEMM / convolution kernels may still be significant, but are not necessarily the first optimization target if they already appear to be using Arm/SVE kernels.
- Recognize that this is a framework/backend/kernel-efficiency problem on Arm, not primarily a Python control-flow problem.
- Suggest a realistic Arm-specific next step such as:
  - moving from the older eager PyTorch path to a newer PyTorch / ExecuTorch / Arm-optimized deployment path,
  - evaluating whether a newer Arm-optimized softmax implementation, or an explicit fused/vectorized AArch64 softmax kernel for this operator path, is available,
  - or otherwise using the Arm-optimized path described in the blog rather than focusing on Python-level changes.
- Stronger answers may also note that once softmax is improved, GEMM / data movement may become relatively more important again.

## Expected Profiling Characteristics
- The hot path should be dominated by native tensor compute rather than Python interpreter overhead.
- Significant samples are expected in:
  - softmax / exp kernels or math-library routines,
  - GEMM / iGEMM / linear-style kernels,
  - surrounding tensor runtime glue.
- Python frames may still appear at the top of the stack, but they should mostly lead into native compute kernels rather than dominate self time themselves.
- The profile should look compute-heavy first; it should not already look like a data-movement-dominated "after optimization" run.
- In the current pinned configuration, a large softmax / exp path is expected and should be treated as the primary optimization target if it outweighs GEMM in the profile.

## Scoring Guidance
- Pass:
  - Correctly identifies softmax / exp style model compute as the primary hotspot when that is what the profile shows.
  - Recognizes that GEMM / convolution remain significant but are not the first issue to fix if they already appear to be using Arm/SVE kernels.
  - Treats this as a backend / kernel optimization opportunity on Arm rather than a Python-level issue.
  - Recommends a realistic Arm-oriented inference-path improvement, such as moving to a newer Arm-optimized PyTorch / ExecuTorch / Arm-kernel direction, or explicitly calling for a fused/vectorized AArch64 softmax implementation in the current path.
- Fail:
  - Correctly identifies the model compute hotspot, but gives only generic "optimize PyTorch" or "vectorize" advice without clearly connecting it to the Arm backend / kernel path.
  - Focuses mainly on Python glue or incidental runtime noise.
  - Insists GEMM / convolution is the primary issue despite the profile showing softmax / exp dominating.
  - Recommends unrelated micro-optimizations that do not address the actual inference compute path.
  - Suggests only generic fusion, lower precision, specialized kernel, or alternative backend ideas without explicitly connecting them to an Arm-oriented softmax or runtime improvement.
