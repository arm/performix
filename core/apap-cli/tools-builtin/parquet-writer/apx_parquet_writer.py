# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import os
import struct
import subprocess
from dataclasses import dataclass
from enum import Enum
from pathlib import Path
from typing import Any, Mapping, Sequence


_MAGIC = b"APXP"
_PROTOCOL_VERSION = 1
_READY = b"\x01"


class ColumnType(Enum):
    INT32 = 1
    INT64 = 2
    STRING = 3


@dataclass(frozen=True)
class Column:
    name: str
    type: ColumnType

    def __post_init__(self) -> None:
        if not isinstance(self.name, str) or not self.name:
            raise ValueError("column name must be a non-empty string")
        _encode_short_string(self.name)
        if not isinstance(self.type, ColumnType):
            raise ValueError(f"unsupported column type: {self.type!r}")


class Schema:
    def __init__(
        self,
        columns: Sequence[Column],
        metadata: Mapping[str, str] | None = None,
    ) -> None:
        self.columns = tuple(columns)
        self.metadata = dict(metadata or {})
        if not self.columns:
            raise ValueError("schema must contain at least one column")
        if len(self.columns) > 1024:
            raise ValueError("schema cannot contain more than 1024 columns")
        if any(not isinstance(column, Column) for column in self.columns):
            raise TypeError("schema columns must be Column instances")
        names = [column.name for column in self.columns]
        if len(names) != len(set(names)):
            raise ValueError("column names must be unique")
        if len(self.metadata) > 1024:
            raise ValueError("schema cannot contain more than 1024 metadata entries")
        for key, value in self.metadata.items():
            if not isinstance(key, str) or not key:
                raise ValueError("metadata keys must be non-empty strings")
            if not isinstance(value, str):
                raise TypeError("metadata values must be strings")
            _encode_short_string(key)
            _encode_short_string(value)


class ParquetWriterError(Exception):
    pass


class ParquetWriter:
    def __init__(
        self,
        path: str | os.PathLike[str],
        schema: Schema,
        batch_size: int = 1024,
    ) -> None:
        if not isinstance(schema, Schema):
            raise TypeError("schema must be a Schema")
        if (
            isinstance(batch_size, bool)
            or not isinstance(batch_size, int)
            or batch_size <= 0
        ):
            raise ValueError("batch_size must be a positive integer")
        self.path = Path(path)
        self.schema = schema
        self.batch_size = batch_size
        self._process: subprocess.Popen[bytes] | None = None

    def __enter__(self) -> ParquetWriter:
        if self._process is not None:
            raise ParquetWriterError("writer is already open")
        self.path.parent.mkdir(parents=True, exist_ok=True)
        executable = Path(__file__).resolve().with_name("parquet-writer")
        try:
            process = subprocess.Popen(
                [
                    str(executable),
                    "--output",
                    str(self.path),
                    "--batch",
                    str(self.batch_size),
                ],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
            )
        except OSError as error:
            raise ParquetWriterError(f"could not start {executable}: {error}") from error

        self._process = process
        assert process.stdin is not None
        assert process.stdout is not None
        try:
            process.stdin.write(_encode_schema(self.schema))
            process.stdin.flush()
            acknowledgement = process.stdout.read(1)
        except (BrokenPipeError, OSError) as error:
            raise self._startup_error(error) from error
        if acknowledgement != _READY:
            raise self._startup_error(None)
        return self

    def __exit__(self, _exception_type, exception, _traceback) -> bool:
        try:
            self.close()
        except ParquetWriterError as writer_error:
            if exception is not None:
                raise ParquetWriterError(
                    f"workload failed with {exception!r}; additionally, {writer_error}"
                ) from exception
            raise
        return False

    def write_row(self, *values: Any) -> None:
        process = self._process
        if process is None or process.stdin is None:
            raise ParquetWriterError("writer is not open")
        encoded = _encode_row(self.schema, values)
        try:
            process.stdin.write(encoded)
        except (BrokenPipeError, OSError) as error:
            raise self._process_error("write row", error) from error

    def close(self) -> None:
        process = self._process
        if process is None:
            return
        self._process = None
        close_error: OSError | None = None
        if process.stdin is not None:
            try:
                process.stdin.close()
            except OSError as error:
                close_error = error
        return_code = process.wait()
        diagnostics = _read_diagnostics(process)
        if process.stdout is not None:
            process.stdout.close()
        if process.stderr is not None:
            process.stderr.close()
        if close_error is not None or return_code != 0:
            details = diagnostics or str(close_error or f"exit code {return_code}")
            raise ParquetWriterError(f"parquet writer failed: {details}")

    def _startup_error(self, error: BaseException | None) -> ParquetWriterError:
        process = self._process
        self._process = None
        if process is None:
            return ParquetWriterError("parquet writer failed during startup")
        if process.stdin is not None:
            try:
                process.stdin.close()
            except OSError:
                pass
        return_code = process.wait()
        diagnostics = _read_diagnostics(process)
        details = diagnostics or str(error or f"exit code {return_code}")
        return ParquetWriterError(f"parquet writer failed during startup: {details}")

    def _process_error(self, action: str, error: BaseException) -> ParquetWriterError:
        process = self._process
        if process is None:
            return ParquetWriterError(f"could not {action}: {error}")
        return_code = process.poll()
        if return_code is None:
            return ParquetWriterError(f"could not {action}: {error}")
        diagnostics = _read_diagnostics(process)
        details = diagnostics or f"exit code {return_code}: {error}"
        return ParquetWriterError(f"could not {action}: {details}")


def _encode_schema(schema: Schema) -> bytes:
    encoded = bytearray(_MAGIC)
    encoded.extend(struct.pack("<H", _PROTOCOL_VERSION))
    metadata = sorted(schema.metadata.items())
    encoded.extend(struct.pack("<H", len(metadata)))
    for key, value in metadata:
        encoded.extend(_encode_short_string(key))
        encoded.extend(_encode_short_string(value))
    encoded.extend(struct.pack("<H", len(schema.columns)))
    for column in schema.columns:
        encoded.extend(_encode_short_string(column.name))
        encoded.extend(struct.pack("<B", column.type.value))
    return bytes(encoded)


def _encode_row(schema: Schema, values: Sequence[Any]) -> bytes:
    if len(values) != len(schema.columns):
        raise ValueError(f"expected {len(schema.columns)} values, got {len(values)}")
    encoded = bytearray()
    for column, value in zip(schema.columns, values):
        if column.type == ColumnType.STRING:
            data = value.encode("utf-8")
            encoded.extend(struct.pack("<I", len(data)))
            encoded.extend(data)
        elif column.type == ColumnType.INT32:
            encoded.extend(struct.pack("<i", value))
        else:
            encoded.extend(struct.pack("<q", value))
    return bytes(encoded)


def _encode_short_string(value: str) -> bytes:
    data = value.encode("utf-8")
    return struct.pack("<H", len(data)) + data


def _read_diagnostics(process: subprocess.Popen[bytes]) -> str:
    if process.stderr is None:
        return ""
    return process.stderr.read().decode("utf-8", errors="replace").strip()
