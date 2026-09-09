# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Shared archive and tabular-data helpers for run modifications."""

from __future__ import annotations

import csv
import hashlib
import io
import json
import math
import os
import tempfile
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _validated_members(
    archive: zipfile.ZipFile,
) -> tuple[str, list[zipfile.ZipInfo], dict[str, bytes], bytes]:
    infos = archive.infolist()
    names = [info.filename for info in infos]
    if len(names) != len(set(names)):
        raise ValueError("archive contains duplicate members")

    roots: set[str] = set()
    contents: dict[str, bytes] = {}
    for info in infos:
        if "\\" in info.filename:
            raise ValueError(f"archive contains unsafe member path: {info.filename!r}")
        path = PurePosixPath(info.filename)
        if path.is_absolute() or ".." in path.parts or not path.parts:
            raise ValueError(f"archive contains unsafe member path: {info.filename!r}")
        canonical_name = path.as_posix() + ("/" if info.is_dir() else "")
        if info.filename != canonical_name:
            raise ValueError(f"archive contains unsafe member path: {info.filename!r}")
        roots.add(path.parts[0])
        if not info.is_dir():
            contents[info.filename] = archive.read(info)
    if len(roots) != 1:
        raise ValueError("archive must contain exactly one run root")
    root = next(iter(roots))
    return root, infos, contents, archive.comment


def _json_object(data: bytes, member: str) -> dict[str, Any]:
    try:
        value = json.loads(data)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError(f"{member} is not valid JSON") from exc
    if not isinstance(value, dict):
        raise ValueError(f"{member} must contain a JSON object")
    return value


def _finite_positive(value: Any, description: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float, str)):
        raise ValueError(f"{description} must be numeric")
    try:
        result = float(value)
    except ValueError as exc:
        raise ValueError(f"{description} must be numeric") from exc
    if not math.isfinite(result) or result <= 0:
        raise ValueError(f"{description} must be finite and greater than zero")
    return result


def _read_csv(data: bytes, member: str) -> tuple[list[str], list[dict[str, str]]]:
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise ValueError(f"{member} is not UTF-8") from exc
    reader = csv.DictReader(io.StringIO(text, newline=""))
    if reader.fieldnames is None:
        raise ValueError(f"{member} has no header")
    return reader.fieldnames, list(reader)


def _write_csv(fieldnames: list[str], rows: list[dict[str, Any]]) -> bytes:
    output = io.StringIO(newline="")
    writer = csv.DictWriter(output, fieldnames=fieldnames, lineterminator="\r\n")
    writer.writeheader()
    writer.writerows(rows)
    return output.getvalue().encode("utf-8")


def _atomic_write_archive(
    destination: Path,
    infos: list[zipfile.ZipInfo],
    contents: dict[str, bytes],
    comment: bytes,
) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{destination.name}.", suffix=".tmp", dir=destination.parent
    )
    os.close(descriptor)
    temporary = Path(temporary_name)
    try:
        with zipfile.ZipFile(temporary, "w") as output:
            output.comment = comment
            for info in infos:
                if info.is_dir():
                    output.writestr(info, b"")
                else:
                    output.writestr(info, contents[info.filename])
        os.replace(temporary, destination)
    finally:
        temporary.unlink(missing_ok=True)
