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

## Counter-series validation

Use `--stdout --validate-counter-series` with one CPU timeline Parquet file to
check its schema and row values instead of converting its rows to JSON.
Validation streams record batches and checks:

- Exactly one column with each required name: `start_timestamp`,
  `end_timestamp`, `device_no`, `thread`, and `value`.
- Integer column types, except `value`, which also accepts floating-point types.
- Non-null timestamps, device numbers, and values; finite numeric values; and
  an end timestamp greater than the start timestamp for each row.
- Non-negative device numbers within JavaScript's safe integer range.

The `thread` column's type is checked, but its row values are not validated.
Empty series are valid only when the required schema is present. Extra columns
are allowed. This checks the file contents; it does not establish hardware
support or coverage of every requested CPU. The tool integration checks native
counter identities and the returned device numbers separately.

The JSON summary contains `valid` and the sorted, distinct device numbers from
valid rows, for example:

```json
{"valid":true,"device_nos":[0,1]}
```

Invalid row values produce `"valid":false` with a successful process exit; callers
must check the flag. Missing or corrupt files and invalid schemas produce a
nonzero exit status rather than an empty successful result.
