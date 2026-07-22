<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 30: .NET Arm64 ASCII Search

## Problem Summary
- Insight target: .NET JSON serialization spends a disproportionate amount of
  time in the Arm64 ASCII search path used by `System.Text.Json` escaping and
  raw-value validation.
- The testcase is pinned to a .NET 11 preview where this runtime path is known
  to be more expensive than expected on Arm64.

## ID
- `test_case_30`

## Public Intent (safe summary)
- Run a BenchmarkDotNet benchmark on .NET 11.
- Repeatedly serialize a mixed token list containing prebuilt JSON strings and
  dictionary-backed records into a `Utf8JsonWriter`.

## What's Wrong In Current Implementation
- The benchmark exercises `System.Text.Json` paths which repeatedly search
  spans for characters that require escaping or validation.
- On Arm64, the relevant `IndexOfAnyAsciiSearcher` helpers can become visible
  in the core `IndexOfAny` / `LastIndexOf` loop instead of disappearing into
  a small amount of vectorised library overhead.
- That makes ASCII character search a visible part of the JSON writer hot path.
- The benchmark's JSON shape is only the vehicle for triggering the runtime
  library issue. It is not primarily intended to recommend redesigning the
  application data model.

## What The LLM Should Suggest
- Identify `System.Private.CoreLib` / `System.Buffers.IndexOfAnyAsciiSearcher`
  as the important runtime hotspot when it accounts for a material share of
  samples.
- Connect that hotspot to `System.Text.Json` escaping, validation, or raw-value
  writing paths such as `Utf8JsonWriter` and `JsonWriterHelper`.
- Recommend validating the workload against a newer .NET runtime, checking for
  known runtime fixes, or investigating the Arm64 SIMD/codegen used by the
  runtime search helper, rather than treating the application benchmark loop as
  the first optimization target.
- It is acceptable to include a secondary note that typed/direct JSON writing
  may reduce serializer overhead, but that must not displace the runtime
  `IndexOfAnyAsciiSearcher` finding when it is hot.

## Expected Profiling Characteristics
- Significant samples may appear under `<jitted-code>` for
  `System.Buffers.IndexOfAnyAsciiSearcher`, especially `IndexOfAnyCore`,
  `IndexOfAny`, or related match-index helper paths.
- Call stacks should tie those samples back to `System.Text.Json` writer or
  serializer code, such as `Utf8JsonWriter`, `JsonWriterHelper`, or raw-value
  validation.
- Some samples in `TokenSerialization`, BenchmarkDotNet, and other
  `System.Text.Json` helpers are expected, but they are supporting context.
- If source attribution is available, the runtime source should point into
  `System.Private.CoreLib/src/System/SearchValues/IndexOfAnyAsciiSearcher.cs`.

## Scoring Guidance
- Pass:
  - Identifies the Arm64 `IndexOfAnyAsciiSearcher` runtime path as the key
    actionable issue when it is present in the profile.
  - Connects that runtime search path to JSON escaping or raw-value
    validation, rather than presenting it as an unrelated helper.
  - Recommends testing a newer runtime or investigating the Arm64 runtime search
    implementation as the likely fix direction.
- Fail:
  - Focuses mainly on BenchmarkDotNet harness overhead.
  - Treats the issue primarily as application-level dictionary/object JSON
    serialization design when `IndexOfAnyAsciiSearcher` is the visible hotspot.
  - Suggests only generic JSON micro-optimizations and misses the runtime
    Arm64 search implementation.
  - Recommends source-only changes to the benchmark without mentioning runtime
    version validation or runtime-library investigation.
