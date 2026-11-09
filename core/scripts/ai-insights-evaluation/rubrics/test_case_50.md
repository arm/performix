<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 50: Route Score Lookups

## Problem Summary

- CPU category and cause: Backend Bound — latency from scattered score-table
  reads. The route-scoring loop spends much of its time waiting for scores from
  a table with a large memory footprint.
- Scores must be combined in the supplied order so the fingerprint remains
  unchanged.

## ID

- `test_case_50`

## Public Intent (safe summary)

- Read scores for a shuffled route stream and combine them in the supplied
  order.
- Keep the stream unchanged while looking for a way to hide the cost of reading
  scores from the large table.

## What's Wrong In Current Implementation

- Scattered score-table reads frequently wait for data or an address mapping
  that is not already available to the CPU.
- The loop waits for each score before doing the inexpensive fingerprint
  update, even though later route identifiers are already known.

## What The LLM Should Suggest

- Recommend focused, order-preserving changes that reduce or hide score-read
  latency, such as:
  - arrange scores for contiguous consumption in route order, without changing
    the fingerprint order; or
  - load scores for future route identifiers early, using a small look-ahead
    buffer or software prefetch while preserving result order.

## Scoring Guidance

- Pass:
  - Makes one concrete assessment that delays from the large, scattered
    score-table reads are the underlying problem.
  - Recommends a concrete, order-preserving remedy and explains how it reduces
    or hides score-read latency. Do not fail solely on the number of relevant remedies.
  - Uses the available profile evidence to support the conclusion. No specific
    measurement or profiling recipe is required.
- Fail:
  - Identifies only the expensive source line but cannot choose the cause or a
    concrete next step.
  - Lists several possible causes or asks for another profile before choosing
    what to change.
  - Suggests sorting or grouping route identifiers in a way that changes their
    order or the fingerprint. A generic "if semantics allow" caveat does not
    excuse a change incompatible with the required order.
  - Gives only a generic instruction to improve locality.
