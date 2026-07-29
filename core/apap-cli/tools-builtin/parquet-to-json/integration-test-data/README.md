<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Integration test data

This directory contains representative Parquet files used by the
`parquet-to-json` integration tests. These files came directly from
a real `sl-record` invocation which produced counter metadata files.

## Files

Each test fixture consists of a matching pair:

- `<name>.parquet` — input passed to `parquet-to-json`.
- `<name>.expected.json` — expected JSON output.

The integration test passes the Parquet files to the command and writes the
generated JSON files to a temporary output directory. Each generated file is
then compared semantically with its corresponding `.expected.json` file.

JSON formatting and whitespace do not need to match.

## Running the tests

From this tool's root directory:

```shell
go test -run 'TestParquetToJSONIntegration'
```

Alternatively, run all APX unit tests from the repository root:

```shell
task core:test:unit:apx
```
