# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import io
import re
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from apx_parquet_writer import (
    Column,
    ColumnType,
    ParquetWriter,
    ParquetWriterError,
    Schema,
)


def test_writer_matches_protocol_transcript(tmp_path):
    messages = _protocol_transcript()
    assert [direction for direction, _ in messages] == [">", "<", ">"]
    schema_message, ready_message, row_message = [data for _, data in messages]

    schema = Schema(
        [
            Column("small", ColumnType.INT32),
            Column("large", ColumnType.INT64),
            Column("name", ColumnType.STRING),
        ],
        metadata={"z": "last", "schema_version": "1"},
    )
    process = MagicMock()
    sent = bytearray()
    process.stdin.write.side_effect = lambda data: sent.extend(data)

    def acknowledge_schema(size):
        assert size == 1
        assert bytes(sent) == schema_message
        return ready_message

    process.stdout.read.side_effect = acknowledge_schema
    process.stderr = io.BytesIO()
    process.wait.return_value = 0
    writer = ParquetWriter(tmp_path / "output.parquet", schema)

    with patch("apx_parquet_writer.subprocess.Popen", return_value=process):
        with writer:
            writer.write_row(-12, 9_000_000_000, "Caf\u00e9 Les Deux Magots")

    assert bytes(sent) == schema_message + row_message


def _decode_transcript_message(text):
    result = bytearray()
    position = 0
    for escape in re.finditer(r"0x[0-9a-f]{2}", text):
        literal = text[position : escape.start()]
        if "0x" in literal:
            raise ValueError(f"invalid byte escape in {text!r}")
        result.extend(literal.encode("utf-8"))
        result.append(int(escape.group()[2:], 16))
        position = escape.end()
    literal = text[position:]
    if "0x" in literal:
        raise ValueError(f"invalid byte escape in {text!r}")
    result.extend(literal.encode("utf-8"))
    return bytes(result)


def _protocol_transcript():
    path = Path(__file__).parents[1] / "testdata" / "protocol_v1.txt"
    messages = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line or line.startswith("#"):
            continue
        if not line.startswith(("> ", "< ")):
            raise ValueError(f"invalid transcript line: {line!r}")
        messages.append((line[0], _decode_transcript_message(line[2:])))
    return messages


def test_schema_rejects_duplicate_columns():
    with pytest.raises(ValueError, match="unique"):
        Schema(
            [
                Column("value", ColumnType.INT32),
                Column("value", ColumnType.INT64),
            ]
        )


def test_writer_reports_startup_failure(tmp_path):
    writer = ParquetWriter(
        tmp_path / "output.parquet",
        Schema([Column("value", ColumnType.INT64)]),
    )

    with patch(
        "apx_parquet_writer.subprocess.Popen",
        side_effect=OSError("could not execute"),
    ):
        with pytest.raises(ParquetWriterError, match="could not start"):
            writer.__enter__()


def test_writer_combines_workload_and_shutdown_failures(tmp_path):
    process = MagicMock()
    process.stdin = io.BytesIO()
    process.stdout = io.BytesIO(b"\x01")
    process.stderr = io.BytesIO(b"footer failed\n")
    process.wait.return_value = 7
    writer = ParquetWriter(
        tmp_path / "output.parquet",
        Schema([Column("value", ColumnType.INT64)]),
    )

    with patch("apx_parquet_writer.subprocess.Popen", return_value=process):
        with pytest.raises(
            ParquetWriterError,
            match="workload failed.*footer failed",
        ) as caught:
            with writer:
                raise ValueError("model failed")

    assert isinstance(caught.value.__cause__, ValueError)

