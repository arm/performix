<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# parquet-to-json

`parquet-to-json` converts one or more Parquet data files into JSON. The conversion is
handled by Apache Arrow.

## Buildling

Build the tool using `go build` from this tool's root directory:
```shell
go build -o parquet-to-json
```

## Usage

```text
parquet-to-json <input_file_path...> [flags]
```

By default, the outputs are written next to the input files with their final file
extensions replaced by `.json`:

```shell
parquet-to-json results/data.parquet other/output.final.parquet
# Writes results/data.json and other/output.final.json
```

Use `--output-dir` (`-o`) to write the JSON files to a specific directory:

```shell
parquet-to-json results/data.parquet --output-dir converted
# Writes converted/data.json
```

If this directory already exists, it must be writable; if not, it must be creatable (i.e. the nearest parent must be writable).

Use `--stdout` (`-s`) to write the converted JSON to standard output instead
of creating a file:

```shell
parquet-to-json results/data.parquet --stdout
```

`--output-dir` and `--stdout` are mutually exclusive.

## Output

The tool writes a compact JSON array containing one object for each row in the
input file. Parquet column names become the keys in each object. For example:

```json
[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]
```
Conversion is written to a temporary file and renamed to the final path on success,
so failures do not leave partial output files. The tool makes a best-effort attempt
to reject writing to output files which already exist, but this is not strictly
guaranteed if the output file is created by another process during the rename.

If `--stdout` is used, output is streamed to the standard output and may be
partial if conversion fails.

## Testing

Run all `apx` unit tests from the repository root:

```shell
task core:test:unit:apx
```

Run only the unit tests for this tool from this tool's root directory:

```shell
go test
```
