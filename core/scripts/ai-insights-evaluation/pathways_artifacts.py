# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Capture the limited evidence retained from a Pathways test attempt.

A Pathways test gives an AI agent a host workspace and a disposable target
machine. CI terminates that machine after the attempt; local developers clean
up their target manually. Source checkouts, agent state and connection details
are needed during execution but must not be copied wholesale into CI artifacts.

This module defines the small, reviewable evidence set that survives cleanup:

- ``codex_session.jsonl``: the agent session record.
- ``codex_session_000.jsonl``, etc.: transcripts saved when multiple sessions are found.
- ``codex_exec.jsonl`` and ``codex_stderr.txt``: Codex diagnostic output.
- ``codex_last_message.txt`` and ``llm_response.md``: the final agent response.
- ``performix_runs.json``: recorded Performix recipe calls and results.
- ``score.json`` and ``score.md``: the machine-readable and human-readable
  evaluation result.

It also provides helpers to capture that evidence before CI removes the target
and select result files for staging and archiving.
"""

import json
import re
import shutil
from pathlib import Path

from codex_session_metrics import iter_mcp_events

PATHWAYS_ARTIFACTS = frozenset({
    "codex_session.jsonl", "performix_runs.json",
    "codex_last_message.txt", "llm_response.md", "score.json", "score.md",
    "codex_exec.jsonl", "codex_stderr.txt",
})


def is_pathways_artifact(name: str) -> bool:
    """Retain named evidence and numbered transcripts from multi-session failures."""
    return name in PATHWAYS_ARTIFACTS or re.fullmatch(r"codex_session_[0-9]{3,}\.jsonl", name) is not None


def pathways_test_ids(manifest: dict) -> set[str]:
    """Return manifest test IDs whose attempt files require Pathways filtering."""
    return {
        case["id"]
        for case in manifest["tests"]
        if case.get("pathways_execution")
    }


def result_path_contains_symlink(results: Path, relative: Path) -> bool:
    """Reject symlinked path components that could read outside the results tree."""
    return any(
        results.joinpath(*relative.parts[:index]).is_symlink()
        for index in range(1, len(relative.parts) + 1)
    )


def pathways_attempt_directory_index(relative: Path, pathways_ids: set[str]) -> int | None:
    """Return the ``attempts`` path index for a Pathways result, or ``None``."""
    if "attempts" not in relative.parts:
        return None

    index = relative.parts.index("attempts")
    if index >= 2 and relative.parts[index - 2] in pathways_ids:
        return index
    return None


def mcp_recipe_calls(session: Path) -> tuple[list[dict], list[int]]:
    """Read actual MCP recipe events, retaining incomplete calls and partial lines."""
    runs = {}
    invalid_lines = []
    for number, event in iter_mcp_events(session, "arm-performix", invalid_lines=invalid_lines):
        invocation = event["invocation"]
        if invocation.get("tool") != "run_recipe":
            continue
        key = event.get("call_id") or f"line-{number}"
        record = runs.setdefault(key, {"call_id": key, "session_line": number})
        if "arguments" in invocation:
            record["arguments"] = invocation["arguments"]
        if event["type"] == "mcp_tool_call_end":
            record["result"] = event.get("result")
            record["completed_event"] = True
    return list(runs.values()), invalid_lines


def mcp_recipe_run_id(call: dict) -> str | None:
    """Return an ID only for a completed recipe result without a tool error."""
    if not call.get("completed_event") or not isinstance(call.get("arguments"), dict):
        return None
    result = (call.get("result") or {}).get("Ok")
    if not isinstance(result, dict) or result.get("isError") or result.get("is_error"):
        return None
    structured = result.get("structuredContent") or result
    for content in result.get("content", []):
        if content.get("type") == "text":
            try:
                candidate = json.loads(content.get("text", ""))
                if isinstance(candidate, dict) and candidate.get("run_id"):
                    structured = candidate
            except ValueError:
                pass
    if (not isinstance(structured, dict) or structured.get("error")
            or structured.get("run_status") != "completed"):
        return None
    run_id = structured.get("run_id")
    return run_id if isinstance(run_id, str) and run_id else None


def load_pathways_judge_evidence(session: Path, supplied_run_id: str | None) -> tuple[dict, str | None]:
    """Load judge evidence and report an incomplete transcript without discarding it."""
    calls, invalid_lines = mcp_recipe_calls(session) if session.is_file() else ([], [])
    transcript = session.read_text(encoding="utf-8", errors="replace") if session.is_file() else ""
    error = None
    if invalid_lines or not transcript:
        error = f"Missing/empty transcript or invalid JSONL lines: {invalid_lines}"
    return {
        "supplied_run_id": supplied_run_id,
        "successful_recipe_run_ids": [run_id for call in calls if (run_id := mcp_recipe_run_id(call))],
        "session_jsonl": transcript,
    }, error


def preserve_pathways_artifacts(attempt_dir: Path, errors: list[str]) -> None:
    """Preserve MCP recipe requests/results and other pathways evidence."""
    session = attempt_dir / "codex_session.jsonl"
    if not session.exists() and (attempt_dir / "codex_exec.jsonl").exists():
        shutil.copyfile(attempt_dir / "codex_exec.jsonl", session)
    calls, invalid_lines = mcp_recipe_calls(session)
    combined, unmatched = [], []
    for call in calls:
        run_id = mcp_recipe_run_id(call)
        if run_id:
            combined.append({"run_id": run_id, "sources": ["mcp"], "mcp_calls": [call]})
        else:
            unmatched.append(call)
    (attempt_dir / "performix_runs.json").write_text(json.dumps({
        "runs": combined, "unmatched_mcp_calls": unmatched, "invalid_session_lines": invalid_lines,
        "collection_errors": errors,
        "coverage": "Recorded MCP recipe calls only; this is not proof that no out-of-band profiling occurred",
    }, indent=2), encoding="utf-8")
    for path in attempt_dir.iterdir():
        if not is_pathways_artifact(path.name) and (path.is_file() or path.is_symlink()):
            path.unlink()
