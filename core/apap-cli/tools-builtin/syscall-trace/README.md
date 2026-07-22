<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# syscall-trace

`syscall-trace` normalizes raw `strace` output into the Performix syscall event
Parquet schema. The JavaScript tool integration invokes `strace`; this built-in
target tool parses the raw trace file or `strace -ff` output prefix and writes
structured Parquet.

## Usage

```sh
syscall-trace --raw-output syscall_trace.log --output syscalls.parquet
```

Arguments:

- `--raw-output`: path to a raw `strace -ttt -T` output file, or the output
  prefix passed to `strace -ff -o`.
- `--output`: path for the generated Parquet file.

## Output Schema

The generated Parquet file has this Arrow schema:

```text
ts_utc: timestamp[us, tz=UTC] not null
pid: int64 not null
syscall: utf8 not null
args: utf8 not null
duration_us: int64 nullable
result: utf8 not null
errno: utf8 nullable
```

Columns:

- `ts_utc`: UTC timestamp derived from the `strace -ttt` timestamp, preserving
  microsecond precision.
- `pid`: traced process ID from `strace -f` output or the `strace -ff` output
  filename suffix.
- `syscall`: syscall name.
- `args`: raw syscall argument text from inside the syscall parentheses.
- `duration_us`: syscall duration in microseconds when present.
- `result`: raw syscall result text.
- `errno`: errno name when the result starts with an errno return.

The Parquet writer stores the Arrow schema in file metadata, uses Zstd
compression, and otherwise relies on the Arrow/Parquet writer's default
encoding choices.

## Parser Scope

The parser accepts normal syscall lines from `strace -f -ttt -T` and per-process
files from `strace -ff -ttt -T -o <prefix>`. When parsing `-ff` output, it
discovers files named `<prefix>.<pid>` and uses the suffix as the row PID when
the line itself omits a PID. It skips non-syscall status lines such as signals,
process attachment messages, and process exit markers.

Unfinished/resumed syscall pairs are reconstructed into a single syscall event
when both halves are present. The event timestamp comes from the original
unfinished line, while the result and duration come from the resumed line.
Incomplete pairs are reported to stderr and omitted. The raw `strace` logs
remain in the target temp directory while the parser runs; use the engine/CLI
temp-directory retention option if you need them for debugging.

## Source and Bundle Versioning

The maintained Go source lives in the stable `source/` directory. The deployable
tool bundle version follows the Performix engine version. The JavaScript tool
integration keeps its own recipe-facing version, currently `1.0.0`, and uses
`performix.engineVersion` only for its deployed `tool_bundle` dependency.
`scripts/get-tools.py` packages this source under the matching runtime bundle path, for example
`syscall-trace/<engine-version>/syscall-trace-Linux-aarch64.tar.gz`.

In CI or release packaging, `scripts/get-tools.py` reads
`PERFORMIX_ENGINE_VERSION` when it is set; otherwise it derives the version via
`scripts/get_atperf_version.py`. Snapshot builds append the `-dev` suffix when
`PERFORMIX_SNAPSHOT_BUILD` is set, with `SNAPSHOT_ARG` retained as a fallback for
older callers.

This bundle version is separate from the emitted data schema version. The
component version recorded for `syscall-trace-parquet` is the CDF
`SchemaVersion`, not the parser binary version. It is currently `1.0`, and
should only be bumped if the stable Parquet schema or parser contract changes
incompatibly. Parser implementation changes that keep the same output schema
should continue to use the current engine-versioned bundle path without moving
or duplicating the `source/` directory.
