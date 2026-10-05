# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os
from unittest.mock import MagicMock, patch

import pyarrow as pa
import pyarrow.parquet as pq
import pytest

from apx_parquet_writer import ColumnType
from pytorch_collect.writer import ApiCall, OperatorCall, ParquetWriter, PrintWriter


class PyArrowParquetWriter:
    """Test double for the subprocess-backed ParquetWriter."""

    def __init__(self, path, schema, _batch_size):
        self.path = path
        self.schema = schema
        self.rows = []

    def __enter__(self):
        return self

    def __exit__(self, *_exception):
        fields = []
        for column in self.schema.columns:
            if column.type == ColumnType.INT32:
                data_type = pa.int32()
            elif column.type == ColumnType.INT64:
                data_type = pa.int64()
            else:
                data_type = pa.string()
            fields.append(pa.field(column.name, data_type, nullable=False))

        schema = pa.schema(fields, metadata=self.schema.metadata)
        table = pa.Table.from_pylist(self.rows, schema=schema)
        pq.write_table(table, self.path)
        return False

    def write_row(self, *values):
        self.rows.append({
            column.name: value
            for column, value in zip(self.schema.columns, values)
        })


@pytest.fixture
def pyarrow_parquet_writer(monkeypatch):
    monkeypatch.setattr(
        "apx_parquet_writer.ParquetWriter",
        PyArrowParquetWriter,
    )


def test_print_writer_closes_operator_timestamp_parenthesis(capsys):
    api_call = ApiCall(
        id=42,
        name="torch.add",
        operator_calls=[OperatorCall(
            id=73,
            name="aten.add.Tensor",
            timestamp_begin=123,
            timestamp_end=456,
        )],
    )

    PrintWriter().write_api_call(api_call)

    assert "[operator] aten.add.Tensor @(123, 456)" in capsys.readouterr().out


def test_parquet_schema_version(tmp_path, pyarrow_parquet_writer):
    """
    Assert on the current version of the parquet schema (ensure it matches the one in the README!)
    """
    with ParquetWriter(tmp_path, 1024):
        pass

    api_calls = pq.read_metadata(tmp_path / "api_calls.parquet").metadata
    operator_calls = pq.read_metadata(tmp_path / "operator_calls.parquet").metadata

    assert api_calls[b"schema_version"] == b"1"
    assert operator_calls[b"schema_version"] == b"1"


def test_parquet_files_are_always_created(tmp_path, pyarrow_parquet_writer):
    with ParquetWriter(tmp_path, 1024):
        pass

    assert os.path.exists(tmp_path / "api_calls.parquet")
    assert os.path.exists(tmp_path / "operator_calls.parquet")


def test_parquet_rows_are_writen(tmp_path, pyarrow_parquet_writer):
    with ParquetWriter(tmp_path, 1024) as writer:
        api_call = ApiCall(
            id=42,
            name="torch.add",
            timestamp_begin=1000,
            timestamp_end=1500,
            operator_calls=[
                OperatorCall(
                    id=73,
                    name="aten.add.Tensor",
                    timestamp_begin=1050,
                    timestamp_end=1450,
                    args=[
                        {"type": "Tensor", "dtype": "float64",
                            "shape": [4, 4]},
                        {"type": "Tensor", "dtype": "float64",
                            "shape": [4, 4]},
                    ],
                    kwargs={"beta": 2},
                    output={"type": "Tensor",
                            "dtype": "float64", "shape": [4, 4]},
                ),
            ],
            args=[
                {"type": "Tensor", "dtype": "float32", "shape": [2, 2]},
                {"type": "Tensor", "dtype": "float32", "shape": [2, 2]},
            ],
            kwargs={"alpha": 1},
            output={"type": "Tensor", "dtype": "float32", "shape": [2, 2]},
        )

        writer.write_api_call(api_call)

    api_calls = pq.read_table(tmp_path / "api_calls.parquet").to_pylist()
    operator_calls = pq.read_table(tmp_path / "operator_calls.parquet").to_pylist()

    assert api_calls == [
        {
            "api_call_id": 42,
            "function_name": "torch.add",
            "ts_begin_ns": 1000,
            "ts_end_ns": 1500,
            "operator_call_count": 1,
            "args_json": '[{"dtype": "float32", "shape": [2, 2], "type": "Tensor"}, {"dtype": "float32", "shape": [2, 2], "type": "Tensor"}]',
            "kwargs_json": '{"alpha": 1}',
            "output_json": '{"dtype": "float32", "shape": [2, 2], "type": "Tensor"}',
        }
    ]

    assert operator_calls == [
        {
            "api_call_id": 42,
            "operator_call_id": 73,
            "operator_name": "aten.add.Tensor",
            "ts_begin_ns": 1050,
            "ts_end_ns": 1450,
            "args_json": '[{"dtype": "float64", "shape": [4, 4], "type": "Tensor"}, {"dtype": "float64", "shape": [4, 4], "type": "Tensor"}]',
            "kwargs_json": '{"beta": 2}',
            "output_json": '{"dtype": "float64", "shape": [4, 4], "type": "Tensor"}',
        }
    ]


def test_parquet_columns_are_required(tmp_path, pyarrow_parquet_writer):
    with ParquetWriter(tmp_path, 1024):
        pass

    for name in ["api_calls.parquet", "operator_calls.parquet"]:
        schema = pq.read_schema(tmp_path / name)
        assert all(not field.nullable for field in schema)


def test_first_writer_is_closed_if_second_writer_cannot_start(tmp_path):
    api_calls_writer = MagicMock()
    api_calls_writer.__enter__.return_value = api_calls_writer
    operator_calls_writer = MagicMock()
    operator_calls_writer.__enter__.side_effect = RuntimeError("cannot start")

    with patch(
        "apx_parquet_writer.ParquetWriter",
        side_effect=[api_calls_writer, operator_calls_writer],
    ):
        with pytest.raises(RuntimeError, match="cannot start"):
            ParquetWriter(tmp_path, 1024).__enter__()

    api_calls_writer.__exit__.assert_called_once()
