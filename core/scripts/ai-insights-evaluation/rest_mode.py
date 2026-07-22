# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""REST-mode adapter for the AI Insights pytest harness.

The model-facing REST payload and request construction is delegated to the
copied pathfinding implementation under `pathfinding_rest/`. This module only
adapts the current pytest harness inputs into the pathfinding run-directory
shape and records pytest-friendly invocation metadata.
"""

from __future__ import annotations

import hashlib
import importlib.util
import json
import logging
import os
import posixpath
import re
import subprocess
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from types import ModuleType
from typing import Any

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

sys.path.append(str(Path(__file__).resolve().parents[1]))
from run_export_helper import CommandFailure, run_cli


REST_MODE = "rest"
DEFAULT_API_BASE = "https://openai-api-proxy.geo.arm.com/api/providers/openai/v1"
OPENAI_RESPONSE_TIMEOUT_SECONDS = 120
OPENAI_RESPONSE_MAX_RETRIES = 3
OPENAI_RETRY_BACKOFF_FACTOR = 1.0
RETRYABLE_HTTP_STATUS = frozenset({429, 500, 502, 503, 504})
SOURCE_THRESHOLD_PERCENT = 99.0
SOURCE_CONTEXT_LINES = 10
SOURCE_HOT_WINDOW_THRESHOLD_PERCENT = 5.0
SOURCE_HOT_WINDOW_CONTEXT_LINES = 100
CALL_TREE_THRESHOLD_PERCENT = 99.0
DISASSEMBLY_THRESHOLD_PERCENT = 90.0
DISASSEMBLY_CONTEXT_LINES = 2
DISASSEMBLY_HOT_WINDOW_THRESHOLD_PERCENT = 5.0
DISASSEMBLY_HOT_WINDOW_CONTEXT_LINES = 100
PATHFINDING_REST_DIR = Path(__file__).resolve().parent / "pathfinding_rest"
PATHFINDING_SCRIPTS_DIR = PATHFINDING_REST_DIR / "private" / "scripts"
PATHFINDING_PROMPT = PATHFINDING_REST_DIR / "public" / "prompts" / "analysis_prompt.md"
LOGGER = logging.getLogger(__name__)

SAMPLE_SUMMARY_QUERY = """
SELECT
  SUM(periodic_samples) AS total_periodic_samples
FROM periodic_samples
""".strip()


def top_source_lines_query(threshold_percent: float) -> str:
    return f"""
WITH line_samples AS (
  SELECT
    p.source_file_id,
    sf.target_location,
    p.line_no,
    p."function" AS function_name,
    p.inlined,
    SUM(p.periodic_samples) AS periodic_samples
  FROM periodic_samples p
  LEFT JOIN source_files sf ON p.source_file_id = sf.source_file_id
  GROUP BY
    p.source_file_id,
    sf.target_location,
    p.line_no,
    p."function",
    p.inlined
),
totals AS (
  SELECT SUM(periodic_samples) AS total_periodic_samples
  FROM line_samples
),
ranked AS (
  SELECT
    source_file_id,
    target_location,
    line_no,
    function_name,
    inlined,
    periodic_samples,
    total_periodic_samples,
    SUM(periodic_samples) OVER (
      ORDER BY periodic_samples DESC, source_file_id, line_no, function_name
      ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
    ) AS cumulative_periodic_samples
  FROM line_samples, totals
)
SELECT
  source_file_id,
  target_location,
  line_no,
  function_name,
  inlined,
  periodic_samples,
  ROUND(100.0 * periodic_samples / NULLIF(total_periodic_samples, 0), 2) AS periodic_samples_percent
FROM ranked
WHERE total_periodic_samples IS NULL
   OR total_periodic_samples = 0
   OR (cumulative_periodic_samples - periodic_samples) < (total_periodic_samples * ({threshold_percent} / 100.0))
ORDER BY periodic_samples DESC
""".strip()


@dataclass(frozen=True)
class DisassemblyInstruction:
    address: int
    image_name: str
    symbol: str
    instruction: str
    arguments: str
    samples: int
    source_file_id: int | None
    source_file: str
    line_no: int | None


def timestamp_utc() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def write_json(path: Path, payload: Any) -> None:
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def compact_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def int_or_none(value: Any) -> int | None:
    if value is None:
        return None
    try:
        return int(float(value))
    except (TypeError, ValueError):
        return None


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_pathfinding_module(name: str, path: Path) -> ModuleType:
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load pathfinding module: {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def pathfinding_payload_module() -> ModuleType:
    return load_pathfinding_module(
        "ai_insights_pathfinding_build_llm_payload",
        PATHFINDING_SCRIPTS_DIR / "build_llm_payload.py",
    )


def pathfinding_request_module() -> ModuleType:
    return load_pathfinding_module(
        "ai_insights_pathfinding_build_openai_request",
        PATHFINDING_SCRIPTS_DIR / "build_openai_request.py",
    )


def run_apx_json(cli_bin: Path, argv: list[str]) -> dict[str, Any]:
    process = run_cli([str(cli_bin), *argv], cli_bin.parent)
    try:
        payload = json.loads(process.stdout)
    except json.JSONDecodeError as exc:
        raise ValueError(f"apx {' '.join(argv)} did not produce JSON") from exc
    if not isinstance(payload, dict):
        raise ValueError(f"apx {' '.join(argv)} JSON output was not an object")
    return payload


def render_run(cli_bin: Path, run_id: str) -> tuple[str, dict[str, Any]]:
    payload = run_apx_json(cli_bin, ["run", "render", run_id, "--json"])
    session_id = (((payload.get("data") or {}).get("invocation") or {}).get("session_id")) or ""
    if not session_id:
        raise ValueError(f"apx run render did not return a session id for run {run_id}")
    return str(session_id), payload


def close_render_session(cli_bin: Path, session_id: str) -> None:
    try:
        run_cli([str(cli_bin), "render", "close", session_id], cli_bin.parent)
    except CommandFailure as exc:
        LOGGER.warning("Failed to close render session %s: %s", session_id, exc)


def query_render_rows(cli_bin: Path, session_id: str, query: str) -> list[dict[str, Any]]:
    payload = run_apx_json(cli_bin, ["render", "query", session_id, query, "--json"])
    rows = ((payload.get("data") or {}).get("rows")) or []
    if not isinstance(rows, list):
        raise ValueError(f"render query rows are not a list for session {session_id}: {rows!r}")
    return [row for row in rows if isinstance(row, dict)]


def resolved_table_name(run_render_payload: dict[str, Any], visualization_id: str, table_key: str) -> str:
    entries = (
        (((run_render_payload.get("data") or {}).get("invocation") or {}).get("visualization_resolved_tables") or {}).get("entries")
        or []
    )
    for entry in entries:
        entry_id = (((entry.get("id") or {}).get("value")) or "")
        if entry_id != visualization_id:
            continue
        table_values = ((((entry.get("tables") or {}).get(table_key)) or {}).get("values")) or []
        if table_values:
            return str(table_values[0])
    return ""


def manifest_table_name(
    run_render_payload: dict[str, Any],
    component_type: str,
    renderer_id: str | None = None,
) -> str:
    entries = (
        (((run_render_payload.get("data") or {}).get("invocation") or {}).get("manifest") or {}).get("entry")
        or []
    )
    for entry in entries:
        if entry.get("component_type") != component_type:
            continue
        entry_renderer_id = (((entry.get("renderer_id") or {}).get("value")) or "")
        if renderer_id is not None and entry_renderer_id != renderer_id:
            continue
        table_name = entry.get("table_name")
        if table_name:
            return str(table_name)
    return ""


def sql_identifier(value: str) -> str:
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", value):
        raise ValueError(f"unsafe SQL identifier from renderer metadata: {value!r}")
    return value


def top_functions_query(drilldown_table: str, measurements_table: str) -> str:
    drilldown_table = sql_identifier(drilldown_table)
    measurements_table = sql_identifier(measurements_table)
    return f"""
WITH per_node AS (
  SELECT
    COALESCE(NULLIF(s.name, ''), 'EMPTY_SYMBOL') AS function_name,
    COALESCE(NULLIF(i.image_name, ''), 'UNKNOWN_IMAGE') AS image_name,
    d.node_type,
    d.call_tree_id,
    MAX(CASE WHEN m.name = 'Periodic Samples (self)' THEN d.measurement_value ELSE 0 END) AS periodic_samples_self,
    MAX(CASE WHEN m.name = 'Periodic Samples (self) - percentage' THEN d.measurement_value ELSE 0 END) AS periodic_samples_self_percent
  FROM {drilldown_table} d
  LEFT JOIN symbols s ON d.symbol_id = s.symbol_id
  LEFT JOIN images i ON s.image_id = i.image_id
  LEFT JOIN {measurements_table} m ON d.measurement_id = m.measurement_id
  GROUP BY d.call_tree_id, s.name, i.image_name, d.node_type
),
agg AS (
  SELECT
    function_name,
    image_name,
    node_type,
    SUM(periodic_samples_self) AS periodic_samples_self,
    SUM(periodic_samples_self_percent) AS periodic_samples_self_percent
  FROM per_node
  GROUP BY function_name, image_name, node_type
)
SELECT
  function_name,
  image_name,
  node_type,
  periodic_samples_self,
  ROUND(periodic_samples_self_percent, 3) AS periodic_samples_self_percent
FROM agg
ORDER BY periodic_samples_self DESC, function_name, image_name, node_type
LIMIT 100
""".strip()


def call_tree_query(drilldown_table: str, measurements_table: str) -> str:
    drilldown_table = sql_identifier(drilldown_table)
    measurements_table = sql_identifier(measurements_table)
    return f"""
WITH per_node AS (
  SELECT
    d.call_tree_id,
    d.call_tree_parent_id,
    d.symbol_id,
    d.node_type,
    MAX(CASE WHEN m.name = 'Periodic Samples (self)' THEN d.measurement_value ELSE NULL END) AS self_samples,
    MAX(CASE WHEN m.name = 'Periodic Samples (total)' THEN d.measurement_value ELSE NULL END) AS total_samples
  FROM {drilldown_table} d
  LEFT JOIN {measurements_table} m ON d.measurement_id = m.measurement_id
  WHERE d.node_type = 'function'
  GROUP BY d.call_tree_id, d.call_tree_parent_id, d.symbol_id, d.node_type
)
SELECT
  p.call_tree_id,
  p.call_tree_parent_id,
  p.symbol_id,
  p.node_type,
  COALESCE(NULLIF(s.name, ''), 'No label') AS label,
  COALESCE(NULLIF(i.image_name, ''), 'UNKNOWN_IMAGE') AS image_name,
  p.self_samples,
  p.total_samples
FROM per_node p
LEFT JOIN symbols s ON p.symbol_id = s.symbol_id
LEFT JOIN images i ON s.image_id = i.image_id
WHERE COALESCE(p.self_samples, 0) > 0
   OR COALESCE(p.total_samples, 0) > 0
   OR p.call_tree_parent_id = -1
ORDER BY COALESCE(p.self_samples, 0) DESC, p.call_tree_id
""".strip()


def disassembly_query(
    disassembly_table: str,
    symbols_table: str,
    images_table: str,
    source_files_table: str,
) -> str:
    disassembly_table = sql_identifier(disassembly_table)
    symbols_table = sql_identifier(symbols_table)
    images_table = sql_identifier(images_table)
    source_files_table = sql_identifier(source_files_table)
    return f"""
SELECT
  d.address,
  d.instruction,
  COALESCE(d.arguments, '') AS arguments,
  d.periodic_samples,
  d.source_file_id,
  COALESCE(sf.target_location, '') AS source_file,
  d.line_no,
  COALESCE(img.image_name, '') AS image_name,
  COALESCE(sym.name, '') AS symbol_name
FROM {disassembly_table} d
LEFT JOIN {symbols_table} sym ON sym.symbol_id = d.symbol_id
LEFT JOIN {images_table} img ON img.image_id = sym.image_id
LEFT JOIN {source_files_table} sf ON sf.source_file_id = d.source_file_id
WHERE d.address IS NOT NULL
  AND d.instruction IS NOT NULL
ORDER BY COALESCE(img.image_name, ''), d.address
""".strip()


def source_relative_path(row: dict[str, Any]) -> str:
    source_file_id = int(row["source_file_id"])
    location = str(
        row.get("target_location")
        or row.get("host_location")
        or f"source_{source_file_id}"
    )
    path = location.replace("\\", "/")
    path = re.sub(r"^[A-Za-z]:", "", path).lstrip("/")
    path = posixpath.normpath(path)
    if path in ("", ".") or path == ".." or path.startswith("../"):
        return f"source_{source_file_id}"
    return path


def source_file_path(source_root: Path, row: dict[str, Any]) -> Path | None:
    rel_path = source_relative_path(row)
    candidate = source_root / rel_path
    if candidate.is_file():
        return candidate
    basename = posixpath.basename(rel_path)
    matches = sorted(path for path in source_root.rglob(basename) if path.is_file())
    return matches[0] if len(matches) == 1 else None


def build_source_contents(
    top_source_lines: list[dict[str, Any]],
    source_root: Path,
) -> tuple[dict[str, Any], list[int]]:
    source_contents: list[dict[str, Any]] = []
    missing_source_file_ids: list[int] = []
    seen_source_file_ids: set[int] = set()

    for row in top_source_lines:
        try:
            source_file_id = int(row["source_file_id"])
        except (KeyError, TypeError, ValueError):
            continue
        if source_file_id in seen_source_file_ids:
            continue
        seen_source_file_ids.add(source_file_id)
        path = source_file_path(source_root, row)
        if path is None:
            missing_source_file_ids.append(source_file_id)
            continue
        source_contents.append(
            {
                "source_file_id": source_file_id,
                "path": row.get("target_location") or row.get("source_path") or str(path),
                "content": path.read_text(encoding="utf-8", errors="replace"),
            }
        )

    return {"source_contents": source_contents}, sorted(set(missing_source_file_ids))


def build_source_hot_windows(
    *,
    top_source_lines: list[dict[str, Any]],
    source_root: Path,
    profile_dir: Path,
    total_profile_samples: int,
) -> tuple[dict[str, Any], dict[str, Any]]:
    top_source_lines_path = profile_dir / "render_query_top_source_lines.json"
    source_contents_path = profile_dir / "source_contents.json"
    source_hot_windows_path = profile_dir / "source_hot_windows.json"
    source_hot_windows_stderr = profile_dir / "source_hot_windows.stderr"

    write_json(top_source_lines_path, {"data": {"rows": top_source_lines}})
    source_contents, source_content_missing_ids = build_source_contents(
        top_source_lines,
        source_root,
    )
    write_json(source_contents_path, source_contents)

    proc = subprocess.run(
        [
            sys.executable,
            str(PATHFINDING_SCRIPTS_DIR / "extract_source_hot_windows.py"),
            "--threshold-percent",
            str(SOURCE_THRESHOLD_PERCENT),
            "--context",
            str(SOURCE_CONTEXT_LINES),
            "--hot-window-threshold-percent",
            str(SOURCE_HOT_WINDOW_THRESHOLD_PERCENT),
            "--hot-window-context",
            str(SOURCE_HOT_WINDOW_CONTEXT_LINES),
            "--total-profile-samples",
            str(total_profile_samples),
            str(top_source_lines_path),
            str(source_contents_path),
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    source_hot_windows_stderr.write_text(proc.stderr, encoding="utf-8")
    if proc.returncode != 0:
        raise RuntimeError(
            "source hot window extraction failed "
            f"(exit={proc.returncode}): {source_hot_windows_stderr}"
        )

    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError("source hot window extraction produced invalid JSON") from exc
    write_json(source_hot_windows_path, payload)

    missing_source_file_ids = sorted(
        set(source_content_missing_ids)
        | set(int(value) for value in payload.get("missing_source_file_ids", []))
    )
    diagnostics = {
        "source_root": str(source_root),
        "source_content_files": len(source_contents.get("source_contents", [])),
        "source_window_files": len(payload.get("files") or []),
        "missing_source_file_ids": missing_source_file_ids,
    }
    return payload, diagnostics


def disassembly_instruction_from_row(row: dict[str, Any]) -> DisassemblyInstruction | None:
    address = int_or_none(row.get("address"))
    if address is None:
        return None
    instruction = str(row.get("instruction") or "")
    if not instruction:
        return None
    return DisassemblyInstruction(
        address=address,
        image_name=str(row.get("image_name") or ""),
        symbol=str(row.get("symbol_name") or ""),
        instruction=instruction,
        arguments=str(row.get("arguments") or ""),
        samples=max(0, int_or_none(row.get("periodic_samples")) or 0),
        source_file_id=int_or_none(row.get("source_file_id")),
        source_file=str(row.get("source_file") or ""),
        line_no=int_or_none(row.get("line_no")),
    )


def load_disassembly_source_lookup(
    instructions: list[DisassemblyInstruction],
    source_root: Path,
) -> dict[tuple[str, int], str]:
    lookup: dict[tuple[str, int], str] = {}
    loaded_paths: dict[str, list[str]] = {}

    for instruction in instructions:
        if not instruction.source_file or instruction.line_no is None:
            continue
        if instruction.source_file not in loaded_paths:
            row = {
                "source_file_id": instruction.source_file_id or 0,
                "target_location": instruction.source_file,
            }
            path = source_file_path(source_root, row)
            if path is None:
                loaded_paths[instruction.source_file] = []
            else:
                loaded_paths[instruction.source_file] = path.read_text(
                    encoding="utf-8",
                    errors="replace",
                ).splitlines()
        lines = loaded_paths[instruction.source_file]
        if 1 <= instruction.line_no <= len(lines):
            lookup[(instruction.source_file, instruction.line_no)] = lines[instruction.line_no - 1]

    return lookup


def select_disassembly_seed_keys(
    instructions: list[DisassemblyInstruction],
    threshold_percent: float,
) -> set[tuple[str, int]]:
    sampled = [instruction for instruction in instructions if instruction.samples > 0]
    total_samples = sum(instruction.samples for instruction in sampled)
    if total_samples <= 0:
        return set()

    target_samples = total_samples * threshold_percent / 100.0
    ordered = sorted(
        sampled,
        key=lambda instruction: (
            -instruction.samples,
            instruction.address,
            instruction.image_name,
        ),
    )

    selected: set[tuple[str, int]] = set()
    running = 0
    start = 0
    while start < len(ordered) and running < target_samples:
        bucket_samples = ordered[start].samples
        end = start + 1
        while end < len(ordered) and ordered[end].samples == bucket_samples:
            end += 1

        bucket = ordered[start:end]
        bucket_total = sum(instruction.samples for instruction in bucket)
        if running + bucket_total > target_samples and running > 0 and len(bucket) > 1:
            break

        for instruction in bucket:
            selected.add((instruction.image_name, instruction.address))
        running += bucket_total
        start = end

    if not selected and ordered:
        hottest_samples = ordered[0].samples
        for instruction in ordered:
            if instruction.samples != hottest_samples:
                break
            selected.add((instruction.image_name, instruction.address))

    return selected


def split_disassembly_segments(
    instructions: list[DisassemblyInstruction],
) -> list[list[DisassemblyInstruction]]:
    if not instructions:
        return []
    segments: list[list[DisassemblyInstruction]] = []
    current = [instructions[0]]
    for instruction in instructions[1:]:
        if instruction.address == current[-1].address + 4:
            current.append(instruction)
        else:
            segments.append(current)
            current = [instruction]
    segments.append(current)
    return segments


def merge_index_windows(windows: list[dict[str, int]]) -> list[dict[str, int]]:
    if not windows:
        return []
    ordered = sorted(windows, key=lambda window: (window["start"], window["end"]))
    merged = [dict(ordered[0])]
    for window in ordered[1:]:
        previous = merged[-1]
        if window["start"] <= previous["end"] + 1:
            previous["end"] = max(previous["end"], window["end"])
        else:
            merged.append(dict(window))
    return merged


def expand_index_windows(
    windows: list[dict[str, int]],
    segment_length: int,
    context: int,
) -> list[dict[str, int]]:
    return merge_index_windows(
        [
            {
                "start": max(0, window["start"] - context),
                "end": min(segment_length - 1, window["end"] + context),
            }
            for window in windows
        ]
    )


def format_disassembly_lines(
    instructions: list[DisassemblyInstruction],
    source_lookup: dict[tuple[str, int], str],
) -> list[str]:
    sample_width = 8
    source_gap = " " * 6
    lines: list[str] = []
    current_symbol = ""
    current_source_file = ""
    current_line_key: tuple[str, int] | None = None

    for instruction in instructions:
        if instruction.symbol and instruction.symbol != current_symbol:
            if lines and lines[-1] != "":
                lines.append("")
            lines.append(f"Symbol: {instruction.symbol}")
            current_symbol = instruction.symbol
            current_source_file = ""
            current_line_key = None

        if instruction.source_file and instruction.source_file != current_source_file:
            lines.append(f"{instruction.source_file}:")
            current_source_file = instruction.source_file
            current_line_key = None

        if instruction.source_file and instruction.line_no is not None:
            line_key = (instruction.source_file, instruction.line_no)
            if line_key != current_line_key:
                source_text = source_lookup.get(line_key, "")
                lines.append(f"{' ' * sample_width}  {instruction.line_no}:{source_gap}{source_text}")
                current_line_key = line_key

        disassembly = instruction.instruction
        if instruction.arguments:
            disassembly += "  " + instruction.arguments
        sample_prefix = f"{instruction.samples:{sample_width}d}" if instruction.samples > 0 else " " * sample_width
        lines.append(f"{sample_prefix}  0x{instruction.address:x}  {disassembly}")

    return lines


def build_disassembly_hot_windows(
    rows: list[dict[str, Any]],
    source_root: Path,
    total_profile_samples: int,
) -> tuple[dict[str, Any], dict[str, Any]]:
    instructions = [
        instruction
        for row in rows
        for instruction in [disassembly_instruction_from_row(row)]
        if instruction is not None
    ]
    instructions.sort(key=lambda instruction: (instruction.image_name, instruction.address))
    seed_keys = select_disassembly_seed_keys(instructions, DISASSEMBLY_THRESHOLD_PERCENT)
    source_lookup = load_disassembly_source_lookup(instructions, source_root)

    images: list[dict[str, Any]] = []
    total_samples_all_images = sum(instruction.samples for instruction in instructions)
    instructions_by_image: dict[str, list[DisassemblyInstruction]] = {}
    for instruction in instructions:
        instructions_by_image.setdefault(instruction.image_name, []).append(instruction)

    hot_window_sample_threshold = 0
    if total_profile_samples > 0 and DISASSEMBLY_HOT_WINDOW_THRESHOLD_PERCENT > 0:
        hot_window_sample_threshold = int(
            total_profile_samples * DISASSEMBLY_HOT_WINDOW_THRESHOLD_PERCENT / 100.0
        )

    for image_name, image_instructions in instructions_by_image.items():
        image_windows: list[dict[str, Any]] = []
        hot_rows = [
            instruction
            for instruction in image_instructions
            if (instruction.image_name, instruction.address) in seed_keys
        ]
        for segment in split_disassembly_segments(image_instructions):
            raw_windows = [
                {
                    "start": max(0, index - DISASSEMBLY_CONTEXT_LINES),
                    "end": min(len(segment) - 1, index + DISASSEMBLY_CONTEXT_LINES),
                }
                for index, instruction in enumerate(segment)
                if (instruction.image_name, instruction.address) in seed_keys
            ]
            merged = merge_index_windows(raw_windows)
            if (
                DISASSEMBLY_HOT_WINDOW_CONTEXT_LINES > DISASSEMBLY_CONTEXT_LINES
                and hot_window_sample_threshold > 0
            ):
                widened_candidates = []
                for window in merged:
                    block = segment[window["start"] : window["end"] + 1]
                    if sum(instruction.samples for instruction in block) >= hot_window_sample_threshold:
                        widened_candidates.append(window)
                if widened_candidates:
                    merged = merge_index_windows(
                        [
                            *merged,
                            *expand_index_windows(
                                widened_candidates,
                                len(segment),
                                DISASSEMBLY_HOT_WINDOW_CONTEXT_LINES - DISASSEMBLY_CONTEXT_LINES,
                            ),
                        ]
                    )

            for window in merged:
                block = segment[window["start"] : window["end"] + 1]
                image_windows.append(
                    {
                        "start_address": f"0x{block[0].address:x}",
                        "end_address": f"0x{block[-1].address:x}",
                        "samples_in_window": sum(instruction.samples for instruction in block),
                        "mixed_disassembly_lines": format_disassembly_lines(block, source_lookup),
                    }
                )

        image_windows.sort(
            key=lambda window: (
                -int(window.get("samples_in_window") or 0),
                str(window.get("start_address") or ""),
            )
        )
        if hot_rows:
            images.append(
                {
                    "image": image_name,
                    "total_samples_in_image": sum(instruction.samples for instruction in image_instructions),
                    "threshold_percent": DISASSEMBLY_THRESHOLD_PERCENT,
                    "hot_window_threshold_percent": DISASSEMBLY_HOT_WINDOW_THRESHOLD_PERCENT,
                    "hot_window_context_lines": DISASSEMBLY_HOT_WINDOW_CONTEXT_LINES,
                    "selected_instruction_count": len(hot_rows),
                    "instruction_count": len(image_instructions),
                    "sampled_instruction_count": len(hot_rows),
                    "windows": image_windows,
                }
            )

    images.sort(
        key=lambda image: (
            -int(image.get("total_samples_in_image") or 0),
            str(image.get("image") or ""),
        )
    )
    payload = {
        "generated_at_utc": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "threshold_percent": DISASSEMBLY_THRESHOLD_PERCENT,
        "context_lines": DISASSEMBLY_CONTEXT_LINES,
        "hot_window_threshold_percent": DISASSEMBLY_HOT_WINDOW_THRESHOLD_PERCENT,
        "hot_window_context_lines": DISASSEMBLY_HOT_WINDOW_CONTEXT_LINES,
        "images": images,
        "total_samples_all_images": total_samples_all_images,
        "image_count": len(images),
    }
    diagnostics = {
        "disassembly_instruction_rows": len(instructions),
        "disassembly_seed_instruction_count": len(seed_keys),
        "disassembly_images": len(images),
        "disassembly_windows": sum(len(image.get("windows") or []) for image in images),
    }
    return payload, diagnostics


def build_call_tree_hot_paths(
    *,
    call_tree_rows: list[dict[str, Any]],
    profile_dir: Path,
) -> tuple[dict[str, Any], dict[str, Any]]:
    call_tree_rows_path = profile_dir / "render_query_call_tree_rows.json"
    call_tree_hot_paths_path = profile_dir / "call_tree_hot_paths.json"
    call_tree_hot_paths_stderr = profile_dir / "call_tree_hot_paths.stderr"

    write_json(call_tree_rows_path, {"data": {"rows": call_tree_rows}})
    proc = subprocess.run(
        [
            sys.executable,
            str(PATHFINDING_SCRIPTS_DIR / "extract_call_tree_hot_paths.py"),
            "--threshold-percent",
            str(CALL_TREE_THRESHOLD_PERCENT),
            str(call_tree_rows_path),
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    call_tree_hot_paths_stderr.write_text(proc.stderr, encoding="utf-8")
    if proc.returncode != 0:
        raise RuntimeError(
            "call tree hot path extraction failed "
            f"(exit={proc.returncode}): {call_tree_hot_paths_stderr}"
        )

    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError("call tree hot path extraction produced invalid JSON") from exc
    write_json(call_tree_hot_paths_path, payload)

    diagnostics = {
        "call_tree_rows": len(call_tree_rows),
        "call_tree_selected_rows": len(payload.get("rows") or []),
        "call_tree_selection_basis": payload.get("selection_basis"),
    }
    return payload, diagnostics


def collect_profile_files(
    *,
    cli_bin: Path,
    run_id: str,
    source_root: Path,
    profile_dir: Path,
) -> dict[str, Any]:
    profile_dir.mkdir(parents=True, exist_ok=True)
    run_info = run_apx_json(cli_bin, ["run", "info", run_id, "--json"])
    session_id, render_payload = render_run(cli_bin, run_id)
    diagnostics: dict[str, Any] = {"render_session_id": session_id}
    try:
        sample_summary_rows = query_render_rows(cli_bin, session_id, SAMPLE_SUMMARY_QUERY)
        top_source_lines = query_render_rows(
            cli_bin,
            session_id,
            top_source_lines_query(SOURCE_THRESHOLD_PERCENT),
        )
        drilldown_table = resolved_table_name(render_payload, "call_stack", "drilldown")
        measurements_table = resolved_table_name(render_payload, "call_stack", "measurements")
        disassembly_table = manifest_table_name(render_payload, "disassembly", "disassembly")
        symbols_table = manifest_table_name(render_payload, "symbols", "streamline_symbols")
        images_table = manifest_table_name(render_payload, "images", "streamline_symbols")
        source_files_table = manifest_table_name(render_payload, "source_files", "streamline_symbols")

        top_functions: list[dict[str, Any]] = []
        call_tree_rows: list[dict[str, Any]] = []
        if drilldown_table and measurements_table:
            top_functions = query_render_rows(
                cli_bin,
                session_id,
                top_functions_query(drilldown_table, measurements_table),
            )
            call_tree_rows = query_render_rows(
                cli_bin,
                session_id,
                call_tree_query(drilldown_table, measurements_table),
            )

        disassembly_rows: list[dict[str, Any]] = []
        if disassembly_table and symbols_table and images_table and source_files_table:
            disassembly_rows = query_render_rows(
                cli_bin,
                session_id,
                disassembly_query(
                    disassembly_table,
                    symbols_table,
                    images_table,
                    source_files_table,
                ),
            )

        total_profile_samples = max(
            (
                int_or_none(row.get("total_periodic_samples")) or 0
                for row in sample_summary_rows
            ),
            default=0,
        )
        _source_windows, source_diagnostics = build_source_hot_windows(
            top_source_lines=top_source_lines,
            source_root=source_root,
            profile_dir=profile_dir,
            total_profile_samples=total_profile_samples,
        )
        _call_tree_hot_paths, call_tree_diagnostics = build_call_tree_hot_paths(
            call_tree_rows=call_tree_rows,
            profile_dir=profile_dir,
        )
        disassembly_windows, disassembly_diagnostics = build_disassembly_hot_windows(
            disassembly_rows,
            source_root,
            total_profile_samples,
        )
        diagnostics.update(source_diagnostics)
        diagnostics.update(call_tree_diagnostics)
        diagnostics.update(disassembly_diagnostics)
        diagnostics.update(
            {
                "sample_summary_rows": len(sample_summary_rows),
                "top_source_line_rows": len(top_source_lines),
                "top_function_rows": len(top_functions),
                "drilldown_table": drilldown_table,
                "measurements_table": measurements_table,
                "disassembly_table": disassembly_table,
                "symbols_table": symbols_table,
                "images_table": images_table,
                "source_files_table": source_files_table,
            }
        )

        write_json(profile_dir / "run_info.json", run_info)
        write_json(
            profile_dir / "render_query_sample_summary.json",
            {"data": {"rows": sample_summary_rows}},
        )
        write_json(
            profile_dir / "render_query_top_functions.json",
            {"data": {"rows": top_functions}},
        )
        write_json(profile_dir / "disassembly_hot_windows.json", disassembly_windows)
        return diagnostics
    finally:
        close_render_session(cli_bin, session_id)


def manifest_for_files(files: list[Path], payload_budget: dict[str, Any]) -> dict[str, Any]:
    return {
        "payload_budget": payload_budget,
        "files": [
            {
                "path": str(path),
                "sha256": sha256_file(path),
                "size_bytes": path.stat().st_size,
            }
            for path in sorted({path for path in files if path.is_file()}, key=lambda path: str(path))
        ],
    }


def build_pathfinding_payload(
    *,
    test_id: str,
    run_dir: Path,
) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any]]:
    payload_module = pathfinding_payload_module()
    prompt_text = PATHFINDING_PROMPT.read_text(encoding="utf-8")
    payload, manifest_files, payload_budget = payload_module.build_profile_payload(
        test_id,
        prompt_text,
        run_dir / "profile",
    )
    manifest = manifest_for_files([PATHFINDING_PROMPT, *manifest_files], payload_budget)
    return payload, manifest, payload_budget


def build_openai_request(payload: dict[str, Any], cfg: dict[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
    request_module = pathfinding_request_module()
    request = request_module.build_request(payload, cfg["model"])
    request_reasoning_effort = str(cfg["reasoning_effort"]).strip()
    if not request_reasoning_effort:
        raise ValueError("cfg['reasoning_effort'] is required")
    request["reasoning"] = {"effort": request_reasoning_effort}
    request_text = compact_json(request)
    request_meta = {
        "model": cfg["model"],
        "configured_reasoning_effort": request_reasoning_effort,
        "request_reasoning_effort": request["reasoning"]["effort"],
        "request_reasoning_effort_sent": "reasoning" in request,
        "request_chars": len(request_text),
        "max_request_chars": request_module.DEFAULT_MAX_REQUEST_CHARS,
        "within_budget": len(request_text) <= request_module.DEFAULT_MAX_REQUEST_CHARS,
    }
    return request, request_meta


def int_value(value: Any) -> int | None:
    return value if isinstance(value, int) else None


def normalize_usage(payload: dict[str, Any]) -> list[dict[str, Any]]:
    usage = payload.get("usage")
    if not isinstance(usage, dict):
        return []
    input_details = usage.get("input_tokens_details") if isinstance(usage.get("input_tokens_details"), dict) else {}
    output_details = usage.get("output_tokens_details") if isinstance(usage.get("output_tokens_details"), dict) else {}
    return [
        {
            "input_tokens": int_value(usage.get("input_tokens")),
            "cached_input_tokens": int_value(input_details.get("cached_tokens")),
            "output_tokens": int_value(usage.get("output_tokens")),
            "reasoning_output_tokens": int_value(output_details.get("reasoning_tokens")),
            "total_tokens": int_value(usage.get("total_tokens")),
            "raw": usage,
        }
    ]


def iter_json_objects(value: Any):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from iter_json_objects(child)
    elif isinstance(value, list):
        for child in value:
            yield from iter_json_objects(child)


def extract_openai_response_text(payload: dict[str, Any]) -> str:
    fragments: list[str] = []
    output = payload.get("output")
    if isinstance(output, list):
        for item in output:
            if not isinstance(item, dict) or item.get("type") != "message":
                continue
            content = item.get("content")
            if not isinstance(content, list):
                continue
            for entry in content:
                if (
                    isinstance(entry, dict)
                    and entry.get("type") == "output_text"
                    and isinstance(entry.get("text"), str)
                ):
                    fragments.append(entry["text"])

    for obj in iter_json_objects(payload):
        if obj.get("type") == "summary_text" and isinstance(obj.get("text"), str):
            fragments.append(obj["text"])
    for obj in iter_json_objects(payload):
        if obj.get("type") == "refusal" and isinstance(obj.get("refusal"), str):
            fragments.append(obj["refusal"])

    return "\n".join(fragment for fragment in fragments if fragment)


def response_error_details(payload: dict[str, Any]) -> tuple[str, str] | None:
    error = payload.get("error")
    if error is None and "error-category" not in payload:
        return None
    if isinstance(error, dict):
        message = str(error.get("message") or "unknown API error")
        code = str(error.get("code") or "unknown_code")
    elif isinstance(error, str):
        message = error
        code = str(payload.get("error-category") or "unknown_code")
    else:
        message = "unknown API error"
        code = str(payload.get("error-category") or "unknown_code")
    return code, message


def format_response_headers(response: requests.Response) -> str:
    lines = [f"HTTP {response.status_code} {response.reason}"]
    lines.extend(f"{key}: {value}" for key, value in response.headers.items())
    return "\n".join(lines) + "\n"


def openai_session() -> requests.Session:
    retry = Retry(
        total=OPENAI_RESPONSE_MAX_RETRIES,
        connect=OPENAI_RESPONSE_MAX_RETRIES,
        read=OPENAI_RESPONSE_MAX_RETRIES,
        status=OPENAI_RESPONSE_MAX_RETRIES,
        status_forcelist=RETRYABLE_HTTP_STATUS,
        allowed_methods=frozenset({"POST"}),
        backoff_factor=OPENAI_RETRY_BACKOFF_FACTOR,
        respect_retry_after_header=True,
        raise_on_status=False,
    )
    adapter = HTTPAdapter(max_retries=retry)
    session = requests.Session()
    session.mount("https://", adapter)
    return session


def write_invoke_log(log_path: Path, stdout: str, stderr: str) -> None:
    log_path.write_text(
        "\n".join(
            [
                "STDOUT:",
                stdout,
                "",
                "STDERR:",
                stderr,
            ]
        ),
        encoding="utf-8",
    )


def invoke_openai_responses(
    *,
    request: dict[str, Any],
    request_meta: dict[str, Any],
    output_dir: Path,
    cfg: dict[str, Any],
) -> dict[str, Any]:
    log_path = output_dir / "invoke_openai.log"
    request_json = output_dir / "openai_request.json"
    request_meta_json = output_dir / "openai_request_meta.json"
    raw_response_json = output_dir / "openai_response_raw.json"
    response_headers_txt = output_dir / "openai_response_headers.txt"
    response_status_txt = output_dir / "openai_http_status.txt"
    response_md = output_dir / "llm_response.md"
    api_base = os.environ.get("OPENAI_API_BASE", DEFAULT_API_BASE).rstrip("/")
    url = f"{api_base}/responses"
    stdout_lines = [f"running: POST {url} # model={cfg['model']}"]
    request_text = compact_json(request)

    request_json.write_text(request_text, encoding="utf-8")
    write_json(request_meta_json, request_meta)

    try:
        with openai_session() as session:
            response = session.post(
                url,
                headers={
                    "Authorization": f"Bearer {cfg['openai_api_key']}",
                    "Content-Type": "application/json",
                },
                data=request_text.encode("utf-8"),
                timeout=OPENAI_RESPONSE_TIMEOUT_SECONDS,
            )
    except requests.RequestException as exc:
        write_invoke_log(log_path, "\n".join(stdout_lines), str(exc))
        raise RuntimeError(
            f"OpenAI API request failed: {exc}. Request={request_json}"
        ) from exc

    raw_response_json.write_text(response.text, encoding="utf-8")
    response_headers_txt.write_text(format_response_headers(response), encoding="utf-8")
    response_status_txt.write_text(f"{response.status_code}\n", encoding="utf-8")

    try:
        payload = response.json()
    except ValueError as exc:
        write_invoke_log(log_path, "\n".join(stdout_lines), "")
        raise RuntimeError(
            f"OpenAI API response was not valid JSON. Request={request_json} "
            f"Response={raw_response_json} Headers={response_headers_txt}"
        ) from exc
    if not isinstance(payload, dict):
        write_invoke_log(log_path, "\n".join(stdout_lines), "")
        raise RuntimeError(f"OpenAI API response was not a JSON object: {raw_response_json}")

    error_details = response_error_details(payload)
    if error_details is not None:
        err_code, err_msg = error_details
        write_invoke_log(log_path, "\n".join(stdout_lines), err_msg)
        raise RuntimeError(
            f"OpenAI API request failed (http={response.status_code}, {err_code}): "
            f"{err_msg}. Request={request_json} Response={raw_response_json} "
            f"Headers={response_headers_txt}"
        )
    if not 200 <= response.status_code < 300:
        write_invoke_log(log_path, "\n".join(stdout_lines), "")
        raise RuntimeError(
            f"OpenAI API request returned non-success HTTP status {response.status_code}. "
            f"Request={request_json} Response={raw_response_json} Headers={response_headers_txt}"
        )

    response_text = extract_openai_response_text(payload)
    response_md.write_text(response_text, encoding="utf-8")
    if not response_text:
        write_invoke_log(log_path, "\n".join(stdout_lines), "")
        raise RuntimeError(f"OpenAI API response did not contain model text. See {raw_response_json}")

    stdout_lines.append(str(response_md))
    write_invoke_log(log_path, "\n".join(stdout_lines), "")
    return payload


def invoke_rest_mode(
    attempt_dir: Path,
    run_meta: dict[str, Any],
    cfg: dict[str, Any],
) -> dict[str, Any]:
    started_at_utc = timestamp_utc()
    started = time.perf_counter()
    run_dir = attempt_dir / "pathfinding_run"
    rest_payload_dir = run_dir / "modes" / "rest"
    rest_payload_dir.mkdir(parents=True, exist_ok=True)

    profile_diagnostics = collect_profile_files(
        cli_bin=cfg["cli_bin"],
        run_id=run_meta["run_id"],
        source_root=Path(run_meta["source_root"]),
        profile_dir=run_dir / "profile",
    )
    payload, manifest, payload_budget = build_pathfinding_payload(
        test_id=run_meta["test_id"],
        run_dir=run_dir,
    )
    payload_path = rest_payload_dir / "input.json"
    manifest_path = rest_payload_dir / "manifest.json"
    raw_response_path = attempt_dir / "openai_response_raw.json"
    response_md = attempt_dir / "llm_response.md"

    write_json(payload_path, payload)
    write_json(manifest_path, manifest)
    request, request_meta = build_openai_request(payload, cfg)

    if not request_meta["within_budget"]:
        raise RuntimeError(
            "REST request exceeds pathfinding preflight budget: "
            f"{request_meta['request_chars']} > {request_meta['max_request_chars']}"
        )

    LOGGER.info("Invoking REST mode for %s, run %s", run_meta["test_id"], run_meta["run_id"])
    response = invoke_openai_responses(
        request=request,
        request_meta=request_meta,
        output_dir=attempt_dir,
        cfg=cfg,
    )
    if not response_md.is_file() or response_md.stat().st_size == 0:
        raise RuntimeError(f"OpenAI API response did not contain model text: {raw_response_path}")

    duration_seconds = time.perf_counter() - started
    return {
        "mode": REST_MODE,
        "status": "ok",
        "model": cfg["model"],
        "reasoning_effort": cfg["reasoning_effort"],
        "request_reasoning_effort": request_meta["request_reasoning_effort"],
        "request_reasoning_effort_sent": request_meta["request_reasoning_effort_sent"],
        "duration_seconds": duration_seconds,
        "duration_display": f"{duration_seconds:.1f}s",
        "started_at_utc": started_at_utc,
        "finished_at_utc": timestamp_utc(),
        "pathfinding_rest_source": str(PATHFINDING_REST_DIR / "SOURCE.md"),
        "profile_diagnostics": profile_diagnostics,
        "payload_budget": payload_budget,
        "token_usage": normalize_usage(response),
        "payload_path": str(payload_path),
        "manifest_path": str(manifest_path),
        "request_path": str(attempt_dir / "openai_request.json"),
        "request_meta_path": str(attempt_dir / "openai_request_meta.json"),
        "response_json": str(raw_response_path),
        "response_headers": str(attempt_dir / "openai_response_headers.txt"),
        "response_status": str(attempt_dir / "openai_http_status.txt"),
        "invoke_log": str(attempt_dir / "invoke_openai.log"),
        "response_markdown": str(response_md),
        "response_bytes": response_md.stat().st_size,
    }
