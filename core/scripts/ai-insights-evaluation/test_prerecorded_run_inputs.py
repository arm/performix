# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import json
import zipfile
from pathlib import Path

import pytest

import conftest as harness


def make_testcase(recipe: str) -> dict[str, str]:
    return {
        "id": "test_case_32",
        "recipe": recipe,
        "prerecord": {"mode": "launch"},
        "run_artifact": "test_case_32/latest.zip",
    }


def write_common_inputs(root: Path, recipe: str) -> Path:
    case_dir = root / "test_case_32"
    case_dir.mkdir(parents=True)
    archive = case_dir / "latest.zip"
    archive.write_bytes(b"run")
    (case_dir / "metadata.json").write_text(
        json.dumps({"recipe": recipe}), encoding="utf-8"
    )
    return case_dir


def write_metadata(case_dir: Path, metadata: dict[str, object]) -> None:
    (case_dir / "metadata.json").write_text(json.dumps(metadata), encoding="utf-8")


def write_source_archive(case_dir: Path) -> None:
    with zipfile.ZipFile(case_dir / "test_src.zip", "w") as archive:
        archive.writestr("test_src/main.cpp", "int main() {}")


def make_instruction_mix_testcase(mode: str) -> dict[str, str | list[str]]:
    test_case = make_testcase("instruction_mix")
    test_case["recipe_params"] = [f"mode={mode}"]
    return test_case


@pytest.mark.parametrize("recipe", ["system_utilization", "syscall_trace_summary"])
def test_recipes_without_source_data_do_not_require_source_archive(
    tmp_path: Path, recipe: str
) -> None:
    write_common_inputs(tmp_path, recipe)

    missing = harness._missing_run_artifacts_for_testcases(
        [make_testcase(recipe)], tmp_path
    )

    assert missing == []


def test_instruction_mix_requires_source_archive(
    tmp_path: Path,
) -> None:
    case_dir = write_common_inputs(tmp_path, "instruction_mix")

    missing = harness._missing_run_artifacts_for_testcases(
        [make_instruction_mix_testcase("dynamic")], tmp_path
    )

    assert missing == [
        ("test_case_32", "source archive", case_dir / "test_src.zip")
    ]


def test_static_instruction_mix_does_not_require_source_archive(tmp_path: Path) -> None:
    write_common_inputs(tmp_path, "instruction_mix")

    missing = harness._missing_run_artifacts_for_testcases(
        [make_instruction_mix_testcase("static")], tmp_path
    )

    assert missing == []


@pytest.mark.parametrize("profile_mode", ["launch", "system-wide"])
def test_code_hotspots_requires_source_archive_regardless_of_execution_mode(
    tmp_path: Path, profile_mode: str
) -> None:
    case_dir = write_common_inputs(tmp_path, "code_hotspots")
    test_case = make_testcase("code_hotspots")
    test_case["prerecord"] = {"mode": profile_mode}

    missing = harness._missing_run_artifacts_for_testcases(
        [test_case], tmp_path
    )

    assert missing == [
        ("test_case_32", "source archive", case_dir / "test_src.zip")
    ]


@pytest.mark.parametrize(
    "recipe", ["cpu_microarchitecture", "cache_sharing", "memory_access"]
)
def test_sampled_recipe_requires_source_archive(tmp_path: Path, recipe: str) -> None:
    case_dir = write_common_inputs(tmp_path, recipe)

    missing = harness._missing_run_artifacts_for_testcases(
        [make_testcase(recipe)], tmp_path
    )

    assert missing == [
        ("test_case_32", "source archive", case_dir / "test_src.zip")
    ]


@pytest.mark.parametrize("recipe", ["system_utilization", "syscall_trace_summary"])
def test_recipes_without_source_data_do_not_download_source_archive(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, recipe: str
) -> None:
    write_common_inputs(tmp_path, recipe)
    downloads = []
    monkeypatch.setattr(
        harness,
        "_download_artifactory_input",
        lambda *args, **kwargs: downloads.append((args, kwargs)),
    )

    harness._download_missing_run_artifacts(
        [make_testcase(recipe)], tmp_path, "repository/path"
    )

    assert downloads == []


def test_shared_run_artifact_downloads_from_declared_artifact_directory(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    downloads = []

    def record_download(*args, **kwargs):
        downloads.append((args, kwargs))
        return True

    monkeypatch.setattr(harness, "_download_artifactory_input", record_download)
    test_case = {
        "id": "test_case_54",
        "recipe": "asct",
        "run_artifact": "test_case_53/latest.zip",
    }

    harness._download_missing_run_artifacts(
        [test_case], tmp_path, "repository/path"
    )

    assert [call[0][1] for call in downloads] == [
        "test_case_53",
        "test_case_53",
    ]


def test_metadata_recipe_must_match_manifest(tmp_path: Path) -> None:
    write_common_inputs(tmp_path, "code_hotspots")

    with pytest.raises(pytest.UsageError, match="recipe mismatch"):
        harness._validate_prerecorded_metadata(
            [make_testcase("system_utilization")], tmp_path
        )


def test_instruction_mix_metadata_mode_must_match_manifest(tmp_path: Path) -> None:
    case_dir = write_common_inputs(tmp_path, "instruction_mix")
    write_metadata(case_dir, {"recipe": "instruction_mix", "recipe_params": ["mode=static"]})

    with pytest.raises(pytest.UsageError, match="recipe params mismatch"):
        harness._validate_prerecorded_metadata(
            [make_instruction_mix_testcase("dynamic")], tmp_path
        )


@pytest.mark.parametrize(
    "recorded_modification", [None, "core_to_core_latency_asymmetry"]
)
def test_metadata_run_modification_must_match_manifest(
    tmp_path: Path, recorded_modification: str | None
) -> None:
    case_dir = write_common_inputs(tmp_path, "asct")
    write_metadata(
        case_dir,
        {
            "recipe": "asct",
            "run_modification": recorded_modification,
            "workload_command": "/remote/test_case_32",
        },
    )
    test_case = make_testcase("asct")
    test_case["run_modification"] = "low_peak_bandwidth"

    with pytest.raises(pytest.UsageError, match="run modification mismatch"):
        harness._validate_prerecorded_metadata([test_case], tmp_path)


@pytest.mark.parametrize(
    ("profile_mode", "recorded_mode"),
    [
        ("system-wide", "launch"),
        ("launch", "system-wide"),
    ],
)
def test_metadata_profile_mode_must_match_manifest(
    tmp_path: Path,
    profile_mode: str,
    recorded_mode: str,
) -> None:
    case_dir = write_common_inputs(tmp_path, "system_utilization")
    write_metadata(
        case_dir,
        {
            "recipe": "system_utilization",
            "profile_mode": recorded_mode,
        },
    )
    test_case = make_testcase("system_utilization")
    test_case["prerecord"] = {"mode": profile_mode}

    with pytest.raises(pytest.UsageError, match="profile mode mismatch"):
        harness._validate_prerecorded_metadata([test_case], tmp_path)


def test_missing_metadata_profile_mode_defaults_to_launch(tmp_path: Path) -> None:
    write_common_inputs(tmp_path, "system_utilization")

    harness._validate_prerecorded_metadata(
        [make_testcase("system_utilization")], tmp_path
    )


def test_jfr_capture_metadata_must_match_manifest(tmp_path: Path) -> None:
    case_dir = write_common_inputs(tmp_path, "code_hotspots")
    test_case = make_testcase("code_hotspots")
    test_case["prerecord"] = {"mode": "launch", "enable_jfr_capture": True}

    with pytest.raises(pytest.UsageError, match="JFR capture mismatch"):
        harness._validate_prerecorded_metadata([test_case], tmp_path)

    write_metadata(case_dir, {"recipe": "code_hotspots", "enable_jfr_capture": True})
    harness._validate_prerecorded_metadata([test_case], tmp_path)


def test_attach_metadata_mode_matches_manifest(tmp_path: Path) -> None:
    case_dir = write_common_inputs(tmp_path, "system_utilization")
    write_metadata(
        case_dir,
        {
            "recipe": "system_utilization",
            "profile_mode": "attach",
        },
    )

    test_case = make_testcase("system_utilization")
    test_case["prerecord"] = {"mode": "attach"}

    harness._validate_prerecorded_metadata([test_case], tmp_path)
