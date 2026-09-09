# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for deterministic ASCT core-to-core latency modification."""

from __future__ import annotations

import csv
import hashlib
import io
import json
import zipfile
from pathlib import Path

import pytest

from modifications import modify_asct_core_to_core_latency as modifier


ROOT = "abcdef123456"
OUTPUT_ROOT = f"{ROOT}/tool/asct/0/output"


def csv_bytes(rows: list[list[object]]) -> bytes:
    output = io.StringIO(newline="")
    csv.writer(output, lineterminator="\r\n").writerows(rows)
    return output.getvalue().encode()


def write_asct_archive(
    path: Path,
    c2c_rows: list[list[object]] | None = None,
    *,
    excluded_members: set[str] | None = None,
    metadata: dict[str, object] | None = None,
) -> None:
    rows = c2c_rows or [
        ["", "CPUA", "CPUB", "LATENCY", "CPUA_NODE", "MEMBIND_NODE"],
        [0, 95, 94, 25.81, 0, 0],
        [1, 94, 95, 25.90, 0, 0],
    ]
    members = {
        f"{OUTPUT_ROOT}/c2c-latency.csv": csv_bytes(rows),
        f"{ROOT}/metadata.json": json.dumps(
            metadata
            or {"run.recipe_name": "asct", "run.result": "success"},
            separators=(",", ":"),
        ).encode(),
        f"{ROOT}/unchanged.bin": b"unchanged",
    }
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        archive.comment = b"fixture comment"
        archive.writestr(f"{ROOT}/", b"")
        for name, data in members.items():
            if not excluded_members or name not in excluded_members:
                archive.writestr(name, data)


def read_c2c_rows(path: Path) -> list[dict[str, str]]:
    with zipfile.ZipFile(path) as archive:
        return list(
            csv.DictReader(
                io.StringIO(
                    archive.read(f"{OUTPUT_ROOT}/c2c-latency.csv").decode()
                )
            )
        )


def test_creates_deterministic_six_to_one_directional_asymmetry(tmp_path: Path) -> None:
    source = tmp_path / "source.zip"
    output = tmp_path / "latest.zip"
    second = tmp_path / "second" / "latest.zip"
    write_asct_archive(source)
    source_hash = hashlib.sha256(source.read_bytes()).hexdigest()

    modifier.create_core_to_core_latency_asymmetry_run(source, output)
    modifier.create_core_to_core_latency_asymmetry_run(source, second)

    assert hashlib.sha256(source.read_bytes()).hexdigest() == source_hash
    assert output.read_bytes() == second.read_bytes()
    with zipfile.ZipFile(output) as archive:
        assert archive.comment == b"fixture comment"
        assert archive.read(f"{ROOT}/unchanged.bin") == b"unchanged"
    rows = {
        (row["CPUA"], row["CPUB"]): float(row["LATENCY"])
        for row in read_c2c_rows(output)
    }
    assert rows[("94", "95")] == pytest.approx(25.81 * 6)
    assert rows[("95", "94")] == pytest.approx(25.81)

    report = json.loads((tmp_path / "run-modification-report.json").read_text())
    assert report["source_archive_sha256"] == source_hash
    assert report["generated_archive_sha256"] == hashlib.sha256(output.read_bytes()).hexdigest()
    assert report["transformation"] == {
        "name": "core_to_core_latency_asymmetry",
        "target_directional_ratio": 6.0,
        "version": 1,
    }
    assert report["changed_members"] == [f"{OUTPUT_ROOT}/c2c-latency.csv"]
    assert report["generated_core_pair"][0]["latency_ns"] == pytest.approx(25.81 * 6)


@pytest.mark.parametrize("ratio", [0.0, -1.0, float("nan"), float("inf"), 1.0])
def test_rejects_invalid_target_directional_ratio(
    tmp_path: Path, ratio: float
) -> None:
    source = tmp_path / "source.zip"
    output = tmp_path / "output.zip"
    write_asct_archive(source)

    with pytest.raises(
        ValueError, match="target directional ratio must be finite and greater than one"
    ):
        modifier.create_core_to_core_latency_asymmetry_run(
            source, output, target_directional_ratio=ratio
        )

    assert not output.exists()
    assert not (tmp_path / "run-modification-report.json").exists()


def test_rejects_archive_without_core_to_core_output(tmp_path: Path) -> None:
    source = tmp_path / "source.zip"
    c2c_member = f"{OUTPUT_ROOT}/c2c-latency.csv"
    write_asct_archive(source, excluded_members={c2c_member})

    with pytest.raises(ValueError, match="missing required members.*c2c-latency"):
        modifier.create_core_to_core_latency_asymmetry_run(
            source, tmp_path / "output.zip"
        )


def test_rejects_run_without_comparable_reverse_pair(tmp_path: Path) -> None:
    source = tmp_path / "source.zip"
    write_asct_archive(
        source,
        [
            ["", "CPUA", "CPUB", "LATENCY", "CPUA_NODE", "MEMBIND_NODE"],
            [0, 94, 95, 25.9, 0, 0],
            [1, 95, 94, 25.8, 0, 1],
        ],
    )

    with pytest.raises(ValueError, match="no reverse core pair"):
        modifier.create_core_to_core_latency_asymmetry_run(
            source, tmp_path / "output.zip"
        )


def test_rejects_already_asymmetric_pair(tmp_path: Path) -> None:
    source = tmp_path / "source.zip"
    write_asct_archive(
        source,
        [
            ["", "CPUA", "CPUB", "LATENCY", "CPUA_NODE", "MEMBIND_NODE"],
            [0, 95, 94, 20.0, 0, 0],
            [1, 94, 95, 40.0, 0, 0],
        ],
    )

    with pytest.raises(ValueError, match="original directional ratio"):
        modifier.create_core_to_core_latency_asymmetry_run(
            source, tmp_path / "output.zip"
        )


def test_skips_asymmetric_pair_when_a_later_pair_is_suitable(tmp_path: Path) -> None:
    source = tmp_path / "source.zip"
    output = tmp_path / "output.zip"
    write_asct_archive(
        source,
        [
            ["", "CPUA", "CPUB", "LATENCY", "CPUA_NODE", "MEMBIND_NODE"],
            [0, 0, 1, 40.0, 0, 0],
            [1, 1, 0, 20.0, 0, 0],
            [2, 2, 3, 26.0, 0, 0],
            [3, 3, 2, 25.0, 0, 0],
        ],
    )

    modifier.create_core_to_core_latency_asymmetry_run(
        source, output, target_directional_ratio=4.0
    )

    rows = {
        (row["CPUA"], row["CPUB"]): float(row["LATENCY"])
        for row in read_c2c_rows(output)
    }
    assert rows[("0", "1")] == 40.0
    assert rows[("1", "0")] == 20.0
    assert rows[("2", "3")] == 25.0 * 4
    assert rows[("3", "2")] == 25.0


@pytest.mark.parametrize(
    ("metadata", "error"),
    [
        ({"run.result": "success"}, "not an ASCT run"),
        ({"run.recipe_name": "asct", "run.result": "failed"}, "did not complete"),
    ],
)
def test_rejects_invalid_run_metadata(
    tmp_path: Path, metadata: dict[str, object], error: str
) -> None:
    source = tmp_path / "source.zip"
    write_asct_archive(source, metadata=metadata)

    with pytest.raises(ValueError, match=error):
        modifier.create_core_to_core_latency_asymmetry_run(
            source, tmp_path / "output.zip"
        )
