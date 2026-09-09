# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

import pytest


MODULE_DIR = Path(__file__).resolve().parent
MODULE_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_prerecord_run",
    MODULE_DIR / "prerecord-run.py",
)
prerecord = importlib.util.module_from_spec(MODULE_SPEC)
MODULE_SPEC.loader.exec_module(prerecord)


def configure_run(
    tmp_path: Path,
    monkeypatch,
    recipe: str,
    recipe_params: list[str] | None = None,
    profile_mode: str = "launch",
):
    cli = tmp_path / "apx"
    cli.write_text("", encoding="utf-8")
    output_dir = tmp_path / "recordings"
    args = SimpleNamespace(
        case="test_case_32",
        cli_bin=cli,
        target="target",
        recipe=recipe,
        workload_cmd=None if profile_mode == "system-wide" else "/remote/test_case_32",
        pid=None,
        timeout=None,
        prerecord=json.dumps({"mode": profile_mode}),
        params_json=json.dumps(recipe_params or []),
        ssh_target=None,
        ssh_key=None,
        target_workload_root=None,
        output_dir=output_dir,
        artifactory_run_base=None,
        param=[],
    )
    monkeypatch.setattr(prerecord, "parse_args", lambda: args)
    monkeypatch.setattr(prerecord, "parse_recipe_run_id", lambda output: "run-123")
    monkeypatch.setattr(prerecord, "print_run_info", lambda cli_bin, run_id: None)
    monkeypatch.setattr(prerecord, "get_cli_version", lambda cli_bin: "1.0")
    run_cli = Mock(return_value=SimpleNamespace(stdout="", stderr=""))
    monkeypatch.setattr(prerecord, "run_cli", run_cli)

    def export_run(cli_bin, run_id, destination):
        archive = destination / "export.zip"
        archive.write_bytes(b"run")
        return archive

    monkeypatch.setattr(prerecord, "export_run", export_run)
    return output_dir, run_cli


@pytest.mark.parametrize("modification", [{}, [], "unknown"])
def test_run_modification_validation_rejects_invalid_config(
    modification: object,
) -> None:
    with pytest.raises(ValueError, match=r"run[_ ]modification"):
        prerecord.run_modification_from_config(
            {"run_modification": modification}, "asct"
        )


@pytest.mark.parametrize(
    "modification",
    ["core_to_core_latency_asymmetry", "low_peak_bandwidth"],
)
def test_asct_modifications_require_asct_recipe(modification: str) -> None:
    with pytest.raises(ValueError, match="requires recipe 'asct'"):
        prerecord.run_modification_from_config(
            {"run_modification": modification}, "code_hotspots"
        )


def test_materialize_run_artifact_copies_ordinary_archive(tmp_path: Path) -> None:
    source = tmp_path / "source.zip"
    destination = tmp_path / "latest.zip"
    stale_report = tmp_path / "run-modification-report.json"
    source.write_bytes(b"run")
    stale_report.write_bytes(b"stale")

    prerecord.materialize_run_artifact(source, destination, None)

    assert destination.read_bytes() == b"run"
    assert not stale_report.exists()


def test_prerecord_metadata_describes_transformed_archive(
    tmp_path: Path, monkeypatch
) -> None:
    output_dir, _ = configure_run(
        tmp_path, monkeypatch, "asct", profile_mode="system-wide"
    )
    args = prerecord.parse_args()
    args.prerecord = json.dumps(
        {"mode": "system-wide", "run_modification": "low_peak_bandwidth"}
    )

    def materialize(source, destination, modification):
        assert source.read_bytes() == b"run"
        assert modification == "low_peak_bandwidth"
        destination.write_bytes(b"transformed run")
        destination.with_name("run-modification-report.json").write_bytes(b"report")

    monkeypatch.setattr(prerecord, "materialize_run_artifact", materialize)

    assert prerecord.main() == 0

    case_dir = output_dir / "test_case_32"
    latest = case_dir / "latest.zip"
    metadata = json.loads((case_dir / "metadata.json").read_text(encoding="utf-8"))
    assert latest.read_bytes() == b"transformed run"
    assert (case_dir / "run-modification-report.json").read_bytes() == b"report"
    assert metadata["archive_size_bytes"] == latest.stat().st_size
    assert metadata["archive_sha256"] == prerecord.sha256_file(latest)
    assert metadata["run_modification"] == "low_peak_bandwidth"


@pytest.mark.parametrize(
    "recipe", ["system_utilization", "syscall_trace_summary", "custom_recipe"]
)
def test_recipe_without_source_query_omits_source_archive(
    tmp_path: Path, monkeypatch, recipe: str
) -> None:
    output_dir, run_cli = configure_run(tmp_path, monkeypatch, recipe)
    collect_sources = Mock()
    monkeypatch.setattr(prerecord, "collect_sampled_sources", collect_sources)

    assert prerecord.main() == 0

    case_dir = output_dir / "test_case_32"
    metadata = json.loads((case_dir / "metadata.json").read_text(encoding="utf-8"))
    assert (case_dir / "latest.zip").is_file()
    assert not (case_dir / "test_src.zip").exists()
    assert "source_archive_path" not in metadata
    collect_sources.assert_not_called()
    assert "--source" not in run_cli.call_args.args[0]


def test_system_wide_prerecord_omits_workload(
    tmp_path: Path, monkeypatch
) -> None:
    output_dir, run_cli = configure_run(
        tmp_path,
        monkeypatch,
        "system_utilization",
        profile_mode="system-wide",
    )

    assert prerecord.main() == 0

    command = run_cli.call_args.args[0]
    assert "--system-wide" in command
    assert "--workload" not in command
    metadata = json.loads((output_dir / "test_case_32" / "metadata.json").read_text(encoding="utf-8"))
    assert metadata["profile_mode"] == "system-wide"


def test_system_wide_prerecord_rejects_lifecycle_commands(
    tmp_path: Path, monkeypatch, capsys
) -> None:
    output_dir, run_cli = configure_run(
        tmp_path, monkeypatch, "code_hotspots", profile_mode="system-wide"
    )
    args = prerecord.parse_args()
    args.prerecord = json.dumps(
        {
            "mode": "system-wide",
            "setup": ["bash", "sidecar.sh", "setup"],
            "cleanup": ["bash", "sidecar.sh", "cleanup"],
        }
    )

    assert prerecord.main() == 1
    assert "system-wide prerecord cannot use" in capsys.readouterr().err
    assert not output_dir.exists()
    run_cli.assert_not_called()


def test_code_hotspots_prerecord_writes_source_archive(
    tmp_path: Path, monkeypatch
) -> None:
    output_dir, run_cli = configure_run(tmp_path, monkeypatch, "code_hotspots")

    def collect_sources(cli_bin, run_id, case_dir, recipe, query):
        archive = case_dir / "test_src.zip"
        archive.write_bytes(b"source")
        assert recipe == "code_hotspots"
        assert "periodic_samples" in query
        return archive

    monkeypatch.setattr(prerecord, "collect_sampled_sources", collect_sources)

    assert prerecord.main() == 0

    case_dir = output_dir / "test_case_32"
    metadata = json.loads((case_dir / "metadata.json").read_text(encoding="utf-8"))
    assert (case_dir / "test_src.zip").is_file()
    assert metadata["source_archive_path"].endswith("test_src.zip")
    assert "--source" not in run_cli.call_args.args[0]


@pytest.mark.parametrize(
    ("expected_recipe", "query_marker"),
    [
        ("cpu_microarchitecture", "periodic_samples"),
        ("cache_sharing", "perf.c2c.samples"),
        ("memory_access", "spe.samples.count"),
    ],
)
def test_sampled_recipe_prerecord_writes_source_archive(
    tmp_path: Path, monkeypatch, expected_recipe: str, query_marker: str
) -> None:
    output_dir, run_cli = configure_run(tmp_path, monkeypatch, expected_recipe)

    def collect_sources(cli_bin, run_id, case_dir, recipe, query):
        archive = case_dir / "test_src.zip"
        archive.write_bytes(b"source")
        assert recipe == expected_recipe
        assert query_marker in query
        return archive

    monkeypatch.setattr(prerecord, "collect_sampled_sources", collect_sources)

    assert prerecord.main() == 0

    case_dir = output_dir / "test_case_32"
    metadata = json.loads((case_dir / "metadata.json").read_text(encoding="utf-8"))
    assert (case_dir / "test_src.zip").is_file()
    assert metadata["source_archive_path"].endswith("test_src.zip")
    assert "--source" not in run_cli.call_args.args[0]


@pytest.mark.parametrize(
    "stack_param", ["collect_java_stacks=true", "collect_dotnet_stacks=true"]
)
def test_cpu_microarchitecture_preserves_managed_stack_params(
    tmp_path: Path, monkeypatch, stack_param: str
) -> None:
    output_dir, run_cli = configure_run(
        tmp_path, monkeypatch, "cpu_microarchitecture", [stack_param]
    )

    def collect_sources(cli_bin, run_id, case_dir, recipe, query):
        archive = case_dir / "test_src.zip"
        archive.write_bytes(b"source")
        return archive

    monkeypatch.setattr(prerecord, "collect_sampled_sources", collect_sources)

    assert prerecord.main() == 0

    metadata = json.loads(
        (output_dir / "test_case_32" / "metadata.json").read_text(encoding="utf-8")
    )
    assert metadata["recipe_params"] == [stack_param]
    assert run_cli.call_args.args[0][-2:] == ["--param", stack_param]


def test_static_instruction_mix_prerecord_omits_source_archive(
    tmp_path: Path, monkeypatch
) -> None:
    output_dir, run_cli = configure_run(
        tmp_path,
        monkeypatch,
        "instruction_mix",
        ["mode=static"],
    )
    collect_sources = Mock()
    monkeypatch.setattr(prerecord, "collect_sampled_sources", collect_sources)

    assert prerecord.main() == 0

    case_dir = output_dir / "test_case_32"
    metadata = json.loads((case_dir / "metadata.json").read_text(encoding="utf-8"))
    assert not (case_dir / "test_src.zip").exists()
    assert "source_archive_path" not in metadata
    collect_sources.assert_not_called()
    assert "--source" not in run_cli.call_args.args[0]


def test_code_hotspots_prerecord_writes_empty_source_archive(
    tmp_path: Path, monkeypatch
) -> None:
    output_dir, run_cli = configure_run(tmp_path, monkeypatch, "code_hotspots")

    def collect_sources(cli_bin, run_id, case_dir, recipe, query):
        archive = case_dir / "test_src.zip"
        archive.write_bytes(b"empty source manifest")
        assert recipe == "code_hotspots"
        assert "periodic_samples" in query
        return archive

    collect = Mock(side_effect=collect_sources)
    monkeypatch.setattr(prerecord, "collect_sampled_sources", collect)

    assert prerecord.main() == 0

    assert (output_dir / "test_case_32" / "test_src.zip").is_file()
    collect.assert_called_once()
    assert "--source" not in run_cli.call_args.args[0]


def test_prerecord_rejects_invalid_instruction_mix_mode(
    tmp_path: Path, monkeypatch
) -> None:
    output_dir, _ = configure_run(
        tmp_path,
        monkeypatch,
        "instruction_mix",
        ["mode=statc"],
    )

    assert prerecord.main() == 1
    assert not output_dir.exists()


def test_collect_sampled_sources_preserves_instruction_mix_weights(
    tmp_path: Path, monkeypatch
) -> None:
    cli = tmp_path / "apx"
    cli.write_text("", encoding="utf-8")
    case_dir = tmp_path / "recordings" / "test_case_32"
    case_dir.mkdir(parents=True)

    monkeypatch.setattr(prerecord, "render_run", lambda cli_bin, run_id: "session-123")
    monkeypatch.setattr(prerecord, "close_render_session", lambda cli_bin, session_id: None)

    def query_render_rows(cli_bin, session_id, query):
        if "load_source_content" in query:
            return [{"content": "int main() { return 0; }\n"}]
        assert "SUM(CASE" in query
        assert "MAX(CASE" not in query
        return [
            {
                "source_file_id": 7,
                "target_location": "/src/main.cpp",
                "host_location": "",
                "self_samples": 200,
            },
        ]

    monkeypatch.setattr(prerecord, "query_render_rows", query_render_rows)

    archive = prerecord.collect_sampled_sources(
        cli,
        "run-123",
        case_dir,
        "instruction_mix",
        prerecord.source_files_query("instruction_mix"),
    )

    assert archive == case_dir / "test_src.zip"
    manifest = json.loads((case_dir / "test_src" / "manifest.json").read_text(encoding="utf-8"))
    assert manifest["fetched"] == [
        {
            "source_file_id": 7,
            "target_location": "/src/main.cpp",
            "host_location": "",
            "periodic_samples": 200,
            "relative_path": "src/main.cpp",
            "size_bytes": 25,
        }
    ]


def test_cpu_microarchitecture_uses_sampled_source_query() -> None:
    query = prerecord.source_files_query("cpu_microarchitecture")

    assert query is not None
    assert "periodic_samples" in query
