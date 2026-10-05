<!--
# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0
-->

# APX Parquet Writer

This built-in tool bundle contains the reusable `apx_parquet_writer` Python
module and its pure-Go Parquet writer process. Python tools declare a schema
using required `ColumnType.INT32`, `ColumnType.INT64`, and `ColumnType.STRING`
columns, then write rows through the `ParquetWriter` context manager.

The Python module controls the process and its private binary protocol. Tools
should not invoke the executable directly.

For local development, build the executable next to the Python module:

```bash
go build -o parquet-writer .
```

Run the Go and Python tests from this directory with:

```bash
go test .
python -m pytest tests
```
