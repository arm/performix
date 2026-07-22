<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 09: Allocation in Hot Loop

## Problem Summary
- Insight target: repeated dynamic allocation while tokenizing and counting words in the hottest loop.

## ID
- `test_case_09`

## Public Intent (safe summary)
- Tokenize whitespace-delimited text lines and count word frequencies.
- Measure runtime and checksum deterministically.

## What's Wrong In Current Implementation
- The hot parse loop materializes a new owned `std::string` token for every parsed word.
- Tokens are intentionally long (beyond typical SSO), so each token copy tends to allocate.
- Allocation/deallocation is repeated for all tokens, even when vocabulary is small and words repeat.

## What The LLM Should Suggest
- Identify per-token owned string construction in the hot loop as the core issue.
- Suggest parsing with `std::string_view` (or pointer/length spans) and heterogeneous lookup to avoid per-token allocations.
- Suggest keeping ownership allocation only for first-seen words, with reused/non-owning paths for repeated tokens.

## Expected Profiling Characteristics
- Samples should show significant time in allocator/string runtime paths plus `run_token_count`.
- Tokenization and hash/map logic should appear, but allocation-heavy string handling should be prominent.

## Scoring Guidance
- Pass:
  - Correctly identifies hot-loop allocation and proposes concrete reuse/preallocation fixes.
- Fail:
  - Notes the hotspot but gives generic optimization advice.
  - Or misses the primary issue or suggests an unrelated main fix.
