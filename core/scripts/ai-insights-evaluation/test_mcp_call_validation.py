# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for MCP call validation in the AI Insights evaluator."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from test_ai_insights_evaluation import (
    MCP_MODES,
    McpServer,
    codex_truncation_markers,
    write_codex_home,
    should_fail_on_tool_output_truncation,
    validate_mcp_call,
)


MODE = McpServer(
    id="performix_mcp",
    server="arm-performix",
    tool="generate_ai_insights",
    args=("mcp", "start", "--acknowledge-source-sharing"),
)


def _write_events(path: Path, *items: dict) -> None:
    path.write_text(
        "".join(
            json.dumps({"type": "item.completed", "item": item}) + "\n"
            for item in items
        ),
        encoding="utf-8",
    )


@pytest.mark.parametrize("mode", MCP_MODES.values(), ids=lambda mode: mode.id)
def test_mcp_modes_use_performix_cli(tmp_path: Path, mode: McpServer) -> None:
    import tomli
    from performix_attempt import prepare_performix_state

    cli = tmp_path / "bin" / "apx"
    home = write_codex_home(
        tmp_path, tmp_path, {"cli_bin": cli},
        mode, False, prepare_performix_state(tmp_path)
    )
    config = tomli.loads((home / "config.toml").read_text())
    server = config["mcp_servers"][mode.server]
    assert server["command"] == str(cli)
    assert server["cwd"] == str(cli.parent)
    assert server["args"] == ["mcp", "start", "--acknowledge-source-sharing"]
    assert server["env"]["APXD_ENABLE_EXPERIMENTAL_RECIPES"] == "true"
    assert server["env"]["APXD_CODE_HOTSPOTS_AI_INSIGHTS_MODE"] == {
        "performix_mcp": "curated",
        "performix_mcp_run_query": "run_query",
    }[mode.id]


def test_query_comparison_mode_reports_failed_run_query(tmp_path: Path) -> None:
    raw_jsonl = tmp_path / "codex_exec.jsonl"
    _write_events(
        raw_jsonl,
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "generate_ai_insights",
            "status": "completed",
        },
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "run_query",
            "status": "failed",
            "error": {"message": "invalid SQL"},
        },
    )

    result = validate_mcp_call(raw_jsonl, MCP_MODES["performix_mcp_run_query"])

    assert result["required_completed_calls"] == 1
    assert result["failed_by_tool"] == {"run_query": 1}


def test_auxiliary_tool_failure_does_not_invalidate_attempt(
    tmp_path: Path, caplog: pytest.LogCaptureFixture
) -> None:
    raw_jsonl = tmp_path / "codex_exec.jsonl"
    _write_events(
        raw_jsonl,
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "generate_ai_insights",
            "status": "completed",
        },
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "run_query",
            "status": "failed",
            "error": {"message": "invalid SQL"},
        },
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "run_query",
            "status": "completed",
        },
    )

    result = validate_mcp_call(raw_jsonl, MODE)

    assert result["required_completed_calls"] == 1
    assert result["failed_calls"] == 1
    assert result["failed_by_tool"] == {"run_query": 1}
    assert result["failure_rate_percent_by_tool"] == {
        "generate_ai_insights": 0.0,
        "run_query": 50.0,
    }
    assert result["failed_messages"] == ["run_query: invalid SQL"]
    assert "run_query: 1/2 failed (50.0%)" in caplog.text


def test_structured_mcp_tool_error_is_reported(tmp_path: Path) -> None:
    raw_jsonl = tmp_path / "codex_exec.jsonl"
    _write_events(
        raw_jsonl,
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "generate_ai_insights",
            "status": "completed",
        },
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "run_query",
            "status": "failed",
            "error": None,
            "result": {
                "structured_content": {
                    "error": {"message": "table does not exist"},
                },
            },
        },
    )

    result = validate_mcp_call(raw_jsonl, MODE)

    assert result["failed_messages"] == ["run_query: table does not exist"]


def test_required_ai_insights_call_must_still_succeed(tmp_path: Path) -> None:
    raw_jsonl = tmp_path / "codex_exec.jsonl"
    _write_events(
        raw_jsonl,
        {
            "type": "mcp_tool_call",
            "server": "arm-performix",
            "tool": "run_query",
            "status": "completed",
        },
    )

    with pytest.raises(RuntimeError, match="generate_ai_insights did not complete successfully"):
        validate_mcp_call(raw_jsonl, MODE)


def test_completed_auxiliary_call_does_not_mask_failed_required_call(
    tmp_path: Path,
) -> None:
    raw_jsonl = tmp_path / "codex_exec.jsonl"
    events = [
        {
            "type": "item.completed",
            "item": {
                "type": "mcp_tool_call",
                "server": "arm-performix",
                "tool": "generate_ai_insights",
                "status": "failed",
            },
        },
        {
            "type": "item.completed",
            "item": {
                "type": "mcp_tool_call",
                "server": "arm-performix",
                "tool": "run_query",
                "status": "completed",
            },
        },
    ]
    raw_jsonl.write_text(
        "".join(json.dumps(event, separators=(",", ":")) + "\n" for event in events),
        encoding="utf-8",
    )

    with pytest.raises(
        RuntimeError, match="generate_ai_insights did not complete successfully"
    ):
        validate_mcp_call(raw_jsonl, MODE)


def test_truncation_markers_record_the_producing_tool(tmp_path: Path) -> None:
    codex_home = tmp_path / "codex_home"
    session = codex_home / "sessions" / "2026" / "08" / "13" / "session.jsonl"
    session.parent.mkdir(parents=True)
    events = [
        {
            "type": "response_item",
            "payload": {
                "type": "function_call",
                "namespace": "mcp__arm_performix",
                "name": "run_query",
                "call_id": "query-call",
            },
        },
        {
            "type": "response_item",
            "payload": {
                "type": "function_call_output",
                "call_id": "query-call",
                "output": "Output:\n…1234 tokens truncated…",
            },
        },
    ]
    session.write_text("".join(json.dumps(event) + "\n" for event in events), encoding="utf-8")

    assert codex_truncation_markers(codex_home, MODE.server) == [
        {
            "namespace": "mcp__arm_performix",
            "tool": "run_query",
            "token_count": 1234,
        }
    ]


def test_performix_function_call_requires_tool_name(tmp_path: Path) -> None:
    codex_home = tmp_path / "codex_home"
    session = codex_home / "sessions" / "2026" / "08" / "13" / "session.jsonl"
    session.parent.mkdir(parents=True)
    event = {
        "type": "response_item",
        "payload": {
            "type": "function_call",
            "namespace": "mcp__arm_performix",
            "call_id": "query-call",
        },
    }
    session.write_text(json.dumps(event) + "\n", encoding="utf-8")

    with pytest.raises(RuntimeError, match="arm-performix MCP call without a tool name"):
        codex_truncation_markers(codex_home, MODE.server)


def test_other_mcp_server_truncation_is_retained_for_warning(tmp_path: Path) -> None:
    codex_home = tmp_path / "codex_home"
    session = codex_home / "sessions" / "2026" / "08" / "13" / "session.jsonl"
    session.parent.mkdir(parents=True)
    events = [
        {
            "type": "response_item",
            "payload": {
                "type": "function_call",
                "namespace": "mcp__other_server",
                "name": "run_query",
                "call_id": "query-call",
            },
        },
        {
            "type": "response_item",
            "payload": {
                "type": "function_call_output",
                "call_id": "query-call",
                "output": "Output:\n…1234 tokens truncated…",
            },
        },
    ]
    session.write_text("".join(json.dumps(event) + "\n" for event in events), encoding="utf-8")

    details = codex_truncation_markers(codex_home, MODE.server)

    assert details == [
        {"namespace": "mcp__other_server", "tool": "run_query", "token_count": 1234}
    ]
    assert not should_fail_on_tool_output_truncation(
        "performix_mcp",
        {"truncation_markers": 1, "truncation_details": details},
    )


def test_auxiliary_tool_truncation_does_not_invalidate_attempt() -> None:
    invoke_meta = {
        "truncation_markers": 1,
        "truncation_details": [
            {
                "namespace": "mcp__arm_performix",
                "tool": "run_query",
                "token_count": 1234,
            }
        ],
    }

    assert not should_fail_on_tool_output_truncation("performix_mcp", invoke_meta)


def test_run_query_mode_truncation_does_not_invalidate_attempt() -> None:
    invoke_meta = {
        "truncation_markers": 1,
        "truncation_details": [
            {
                "namespace": "mcp__arm_performix",
                "tool": "run_query",
                "token_count": 1234,
            }
        ],
    }

    assert not should_fail_on_tool_output_truncation(
        "performix_mcp_run_query", invoke_meta
    )


def test_required_tool_truncation_invalidates_attempt() -> None:
    invoke_meta = {
        "truncation_markers": 1,
        "truncation_details": [
            {
                "namespace": "mcp__arm_performix",
                "tool": "generate_ai_insights",
                "token_count": 1234,
            }
        ],
    }

    assert should_fail_on_tool_output_truncation("performix_mcp", invoke_meta)


def test_required_details_tool_truncation_invalidates_attempt() -> None:
    invoke_meta = {
        "truncation_markers": 1,
        "truncation_details": [
            {
                "namespace": "mcp__arm_performix",
                "tool": "read_ai_insights_payload_details",
                "token_count": 1234,
            }
        ],
    }

    assert should_fail_on_tool_output_truncation("performix_mcp", invoke_meta)


def test_curated_tool_name_in_other_namespace_is_only_a_warning() -> None:
    invoke_meta = {
        "truncation_markers": 1,
        "truncation_details": [
            {
                "namespace": "mcp__other_server",
                "tool": "generate_ai_insights",
                "token_count": 1234,
            }
        ],
    }

    assert not should_fail_on_tool_output_truncation("performix_mcp", invoke_meta)


def test_unattributed_truncation_invalidates_attempt() -> None:
    invoke_meta = {
        "truncation_markers": 1,
        "truncation_marker_token_counts": [1234],
    }

    assert should_fail_on_tool_output_truncation("performix_mcp", invoke_meta)
