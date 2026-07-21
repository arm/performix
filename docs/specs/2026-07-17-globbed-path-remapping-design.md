<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Globbed Path Remapping

## Summary

`RemapGlobbedPath` maps a concrete remote path into a concrete local destination path.

When the remote side is concrete, remapping is a direct identity check. When the remote side is globbed, remapping is defined by a shared wildcard-containing suffix: the local template and the remote-base template must have the same suffix starting at the first path segment that contains `*`. Remapping then preserves that concrete suffix while replacing only the concrete prefix.

This design keeps globbed remapping predictable and auditable. Wildcarded segments are mirrored, not transformed.

## Scope

This spec defines the current remapping contract used when a concrete matched path must be mapped from a remote template into a local destination template.

It applies to:

- direct calls to `RemapGlobbedPath`
- caller-facing contracts such as `emitOutput` that rely on the same remapping rule
- artifact transfer and rerender flows that consume mirrored globbed path templates

This spec does not define a general-purpose glob templating system. It defines the current remapping contract only.

## Core Remapping Rule

`RemapGlobbedPath` accepts three paths:

- `local`: the local destination path or template
- `remote`: the concrete matched remote path
- `remoteBase`: the original remote path or glob template

The contract is:

1. Normalize all three paths.
2. Require `remote` to be concrete.
3. If `remoteBase` is concrete:
   - `remote` must equal `remoteBase`
   - `local` must also be concrete
   - the result is `local`
4. If `remoteBase` is globbed:
   - `local` must contain the same wildcard-containing suffix as `remoteBase`
   - that suffix starts at the first path segment containing `*`
   - `remote` must match `remoteBase`
   - the result is built by removing the concrete prefix of `remoteBase` from `remote` and attaching the remaining concrete delta to the concrete prefix of `local`

The wildcarded region is therefore a shared suffix contract. It is not a transformation template.

## Wildcard Semantics

The current design accepts `*`-based globbing in normalized paths, including both single-segment and multi-segment wildcard patterns.

- `*` matches within a path segment
- `**` matches across path segments
- `**` may match zero directories
- a wildcard-containing suffix begins at the start of the first path segment that contains `*`, not at the `*` character itself

Examples of wildcard-containing suffixes:

```text
series_id=*/bin_duration=*/counter.parquet
**/*.json
sources-capture-periodic_sampling*
```

Suffix equality is textual after normalization. Structural similarity is not enough.

## Validation Rules

The current contract rejects remaps in these cases:

- any path contains unsupported metacharacters
- any path is a directory path
- `remote` is not concrete
- `remoteBase` is concrete but does not equal `remote`
- `remoteBase` is concrete but `local` is globbed
- `remoteBase` is globbed but `local` does not contain the same wildcard-containing suffix
- `remote` does not match `remoteBase`
- the remapped local path would introduce `..` traversal
- a relative local template would remap outside its base or become absolute

Validation failures are contract failures. Callers should treat them as invalid remap inputs, not as alternative remap shapes to recover from.

## Caller Expectations

Callers that expose globbed output declarations should treat the destination path as a mirrored suffix of the source template.

In practice this means:

- only the concrete prefix before the first wildcard-containing segment may differ between `local` and `remoteBase`
- wildcarded path segments may be preserved, but not renamed
- wildcard layout may be preserved, but not reshaped
- callers should supply concrete `remote` matches and template-shaped `local` and `remoteBase` inputs

This keeps the remapping rule simple for both tool authors and engine code:

- source template describes what was produced remotely
- destination template describes where the same matched structure should live locally

## Examples

Concrete path remap:

```text
local      = tool/output/result.csv
remote     = /tmp/out/result.csv
remoteBase = /tmp/out/result.csv
result     = tool/output/result.csv
```

Mirrored single-star suffix:

```text
local      = output/sources-capture-periodic_sampling*
remote     = /tmp/out/sources-capture-periodic_sampling-libc.so.6.csv
remoteBase = /tmp/out/sources-capture-periodic_sampling*
result     = output/sources-capture-periodic_sampling-libc.so.6.csv
```

Mirrored doublestar suffix:

```text
local      = capture.apc/**/*
remote     = /tmp/out/db/image-metadata/bash/functions.bin
remoteBase = /tmp/out/**/*
result     = capture.apc/db/image-metadata/bash/functions.bin
```

Mirrored structured suffix:

```text
local      = output/parquet/timeline/series_id=*/bin_duration=*/counter.parquet
remote     = /tmp/out/report-new/apx/timeline/series_id=22/bin_duration=1000000000/counter.parquet
remoteBase = /tmp/out/report-new/apx/timeline/series_id=*/bin_duration=*/counter.parquet
result     = output/parquet/timeline/series_id=22/bin_duration=1000000000/counter.parquet
```

Zero-directory `**` match:

```text
local      = mirror/**/target
remote     = out/target
remoteBase = out/**/target
result     = mirror/target
```

Invalid because the wildcard-containing suffix differs:

```text
local      = output/bar-*.csv
remote     = /tmp/out/foo-a.csv
remoteBase = /tmp/out/foo-*.csv
```

Invalid because the wildcard layout is reshaped:

```text
local      = capture.apc/*
remote     = /tmp/out/db/image-metadata/bash/functions.bin
remoteBase = /tmp/out/**/*
```

## Testing Expectations

Tests for this contract should cover:

- concrete path passthrough
- mirrored `*` suffixes
- mirrored `**/*` suffixes
- structured suffixes such as `series_id=*/bin_duration=*/counter.parquet`
- zero-directory `**` matches
- path separator normalization across platforms
- rejection when wildcard-containing suffixes differ
- rejection when `remote` does not match `remoteBase`
- rejection of unsafe remaps involving `..` traversal or relative-path escape

## Success Criteria

This design is correctly implemented when:

- concrete remaps remain direct and unsurprising
- globbed remaps work only for mirrored wildcard-containing suffixes
- structured suffixes remap correctly without special-case logic
- callers can rely on `emitOutput` and `RemapGlobbedPath` following the same contract
- invalid remap shapes fail explicitly instead of being reinterpreted
