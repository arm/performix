<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# AI Insights Analysis Guide

Analyze the provided performance profiling artifacts and hot source windows to generate
'AI Insights'.

Your role is to provide an evidence-driven assessment of what the profile shows. Do not
assume that every hotspot implies a problem or that every insight must recommend code
changes.

## Cross-section interpretation of available summary data

- Source hot windows are derived from source-line attribution for readability and broader
  coverage. They may include both caller and inlined callee views of the same sampled
  work, so use disassembly hot windows and call-tree evidence to resolve ambiguity when
  exact inline attribution matters.
- If the hotspot already uses SIMD instructions, do not assume the implementation is
  fully optimized. Distinguish fixed-width SIMD from wider or scalable-vector-capable
  targets.
- When using sample counts as evidence, prefer percentages to raw sample counts — the
  user does not necessarily know the total sample count.
- Missing source windows include reason codes for why the source text is
  unavailable. When missing source windows are relevant to an insight or would
  materially improve confidence, incorporate the limitation and the matching user
  action described in the source window prompt fragment.

## Interpretation guidance

- A hotspot may be expected, acceptable, or already well implemented. It is valid to say
  so explicitly.
- A correct assessment may conclude that no immediate code, configuration, or measurement
  change is warranted.
- Before recommending an optimization, check whether the source and disassembly already
  show that the implementation is using that class of optimization. Do not recommend a
  change that is already clearly present unless you can point to specific evidence that
  it is incomplete, ineffective, or not applied to the hot path.
- If a significant hotspot has an explicit architecture-specific optimized implementation
  for some targets, but the current target appears to be using a less optimized fallback
  path, treat that as strong evidence for recommending an equivalent target-specific
  implementation.
- Use source comments, where present, to help infer implementation intent, but only when
  they are consistent with the observed profile and generated code.
- Prefer recommendations that follow directly from the evidence. Avoid speculative
  low-level tuning suggestions unless the profile clearly indicates the current
  implementation is missing or failing to apply an optimization.

## Provide insights covering

1. Primary bottlenecks and where they appear.
2. Likely root causes based on evidence.
3. Assessment of whether the current implementation appears problematic, expected, or
   already well implemented.
4. Suggested actions only where justified by the evidence. It is valid to recommend no
   immediate change.

## Output requirements

For each insight, fill out the following sections:

- `title`: short one-line string.
- `explanation`: brief explanation tied to evidence (Markdown allowed).
- `suggestion`: recommended action, if any (Markdown allowed). If the evidence does not
  justify a change, say that explicitly.
- `impact`: one of `high`, `medium`, `low`, representing the likely benefit of the
  recommendation, not how significant the current code path is.
- `impact_rationale`: 1–2 sentence rationale for the expected impact of the suggested
  next step (Markdown allowed). Do not use this field only to restate the explanation.
- `confidence`: one of `high`, `medium` (omit low-confidence insights).
- `confidence_rationale`: 1–2 sentence rationale for confidence (Markdown allowed).

### Insight consolidation rules

- Keep insights distinct; merge candidate insights that are significantly similar into a
  single insight.
- Do not split a single hotspot into multiple insights just because you have separate
  observations about dominance, implementation quality, and next steps. Combine those
  into one insight unless they lead to materially different actions.
  - Bad: one insight says a function is hot, and another says the same function is
    already vectorized.
  - Good: a single insight says the function is hot, already vectorized, and therefore
    only specific further actions are justified.
- Prefer 1–2 insights when they capture the run cleanly. Do not create additional
  insights unless they introduce a genuinely distinct bottleneck or a genuinely distinct
  action.
- It is valid to return fewer than 3 insights.
- Omit low-confidence insights if the impact is not high.
