# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for metrics extracted from persisted Codex sessions."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from codex_session_metrics import collect_mcp_tool_metrics


def _write_session(codex_home: Path, *payloads: dict) -> None:
    session = codex_home / "sessions" / "2026" / "07" / "28" / "session.jsonl"
    session.parent.mkdir(parents=True)
    session.write_text(
        "".join(
            json.dumps({"type": "event_msg", "payload": payload}) + "\n"
            for payload in payloads
        ),
        encoding="utf-8",
    )


def test_collects_mcp_duration_by_outcome(tmp_path: Path) -> None:
    codex_home = tmp_path / "codex_home"
    _write_session(
        codex_home,
        {
            "type": "mcp_tool_call_end",
            "invocation": {"server": "arm-performix", "tool": "generate_ai_insights"},
            "duration": {"secs": 0, "nanos": 250_000_000},
            "result": {"Ok": {}},
        },
        {
            "type": "mcp_tool_call_end",
            "invocation": {"server": "arm-performix", "tool": "generate_ai_insights"},
            "duration": {"secs": 10, "nanos": 500_000_000},
            "result": {"Err": "query failed"},
        },
        {
            "type": "mcp_tool_call_end",
            "invocation": {"server": "arm-performix", "tool": "generate_ai_insights"},
            "duration": {"secs": 1, "nanos": 0},
            "result": {"Ok": {"isError": True}},
        },
        {
            "type": "mcp_tool_call_end",
            "invocation": {"server": "another-server", "tool": "other"},
            "duration": {"secs": 30, "nanos": 0},
            "result": {"Ok": {}},
        },
    )

    metrics = collect_mcp_tool_metrics(codex_home, "arm-performix")

    assert metrics == {
        "tool_calls_succeeded": 1,
        "tool_calls_failed": 2,
        "tool_duration_seconds_succeeded": pytest.approx(0.25),
        "tool_duration_seconds_failed": pytest.approx(11.5),
    }
