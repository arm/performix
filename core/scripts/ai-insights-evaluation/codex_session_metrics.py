# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Utilities for Codex session transcripts: extract MCP call metrics and preserve attempt evidence."""

from __future__ import annotations

import json
import shutil
from pathlib import Path
from typing import Any, Iterator


def iter_jsonl(path: Path) -> Iterator[dict[str, Any]]:
    """Yield JSON objects from a completed Codex JSONL transcript."""
    for _, event in iter_jsonl_records(path):
        yield event


def iter_jsonl_records(path: Path, *, invalid_lines: list[int] | None = None) -> Iterator[tuple[int, dict[str, Any]]]:
    """Yield numbered objects; optionally record malformed lines and keep reading."""
    if not path.is_file():
        return
    with path.open("rb") as stream:
        for number, line in enumerate(stream, 1):
            if not line.strip():
                continue
            try:
                value = json.loads(line)
            except (ValueError, UnicodeError) as exc:
                if invalid_lines is None:
                    raise ValueError(f"{path} line {number} is not valid JSON") from exc
                invalid_lines.append(number)
                continue
            if isinstance(value, dict):
                yield number, value
            elif invalid_lines is not None:
                invalid_lines.append(number)


def iter_mcp_events(path: Path, server: str, *, invalid_lines: list[int] | None = None) -> Iterator[tuple[int, dict[str, Any]]]:
    """Yield numbered MCP begin/end events, preserving their source format and data."""
    for number, event in iter_jsonl_records(path, invalid_lines=invalid_lines):
        payload = event.get("payload") or {}
        try:
            if not isinstance(payload, dict):
                raise ValueError("payload is not an object")
            source_type = payload.get("type")
            if source_type == "function_call" and payload.get("namespace") == f"mcp__{server.replace('-', '_')}":
                arguments = payload.get("arguments")
                if isinstance(arguments, str):
                    try:
                        arguments = json.loads(arguments)
                    except ValueError:
                        pass  # Retain invalid arguments as incomplete evidence.
                payload = {"type": "mcp_tool_call_begin", "call_id": payload.get("call_id"),
                           "invocation": {"server": server, "tool": payload.get("name"), "arguments": arguments}}
            item = payload.get("item") or {}
            if source_type in ("item_started", "item_completed"):
                if not isinstance(item, dict):
                    raise ValueError("item is not an object")
                if item.get("type") == "McpToolCall":
                    payload = {
                        "type": "mcp_tool_call_end" if source_type == "item_completed" else "mcp_tool_call_begin",
                        "call_id": item.get("id"), "invocation": item,
                        "result": ({"Ok": item.get("result")} if item.get("status") == "completed"
                                   else {"Err": item.get("error")}),
                    }
            invocation = payload.get("invocation") or {}
            if not isinstance(invocation, dict):
                raise ValueError("invocation is not an object")
        except ValueError as exc:
            if invalid_lines is None:
                raise ValueError(f"{path} line {number}: {exc}") from exc
            invalid_lines.append(number)
            continue
        if invocation.get("server") == server and payload.get("type") in ("mcp_tool_call_begin", "mcp_tool_call_end"):
            yield number, {**payload, "source_type": source_type}


def codex_session_log(codex_home: Path) -> Path:
    """Return the single Codex session transcript for an attempt."""
    sessions = codex_home / "sessions"
    session_logs = sorted(sessions.rglob("*.jsonl")) if sessions.is_dir() else []
    if len(session_logs) != 1:
        raise RuntimeError(
            f"expected exactly one Codex session log under {sessions}, found {len(session_logs)}"
        )
    return session_logs[0]


def persist_codex_session_log(codex_home: Path, attempt_dir: Path) -> Path:
    """Copy the complete Codex session transcript into the attempt artefacts."""
    # Preserve every session even when the single-session metrics contract fails.
    sessions = sorted((codex_home / "sessions").rglob("*.jsonl"))
    if len(sessions) > 1:
        for index, source in enumerate(sessions):
            shutil.copyfile(source, attempt_dir / f"codex_session_{index:03d}.jsonl")
    source = codex_session_log(codex_home)
    destination = attempt_dir / "codex_session.jsonl"
    shutil.copyfile(source, destination)
    return destination


def collect_mcp_tool_metrics(codex_home: Path, server: str) -> dict[str, int | float]:
    """Return MCP call rounds, counts and durations grouped by outcome."""
    succeeded = 0
    failed = 0
    rounds = 0
    succeeded_duration = 0.0
    failed_duration = 0.0
    session_log = codex_session_log(codex_home)
    events = list(iter_mcp_events(session_log, server))
    target_call_ids = {
        event["call_id"]
        for _, event in events
        if event["source_type"] == "item_completed"
    }
    if None in target_call_ids:
        raise RuntimeError(f"Codex MCP call for {server} has no call ID")

    active_calls: set[str] = set()
    started_calls: set[str] = set()
    completed_calls: set[str] = set()
    for _, event in events:
        call_id = event["call_id"]
        if event["type"] == "mcp_tool_call_begin" and call_id in target_call_ids:
            # Codex may record the same start as both a function call and an item.
            if call_id in started_calls:
                continue
            if not active_calls:
                rounds += 1
            active_calls.add(call_id)
            started_calls.add(call_id)
            continue

        # Only completed items carry the timing and status used below.
        if event["source_type"] != "item_completed":
            continue
        item = event["invocation"]
        if call_id not in active_calls:
            raise RuntimeError(f"Codex MCP call for {server} has no matching start event")
        active_calls.remove(call_id)
        completed_calls.add(call_id)
        duration = item.get("duration") or {}
        seconds = duration.get("secs")
        nanoseconds = duration.get("nanos")
        if not isinstance(seconds, (int, float)) or not isinstance(nanoseconds, (int, float)):
            raise RuntimeError(f"Codex MCP call for {server} has no valid duration")
        elapsed = seconds + nanoseconds / 1_000_000_000
        if item.get("status") == "failed":
            failed += 1
            failed_duration += elapsed
            continue
        if item.get("status") != "completed":
            raise RuntimeError(f"Codex MCP call for {server} has an invalid status")
        response = item.get("result")
        if not isinstance(response, dict):
            raise RuntimeError(f"Codex MCP call for {server} has no valid response")
        if response.get("isError") or response.get("is_error"):
            failed += 1
            failed_duration += elapsed
        else:
            succeeded += 1
            succeeded_duration += elapsed
    if active_calls or completed_calls != target_call_ids:
        raise RuntimeError(f"Codex MCP calls for {server} are incomplete")
    if succeeded + failed == 0:
        raise RuntimeError(f"Codex session contains no MCP calls for {server}")
    return {
        "tool_rounds": rounds,
        "tool_calls_succeeded": succeeded,
        "tool_calls_failed": failed,
        "tool_duration_seconds_succeeded": succeeded_duration,
        "tool_duration_seconds_failed": failed_duration,
    }
