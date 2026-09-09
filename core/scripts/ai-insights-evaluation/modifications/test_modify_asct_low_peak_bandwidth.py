# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for deterministic ASCT low-peak-bandwidth modification."""

from __future__ import annotations

import csv
import hashlib
import io
import json
import zipfile
from pathlib import Path

import pytest

from modifications import modify_asct_low_peak_bandwidth as modifier


ROOT = "abcdef123456"
OUTPUT_ROOT = f"{ROOT}/tool/asct/0/output"
ASCT_JSON = b'{"source_measurements":"unchanged"}'


def csv_bytes(rows: list[list[object]]) -> bytes:
    output = io.StringIO(newline="")
    csv.writer(output, lineterminator="\r\n").writerows(rows)
    return output.getvalue().encode()


def write_asct_archive(
    path: Path,
    member_overrides: dict[str, bytes] | None = None,
    excluded_members: set[str] | None = None,
    extra_members: list[tuple[str, bytes]] | None = None,
) -> None:
    peak_rows = [
        ["", "Traffic type", "Peak BW [GB/s]", "% of Peak Theoretical"],
        [0, "All Reads", 160.0, 80.0],
        [1, "1:1 Reads-Writes", 120.0, 60.0],
    ]
    cross_rows = [["", "Node 0"], ["Node 0", 160.0]]
    metadata = {
        "run.recipe_name": "asct",
        "run.result": "success",
        "run.parameters": {
            "system_info_only": False,
            "default_benchmarks": False,
            "benchmark_peak_bandwidth": False,
        },
        "run.size_bytes": 1,
    }
    members = {
        f"{OUTPUT_ROOT}/peak-bandwidth.csv": csv_bytes(peak_rows),
        f"{OUTPUT_ROOT}/cross-numa-bandwidth.csv": csv_bytes(cross_rows),
        f"{OUTPUT_ROOT}/asct.json": ASCT_JSON,
        f"{OUTPUT_ROOT}/system-info.csv": b"report.asct_ver,0.6.0\r\nsys_hw.cpu_type,test\r\n",
        f"{ROOT}/metadata.json": json.dumps(metadata, separators=(",", ":")).encode(),
        f"{ROOT}/unchanged.bin": b"unchanged",
    }
    members.update(member_overrides or {})
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        archive.comment = b"fixture comment"
        archive.writestr(f"{ROOT}/", b"")
        for name, data in members.items():
            if not excluded_members or name not in excluded_members:
                archive.writestr(name, data)
        for name, data in extra_members or []:
            archive.writestr(name, data)


def read_member(path: Path, name: str) -> bytes:
    with zipfile.ZipFile(path) as archive:
        return archive.read(name)


def generate(tmp_path: Path) -> tuple[Path, Path, Path]:
    source = tmp_path / "source.zip"
    output = tmp_path / "latest.zip"
    report = tmp_path / "run-modification-report.json"
    write_asct_archive(source)
    modifier.create_low_peak_bandwidth_run(source, output)
    return source, output, report


def test_implicit_default_low_peak_bandwidth_scales_csvs_and_is_deterministic(
    tmp_path: Path,
) -> None:
    source, output, report = generate(tmp_path)
    source_hash = hashlib.sha256(source.read_bytes()).hexdigest()
    second = tmp_path / "second" / "latest.zip"
    modifier.create_low_peak_bandwidth_run(source, second)

    assert hashlib.sha256(source.read_bytes()).hexdigest() == source_hash
    assert output.read_bytes() == second.read_bytes()
    assert read_member(output, f"{ROOT}/unchanged.bin") == b"unchanged"
    assert read_member(output, f"{OUTPUT_ROOT}/asct.json") == ASCT_JSON
    assert read_member(output, f"{ROOT}/metadata.json") == read_member(
        source, f"{ROOT}/metadata.json"
    )
    with zipfile.ZipFile(output) as archive:
        assert archive.comment == b"fixture comment"
        peak = list(
            csv.DictReader(
                io.StringIO(
                    archive.read(f"{OUTPUT_ROOT}/peak-bandwidth.csv").decode()
                )
            )
        )
        cross = list(
            csv.DictReader(
                io.StringIO(
                    archive.read(f"{OUTPUT_ROOT}/cross-numa-bandwidth.csv").decode()
                )
            )
        )
    assert [float(row["% of Peak Theoretical"]) for row in peak] == [50.0, 37.5]
    assert [float(row["Peak BW [GB/s]"]) for row in peak] == [100.0, 75.0]
    assert float(cross[0]["Node 0"]) == 100.0
    audit = json.loads(report.read_text())
    assert audit["source_archive_sha256"] == source_hash
    assert audit["generated_archive_sha256"] == hashlib.sha256(output.read_bytes()).hexdigest()
    assert audit["transformation"]["scale_factor"] == 0.625
    assert audit["changed_members"] == [
        f"{OUTPUT_ROOT}/cross-numa-bandwidth.csv",
        f"{OUTPUT_ROOT}/peak-bandwidth.csv",
    ]


def test_rejects_archive_without_peak_bandwidth_output(tmp_path: Path) -> None:
    source = tmp_path / "source.zip"
    peak_member = f"{OUTPUT_ROOT}/peak-bandwidth.csv"
    write_asct_archive(source, excluded_members={peak_member})

    with pytest.raises(ValueError, match="missing required members.*peak-bandwidth"):
        modifier.create_low_peak_bandwidth_run(
            source,
            tmp_path / "output.zip",
        )


@pytest.mark.parametrize(
    ("overrides", "extra_members", "error"),
    [
        ({f"{ROOT}/metadata.json": b"{}"}, None, "not an ASCT run"),
        ({f"{OUTPUT_ROOT}/peak-bandwidth.csv": b"wrong,header\n1,2\n"}, None, "schema"),
        (None, [("../escape", b"x")], "unsafe member"),
        (None, [(f"{ROOT}/./alias.bin", b"x")], "unsafe member"),
        (None, [(f"{ROOT}//alias.bin", b"x")], "unsafe member"),
        (None, [(f"{ROOT}/unchanged.bin", b"duplicate")], "duplicate"),
    ],
)
def test_invalid_archives_are_rejected(
    tmp_path: Path,
    overrides: dict[str, bytes] | None,
    extra_members: list[tuple[str, bytes]] | None,
    error: str,
) -> None:
    source = tmp_path / "source.zip"
    write_asct_archive(
        source, member_overrides=overrides, extra_members=extra_members
    )

    with pytest.raises(ValueError, match=error):
        modifier.create_low_peak_bandwidth_run(
            source,
            tmp_path / "output.zip",
        )
