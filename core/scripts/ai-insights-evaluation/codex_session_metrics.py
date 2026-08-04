# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Read MCP call metrics from persisted Codex session transcripts."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Iterator


def iter_jsonl(path: Path) -> Iterator[dict[str, Any]]:
    """Yield JSON objects from a completed Codex JSONL transcript."""
    if not path.is_file():
        return
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError(f"{path} line {line_number} is not valid JSON") from exc
        if isinstance(value, dict):
            yield value


def codex_session_log(codex_home: Path) -> Path:
    """Return the single Codex session transcript for an attempt."""
    sessions = codex_home / "sessions"
    session_logs = sorted(sessions.rglob("*.jsonl")) if sessions.is_dir() else []
    if len(session_logs) != 1:
        raise RuntimeError(
            f"expected exactly one Codex session log under {sessions}, found {len(session_logs)}"
        )
    return session_logs[0]


def collect_mcp_tool_metrics(codex_home: Path, server: str) -> dict[str, int | float]:
    """Return MCP call counts and durations grouped by outcome."""
    succeeded = 0
    failed = 0
    succeeded_duration = 0.0
    failed_duration = 0.0
    session_log = codex_session_log(codex_home)
    for event in iter_jsonl(session_log):
        payload = event.get("payload") or {}
        invocation = payload.get("invocation") or {}
        if (
            payload.get("type") != "mcp_tool_call_end"
            or invocation.get("server") != server
        ):
            continue
        duration = payload.get("duration") or {}
        seconds = duration.get("secs")
        nanoseconds = duration.get("nanos")
        if not isinstance(seconds, (int, float)) or not isinstance(nanoseconds, (int, float)):
            raise RuntimeError(f"Codex MCP call for {server} has no valid duration")
        elapsed = seconds + nanoseconds / 1_000_000_000
        result = payload.get("result")
        if not isinstance(result, dict) or len({"Ok", "Err"}.intersection(result)) != 1:
            raise RuntimeError(f"Codex MCP call for {server} has no valid result")
        if "Err" in result:
            failed += 1
            failed_duration += elapsed
            continue
        response = result["Ok"]
        if not isinstance(response, dict):
            raise RuntimeError(f"Codex MCP call for {server} has no valid response")
        if response.get("isError") or response.get("is_error"):
            failed += 1
            failed_duration += elapsed
        else:
            succeeded += 1
            succeeded_duration += elapsed
    if succeeded + failed == 0:
        raise RuntimeError(f"Codex session contains no MCP calls for {server}")
    return {
        "tool_calls_succeeded": succeeded,
        "tool_calls_failed": failed,
        "tool_duration_seconds_succeeded": succeeded_duration,
        "tool_duration_seconds_failed": failed_duration,
    }
