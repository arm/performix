# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Test JFR checks against a fake CLI and an already imported run.

The fake CLI lets these tests check missing-table errors, query attempts,
timeouts, and session cleanup. It does not prove that a particular recording
contains JFR data.
"""

import json
import subprocess
from pathlib import Path
from types import SimpleNamespace

import pytest

import jfr_preflight
from tests.evaluation import test_ai_insights_evaluation as evaluation


@pytest.mark.parametrize(
    ("included_renderers", "expected_error", "query_times_out"),
    [
        (("jfr_recordings",), "missing rendered tables", False),
        (jfr_preflight.REQUIRED_JFR_RENDERERS, None, False),
        (
            jfr_preflight.REQUIRED_JFR_RENDERERS,
            "JFR preflight failed for test_case_72:.*timed out",
            True,
        ),
    ],
)
def test_jfr_preflight_checks_rendered_tables(
    monkeypatch: pytest.MonkeyPatch,
    included_renderers: tuple[str, ...],
    expected_error: str | None,
    query_times_out: bool,
) -> None:
    """Reject missing or timed-out JFR tables; query all required tables when present."""
    commands = []
    env = {"APXD_DATA_DIR": "private-data"}

    def fake_run_cli(command, **kwargs):
        commands.append(command)
        assert kwargs["env"] is env
        if command[1:3] == ["run", "render"]:
            data = {"invocation": {
                "session_id": "session-1",
                "manifest": {"entry": [
                    {"renderer_id": {"value": renderer}, "table_name": f"table_{renderer}"}
                    for renderer in included_renderers
                ]},
            }}
        elif command[1:3] == ["render", "query"]:
            if query_times_out:
                raise subprocess.TimeoutExpired(command, 120)
            data = {"rows": [] if "LIMIT 0" in command[4] else [{"Recording": "0"}]}
        else:
            return SimpleNamespace(stdout="")
        return SimpleNamespace(stdout=json.dumps({"data": data}))

    monkeypatch.setattr(jfr_preflight, "run_cli", fake_run_cli)

    if expected_error:
        with pytest.raises(pytest.UsageError, match=expected_error):
            jfr_preflight.validate_jfr_run("test_case_72", "run-1", Path("apx"), env)
    else:
        jfr_preflight.validate_jfr_run("test_case_72", "run-1", Path("apx"), env)

    assert any(command[1:3] == ["render", "close"] for command in commands)
    if expected_error is None:
        assert sum(command[1:3] == ["render", "query"] for command in commands) == 7


@pytest.mark.parametrize("requires_jfr", [False, True])
def test_jfr_requirement_passes_validator_to_run_import(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, requires_jfr: bool,
) -> None:
    archive = tmp_path / "test_case_72" / "latest.zip"
    archive.parent.mkdir()
    archive.write_bytes(b"saved run")
    test_case = {
        "id": "test_case_72",
        "recipe": "code_hotspots",
        "run_artifact": "test_case_72/latest.zip",
        "requirements": ["jfr_data"] if requires_jfr else [],
        "prerecord": {"enable_jfr_capture": True},
    }
    cfg = {"run_artifact_base": tmp_path, "cli_bin": Path("apx")}
    checked = []

    class FakePerformix:
        def import_fixture(self, cli, run_archive, source_root, validate_run=None):
            assert run_archive == archive
            if validate_run:
                validate_run("imported-run", {"APXD_DATA_DIR": "private-data"})
            return "imported-run"

    monkeypatch.setattr(evaluation, "requires_source_archive", lambda *args: False)
    monkeypatch.setattr(evaluation, "validate_jfr_run", lambda *args: checked.append(args))
    result = evaluation.import_attempt_run(test_case, cfg, FakePerformix())

    assert result["run_id"] == "imported-run"
    if requires_jfr:
        assert checked == [
            ("test_case_72", "imported-run", Path("apx"), {"APXD_DATA_DIR": "private-data"})
        ]
    else:
        assert checked == []


@pytest.mark.parametrize("renderers,matching_rows,expected_error", [
    (jfr_preflight.REQUIRED_JFR_RENDERERS,
     [{"Recording": "0"}], None),
    (jfr_preflight.REQUIRED_JFR_RENDERERS,
     [], "no parsed recording"),
    (("jfr_recordings", "jvm_garbage_collections", "jvm_heap_summary"), [], "missing rendered tables"),
])
def test_jfr_preflight_checks_contents_and_closes_session(
    monkeypatch: pytest.MonkeyPatch, renderers: tuple[str, ...],
    matching_rows: list[dict], expected_error: str | None,
) -> None:
    commands = []

    def fake_run_cli(command, **kwargs):
        commands.append(command)
        if command[1:3] == ["run", "render"]:
            data = {"invocation": {
                "session_id": "config-session",
                "manifest": {"entry": [
                    {"renderer_id": {"value": renderer}, "table_name": f"table_{renderer}"}
                    for renderer in renderers
                ]},
            }}
        elif command[1:3] == ["render", "query"]:
            data = {"rows": [] if "LIMIT 0" in command[4] else matching_rows}
        else:
            return SimpleNamespace(stdout="")
        return SimpleNamespace(stdout=json.dumps({"data": data}))

    monkeypatch.setattr(jfr_preflight, "run_cli", fake_run_cli)
    if expected_error:
        with pytest.raises(pytest.UsageError, match=expected_error):
            jfr_preflight.validate_jfr_run("control", "run-1", Path("apx"), {})
    else:
        jfr_preflight.validate_jfr_run("control", "run-1", Path("apx"), {})
    assert commands[-1][1:4] == ["render", "close", "config-session"]
    if expected_error is None:
        content_query = commands[-2][4]
        assert 'i."Recording" = r."Recording"' in content_query
        assert 'f."Recording" = r."Recording"' in content_query
        assert 'p."Recording" = r."Recording"' in content_query
