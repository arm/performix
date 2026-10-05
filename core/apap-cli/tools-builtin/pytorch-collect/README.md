<!--
# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0
-->

# PyTorch Collect

PyTorch Collect traces a Python workload that uses PyTorch and records:

- calls to the high-level PyTorch API.
- calls to low-level PyTorch operators.
- start and end timestamps for each API call and operator call (using CLOCK_MONOTONIC_RAW).
- compact JSON summaries of call arguments, keyword arguments, and outputs.

## Install

Install the package locally from this directory:

```bash
pip install "."
pip install ".[samples]" # optional to run local sample workloads
```

The Parquet backend also requires the sibling `apx_parquet_writer` module and
its Go executable. Build the executable next to the module for local development:

```bash
pip install -e ../parquet-writer
(cd ../parquet-writer && go build -o parquet-writer .)
```

## Usage

Run a Python module file under the collector, including any arguments used by the module:

```bash
pytorch-collect --output ./trace-output --writer parquet -- samples/gpt2.py "What is Arm Performix?" 100
```

This generates two parquet files under the trace-output directory:

- `api_calls.parquet`
- `operator_calls.parquet`

## Parquet Schema

Current schema version: `1`

### `api_calls.parquet`

| Column | Type | Description |
| --- | --- | --- |
| `api_call_id` | `int64` | API call ID. |
| `function_name` | `string` | PyTorch API function name. |
| `ts_begin_ns` | `int64` | API call start time (CLOCK_MONOTONIC_RAW). |
| `ts_end_ns` | `int64` | API call end time (CLOCK_MONOTONIC_RAW). |
| `operator_call_count` | `int32` | Number of operator calls recorded for the API call. |
| `args_json` | `string` | JSON summary of positional arguments. |
| `kwargs_json` | `string` | JSON summary of keyword arguments. |
| `output_json` | `string` | JSON summary of the API call output. |

### `operator_calls.parquet`

| Column | Type | Description |
| --- | --- | --- |
| `api_call_id` | `int64` | API call ID (can be joined with the column of the same name in `api_calls.parquet`). |
| `operator_call_id` | `int64` | Operator call ID. |
| `operator_name` | `string` | PyTorch operator name. |
| `ts_begin_ns` | `int64` | Operator call start time (CLOCK_MONOTONIC_RAW). |
| `ts_end_ns` | `int64` | Operator call end time (CLOCK_MONOTONIC_RAW). |
| `args_json` | `string` | JSON summary of positional arguments. |
| `kwargs_json` | `string` | JSON summary of keyword arguments. |
| `output_json` | `string` | JSON summary of the operator call output. |

## Testing

In order to run the unit tests, run the following:

```bash
# install dependencies; the Parquet test double and assertions use PyArrow
pip install -e ../parquet-writer
pip install ".[test]"

# run tests
pytest tests/*
```
