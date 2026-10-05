# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import json
import sys
from contextlib import ExitStack
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

try:
    from typing import override
except ImportError:
    def override(method):
        return method


@dataclass
class ApiCall:
    id: int
    name: str
    timestamp_begin: int = 0
    timestamp_end: int = 0
    operator_calls: list[OperatorCall] = field(default_factory=list)
    args: list[Any] = field(default_factory=list)
    kwargs: dict[str, Any] = field(default_factory=dict)
    output: dict[str, Any] = field(default_factory=dict)


@dataclass
class OperatorCall:
    id: int
    name: str
    timestamp_begin: int = 0
    timestamp_end: int = 0
    args: list[Any] = field(default_factory=list)
    kwargs: dict[str, Any] = field(default_factory=dict)
    output: dict[str, Any] = field(default_factory=dict)


class Writer:
    def __init__(self):
        pass

    def __enter__(self):
        return self

    def __exit__(self, *exception):
        pass

    def write_api_call(self, api_call: ApiCall):
        pass


class ParquetWriter(Writer):
    """
    Output traced PyTorch API and operator calls to Parquet files.
    """

    schema_version = "1"  # change this value whenever the schema version changes

    @override
    def __init__(self, output: str, flush_limit: int):
        self.output = Path(output)
        self.flush_limit = flush_limit
        self._stack = ExitStack()

    @override
    def __enter__(self):
        from apx_parquet_writer import ParquetWriter as StreamParquetWriter
        api_calls_path, operator_calls_path = self._output_paths()
        api_calls_path.parent.mkdir(parents=True, exist_ok=True)
        operator_calls_path.parent.mkdir(parents=True, exist_ok=True)

        try:
            self.api_calls_writer = self._stack.enter_context(StreamParquetWriter(
                api_calls_path,
                self._api_calls_schema(),
                self.flush_limit,
            ))
            self.operator_calls_writer = self._stack.enter_context(StreamParquetWriter(
                operator_calls_path,
                self._operator_calls_schema(),
                self.flush_limit,
            ))
        except BaseException:
            self._stack.__exit__(*sys.exc_info())
            raise

        return self

    @override
    def __exit__(self, *exception):
        return self._stack.__exit__(*exception)

    @override
    def write_api_call(self, api_call: ApiCall):
        self.api_calls_writer.write_row(
            api_call.id,
            api_call.name,
            api_call.timestamp_begin,
            api_call.timestamp_end,
            len(api_call.operator_calls),
            self._to_json(api_call.args),
            self._to_json(api_call.kwargs),
            self._to_json(api_call.output),
        )

        for operator_call in api_call.operator_calls:
            self.operator_calls_writer.write_row(
                api_call.id,
                operator_call.id,
                operator_call.name,
                operator_call.timestamp_begin,
                operator_call.timestamp_end,
                self._to_json(operator_call.args),
                self._to_json(operator_call.kwargs),
                self._to_json(operator_call.output),
            )

    def _output_paths(self):
        if self.output.suffix == ".parquet":
            return (
                self.output.with_name(
                    f"{self.output.stem}.api_calls.parquet"),
                self.output,
            )

        return (
            self.output / "api_calls.parquet",
            self.output / "operator_calls.parquet",
        )

    @staticmethod
    def _api_calls_schema():
        from apx_parquet_writer import Column, ColumnType, Schema
        return Schema([
            Column("api_call_id", ColumnType.INT64),
            Column("function_name", ColumnType.STRING),
            Column("ts_begin_ns", ColumnType.INT64),
            Column("ts_end_ns", ColumnType.INT64),
            Column("operator_call_count", ColumnType.INT32),
            Column("args_json", ColumnType.STRING),
            Column("kwargs_json", ColumnType.STRING),
            Column("output_json", ColumnType.STRING),
        ], metadata={"schema_version": ParquetWriter.schema_version})

    @staticmethod
    def _operator_calls_schema():
        from apx_parquet_writer import Column, ColumnType, Schema
        return Schema([
            Column("api_call_id", ColumnType.INT64),
            Column("operator_call_id", ColumnType.INT64),
            Column("operator_name", ColumnType.STRING),
            Column("ts_begin_ns", ColumnType.INT64),
            Column("ts_end_ns", ColumnType.INT64),
            Column("args_json", ColumnType.STRING),
            Column("kwargs_json", ColumnType.STRING),
            Column("output_json", ColumnType.STRING),
        ], metadata={"schema_version": ParquetWriter.schema_version})

    @staticmethod
    def _to_json(value: Any) -> str:
        return json.dumps(value, default=str, sort_keys=True)


class PrintWriter(Writer):
    """
    Output traced PyTorch API and operator calls to stdout using print.
    """

    @override
    def write_api_call(self, api_call: ApiCall):
        build = []
        build.append(
            f"[api] {api_call.name} @({api_call.timestamp_begin}, {api_call.timestamp_end})")
        build.append(f"     args:")

        for arg in api_call.args:
            build.append(f"         - {arg}")

        build.append(f"     kwargs: {api_call.kwargs}")
        build.append(f"     output: {api_call.output}")

        for operator_call in api_call.operator_calls:
            build.append(
                f"     [operator] {operator_call.name} @({operator_call.timestamp_begin}, {operator_call.timestamp_end})")
            build.append(f"                args:")

            for arg in operator_call.args:
                build.append(f"                    - {arg}")

            build.append(f"                kwargs: {operator_call.kwargs}")
            build.append(f"                output: {operator_call.output}")

        print("\n".join(build))
