#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Build the REST payload for one testcase run.

Pipeline stage:
    Called after profile extraction, before REST invocation.

Important inputs:
    The extracted profile artefacts for a run and the selected prompt template.

Important outputs:
    ``modes/rest/input.json`` and ``modes/rest/manifest.json``.

Purpose:
    Payload construction is a core part of the prototype. The model can only
    reason over the evidence that survives this stage, so budgeting and
    allowlisting need to be explicit and reviewable.

Typical failures:
    Missing extracted artefacts, payload budget overruns, or attempts to
    include non-allowlisted content.
"""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# gpt-5-mini has a 400k-token context window. The harness budgets in serialized
# request bytes instead because it builds JSON requests containing rendered
# profiling data. For recent profiling-heavy REST requests we measured about
# 2.7-4.2 request bytes per input token, with about 3.2 bytes/token typical, so
# we set a budget of ~50% of the limit - a 400,000-byte budget corresponds to
# roughly 95k-148k input tokens.
# This should give us a decent-sized amount of profiling data, but still
# well within the models' limit.
PROFILE_PAYLOAD_BUDGET_CHARS = 400_000
REQUEST_PREFLIGHT_BUDGET_CHARS = 400_000
VARIABLE_SECTION_PERCENTAGES: tuple[tuple[str, float], ...] = (
    ("top_functions", 0.10),
    ("call_tree_hot_paths", 0.15),
    ("source_hot_windows_json", 0.30),
    ("disassembly_hot_windows_json", 0.45),
)
PROFILE_INPUT_NAMES = (
    "run_info.json",
    "render_query_sample_summary.json",
    "render_query_top_functions.json",
    "source_hot_windows.json",
    "disassembly_hot_windows.json",
    "call_tree_hot_paths.json",
)


class PayloadBuildError(RuntimeError):
    """Raised when the payload builder cannot produce a valid payload."""


def timestamp_utc() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def read_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def compact_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def compact_chars(value: Any) -> int:
    return len(compact_json(value))


def clean_number(value: Any) -> Any:
    try:
        if value is None:
            return None
        number = float(value)
    except (TypeError, ValueError):
        return value
    if abs(number - round(number)) < 1e-9:
        return int(round(number))
    return number


def safe_ratio(numerator: float | int | None, denominator: float | int | None) -> float | None:
    if not isinstance(numerator, (int, float)) or not isinstance(denominator, (int, float)):
        return None
    if denominator <= 0:
        return None
    return float(numerator) / float(denominator)


def build_prompt(prompt_text: str, *, profiled: bool) -> str:
    if not profiled:
        return prompt_text
    return (
        prompt_text
        + "\n\nNotes:\n"
        + "- Large profiling sections may be trimmed to fit the payload budget.\n"
        + "- In structured profiling JSON, omitted optional fields mean unknown, empty, or not applicable.\n"
        + "- Source and disassembly windows include adjacent unsampled context around sampled hotspots.\n"
    )


def _require_dict(value: Any, context: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise PayloadBuildError(f"{context} must be a JSON object")
    return value


def _require_list(value: Any, context: str) -> list[Any]:
    if not isinstance(value, list):
        raise PayloadBuildError(f"{context} must be a JSON array")
    return value


def _require_present(mapping: dict[str, Any], key: str, context: str) -> Any:
    if key not in mapping:
        raise PayloadBuildError(f"{context} is missing required field '{key}'")
    return mapping[key]


def profile_inputs(profile_dir: Path) -> dict[str, Path]:
    # Profile-backed payloads are strict: if extraction missed one of the core
    # JSON artifacts, the payload is incomplete and the caller should rerun the
    # upstream extract step rather than guess.
    inputs = {name: profile_dir / name for name in PROFILE_INPUT_NAMES}
    missing = [str(path) for path in inputs.values() if not path.is_file()]
    if missing:
        raise PayloadBuildError(f"missing profiling inputs: {', '.join(missing)}")
    return inputs


def trim_call_tree_hot_paths(payload: Any) -> Any:
    payload = _require_dict(payload, "call_tree_hot_paths input")

    rows = payload.get("rows")
    if isinstance(rows, list):
        return {
            "created_at_utc": payload.get("created_at_utc"),
            "measurement": payload.get("measurement"),
            "selection_basis": payload.get("selection_basis"),
            "threshold_percent": payload.get("threshold_percent"),
            "total_samples": payload.get("total_samples"),
            "selected_node_count": payload.get("selected_node_count"),
            "node_count": payload.get("node_count"),
            "rows": rows,
        }

    def flatten_legacy_roots(roots: list[dict[str, Any]]) -> list[dict[str, Any]]:
        flattened: list[dict[str, Any]] = []

        def visit(node: dict[str, Any], depth: int) -> None:
            flattened.append(
                {
                    "id": node.get("call_tree_id"),
                    "parent_id": (
                        None
                        if node.get("call_tree_parent_id") in (-1, 0, None)
                        else node.get("call_tree_parent_id")
                    ),
                    "depth": depth,
                    "label": node.get("label"),
                    "image": node.get("image_name"),
                    "self_samples": node.get("self_samples"),
                    "total_samples": node.get("total_samples"),
                }
            )
            for child in node.get("children", []) or []:
                visit(child, depth + 1)

        for root in roots:
            visit(root, 0)
        return flattened

    nodes = payload.get("nodes")
    if not isinstance(nodes, list):
        nodes = flatten_legacy_roots(payload.get("roots", []) or [])

    return {
        "created_at_utc": payload.get("created_at_utc"),
        "measurement": payload.get("measurement"),
        "selection_basis": payload.get("selection_basis"),
        "threshold_percent": payload.get("threshold_percent"),
        "total_samples": payload.get("total_samples"),
        "selected_node_count": payload.get("selected_node_count"),
        "node_count": payload.get("node_count"),
        "rows": nodes,
    }


def simplify_top_functions(payload: Any) -> list[dict[str, Any]]:
    payload = _require_dict(payload, "render_query_top_functions.json")
    data = payload.get("data")
    if isinstance(data, dict):
        rows = _require_list(data.get("rows"), "render_query_top_functions.json rows")
    else:
        rows = _require_list(payload.get("rows"), "render_query_top_functions.json rows")

    simplified = []
    for index, row in enumerate(rows):
        row = _require_dict(row, f"render_query_top_functions.json row {index}")
        simplified.append(
            {
                "function": _require_present(row, "function_name", f"render_query_top_functions.json row {index}"),
                "image": _require_present(row, "image_name", f"render_query_top_functions.json row {index}"),
                "node_type": _require_present(row, "node_type", f"render_query_top_functions.json row {index}"),
                "self_samples": _require_present(row, "periodic_samples_self", f"render_query_top_functions.json row {index}"),
                "self_percent": _require_present(
                    row,
                    "periodic_samples_self_percent",
                    f"render_query_top_functions.json row {index}",
                ),
            }
        )
    return simplified


def simplify_run_info(payload: Any) -> dict[str, Any]:
    data = payload.get("data") if isinstance(payload, dict) else {}
    if not isinstance(data, dict):
        return {}

    cpu_rows = ((data.get("target_info_cpus") or {}).get("chunk")) or []
    cpu_groups: dict[tuple[Any, Any], dict[str, Any]] = {}
    for row in cpu_rows:
        if not isinstance(row, dict):
            continue
        key = (row.get("name"), row.get("midr"))
        entry = cpu_groups.setdefault(
            key,
            {
                "name": row.get("name"),
                "midr": row.get("midr"),
                "core_count": 0,
                "cluster_ids": set(),
            },
        )
        entry["core_count"] += 1
        cluster_id = row.get("cluster_id")
        if cluster_id is not None:
            entry["cluster_ids"].add(cluster_id)

    cpu_types = []
    for entry in cpu_groups.values():
        cpu_types.append(
            {
                "name": entry["name"],
                "midr": entry["midr"],
                "core_count": entry["core_count"],
                "cluster_ids": sorted(entry["cluster_ids"]),
            }
        )
    cpu_types.sort(key=lambda item: (-int(item.get("core_count") or 0), str(item.get("name") or "")))

    os_rows = ((data.get("target_info_os") or {}).get("chunk")) or []
    os_info = os_rows[0] if os_rows and isinstance(os_rows[0], dict) else {}

    return {
        "run_id": data.get("id"),
        "run_name": data.get("name"),
        "recipe_name": data.get("recipe_name"),
        "run_result": data.get("run_result"),
        "run_error": data.get("run_error"),
        "workload_type": data.get("workload_type"),
        "workload_cmdline": data.get("cmdline"),
        "start_time": data.get("start_time"),
        "end_time": data.get("end_time"),
        "timeout": data.get("timeout"),
        "target_name": data.get("target_name"),
        "sampling_parameters": data.get("parameters"),
        "os": {
            "description": os_info.get("os_description"),
            "kernel_version": os_info.get("kernel_version"),
        },
        "cpu_topology": {
            "total_cpu_count": len(cpu_rows),
            "cpu_types": cpu_types,
        },
    }


def trim_source_hot_windows(payload: Any) -> Any:
    payload = _require_dict(payload, "source_hot_windows.json")
    return {
        "created_at_utc": payload.get("created_at_utc"),
        "threshold_percent": payload.get("threshold_percent"),
        "context_lines": payload.get("context_lines"),
        "total_source_line_samples": payload.get("total_source_line_samples"),
        "selected_source_line_count": payload.get("selected_source_line_count"),
        "missing_source_file_ids": payload.get("missing_source_file_ids"),
        "files": [
            {
                "source_file_id": entry.get("source_file_id"),
                "path": _require_present(entry, "path", f"source_hot_windows.json file {file_index}"),
                "total_samples_in_file": entry.get("total_samples_in_file"),
                "window_count": entry.get("window_count", len(windows)),
                "windows": [
                    {
                        "window_start": _require_present(
                            window,
                            "window_start",
                            f"source_hot_windows.json file {file_index} window {window_index}",
                        ),
                        "window_end": _require_present(
                            window,
                            "window_end",
                            f"source_hot_windows.json file {file_index} window {window_index}",
                        ),
                        "samples_in_window": _require_present(
                            window,
                            "samples_in_window",
                            f"source_hot_windows.json file {file_index} window {window_index}",
                        ),
                        "source_lines": _require_list(
                            _require_present(
                                window,
                                "source_lines",
                                f"source_hot_windows.json file {file_index} window {window_index}",
                            ),
                            f"source_hot_windows.json file {file_index} window {window_index} source_lines",
                        ),
                    }
                    for window_index, window in enumerate(windows)
                ],
            }
            for file_index, entry in enumerate(_require_list(payload.get("files", []), "source_hot_windows.json files"))
            for windows in [_require_list(entry.get("windows", []), f"source_hot_windows.json file {file_index} windows")]
        ],
    }


def trim_disassembly_hot_windows(payload: Any) -> Any:
    payload = _require_dict(payload, "disassembly_hot_windows.json")
    return {
        "generated_at_utc": payload.get("generated_at_utc"),
        "threshold_percent": payload.get("threshold_percent"),
        "context": payload.get("context"),
        "total_samples_all_images": payload.get("total_samples_all_images"),
        "image_count": payload.get("image_count"),
        "images": [
            {
                "image": _require_present(image, "image", f"disassembly_hot_windows.json image {image_index}"),
                "total_samples_in_image": image.get("total_samples_in_image"),
                "threshold_percent": image.get("threshold_percent"),
                "selected_instruction_count": image.get("selected_instruction_count"),
                "instruction_count": image.get("instruction_count"),
                "sampled_instruction_count": image.get("sampled_instruction_count"),
                "window_count": len(windows),
                "windows": [
                    {
                        "start_address": _require_present(
                            window,
                            "start_address",
                            f"disassembly_hot_windows.json image {image_index} window {window_index}",
                        ),
                        "end_address": _require_present(
                            window,
                            "end_address",
                            f"disassembly_hot_windows.json image {image_index} window {window_index}",
                        ),
                        "samples_in_window": _require_present(
                            window,
                            "samples_in_window",
                            f"disassembly_hot_windows.json image {image_index} window {window_index}",
                        ),
                        "mixed_disassembly_lines": _require_list(
                            _require_present(
                                window,
                                "mixed_disassembly_lines",
                                f"disassembly_hot_windows.json image {image_index} window {window_index}",
                            ),
                            f"disassembly_hot_windows.json image {image_index} window {window_index} mixed_disassembly_lines",
                        ),
                    }
                    for window_index, window in enumerate(windows)
                ],
            }
            for image_index, image in enumerate(_require_list(payload.get("images", []), "disassembly_hot_windows.json images"))
            for windows in [_require_list(image.get("windows", []), f"disassembly_hot_windows.json image {image_index} windows")]
        ],
    }


def allocate_percentage_budgets(total_budget: int) -> dict[str, int]:
    """Split the variable payload budget using the configured percentages.

    The last section receives any remainder so the quotas always add up to the
    requested total. This keeps later reporting and truncation decisions easy
    to explain.
    """
    budgets: dict[str, int] = {}
    remaining = max(0, int(total_budget))
    entries = list(VARIABLE_SECTION_PERCENTAGES)
    for index, (name, fraction) in enumerate(entries):
        if index == len(entries) - 1:
            budget = remaining
        else:
            budget = int(total_budget * fraction)
            budget = min(budget, remaining)
        budgets[name] = max(0, budget)
        remaining -= budgets[name]
    return budgets


def truncate_list_by_budget(rows: list[dict[str, Any]], budget_chars: int) -> list[dict[str, Any]]:
    """Keep a prefix of rows that still fits within the character budget.

    The caller is expected to sort rows into the preferred order first. This
    helper preserves that order and stops at the first row that would exceed
    the budget.
    """
    if budget_chars <= 0:
        return []
    kept: list[dict[str, Any]] = []
    for row in rows:
        candidate = kept + [row]
        if compact_chars(candidate) > budget_chars:
            break
        kept = candidate
    return kept


def budget_top_functions(rows: list[dict[str, Any]], budget_chars: int) -> list[dict[str, Any]]:
    ordered = sorted(
        rows,
        key=lambda row: (
            -float(row.get("self_samples") or 0),
            str(row.get("function") or ""),
            str(row.get("image") or ""),
        ),
    )
    return truncate_list_by_budget(ordered, budget_chars)


def budget_call_tree_hot_paths(payload: dict[str, Any], budget_chars: int) -> dict[str, Any]:
    if budget_chars <= 0 or not isinstance(payload, dict):
        rows: list[dict[str, Any]] = []
    else:
        rows = truncate_list_by_budget(list(payload.get("rows") or []), budget_chars)
    result = dict(payload)
    result["rows"] = rows
    result["selected_node_count"] = len(rows)
    return result


def budget_source_hot_windows(payload: dict[str, Any], budget_chars: int) -> dict[str, Any]:
    if not isinstance(payload, dict):
        return payload
    result = {
        "created_at_utc": payload.get("created_at_utc"),
        "threshold_percent": payload.get("threshold_percent"),
        "context_lines": payload.get("context_lines"),
        "total_source_line_samples": payload.get("total_source_line_samples"),
        "selected_source_line_count": payload.get("selected_source_line_count"),
        "missing_source_file_ids": payload.get("missing_source_file_ids"),
        "files": [],
    }
    if budget_chars <= 0:
        return result
    files = sorted(
        list(payload.get("files") or []),
        key=lambda entry: (
            -float(entry.get("total_samples_in_file") or 0),
            str(entry.get("path") or ""),
        ),
    )
    for file_entry in files:
        candidate_file = {
            "source_file_id": file_entry.get("source_file_id"),
            "path": file_entry.get("path"),
            "total_samples_in_file": file_entry.get("total_samples_in_file"),
            "window_count": 0,
            "windows": [],
        }
        for window in file_entry.get("windows") or []:
            window_entry = {
                "window_start": window.get("window_start"),
                "window_end": window.get("window_end"),
                "samples_in_window": window.get("samples_in_window"),
                "source_lines": window.get("source_lines"),
            }
            candidate_file["windows"].append(window_entry)
            candidate_file["window_count"] = len(candidate_file["windows"])
            candidate_payload = {**result, "files": [*result["files"], candidate_file]}
            if compact_chars(candidate_payload) > budget_chars:
                candidate_file["windows"].pop()
                candidate_file["window_count"] = len(candidate_file["windows"])
                break
        if candidate_file["windows"]:
            result["files"].append(candidate_file)
    return result


def budget_disassembly_hot_windows(payload: dict[str, Any], budget_chars: int) -> dict[str, Any]:
    if not isinstance(payload, dict):
        return payload
    result = {
        "generated_at_utc": payload.get("generated_at_utc"),
        "threshold_percent": payload.get("threshold_percent"),
        "context": payload.get("context"),
        "total_samples_all_images": payload.get("total_samples_all_images"),
        "image_count": payload.get("image_count"),
        "images": [],
    }
    if budget_chars <= 0:
        return result
    images = sorted(
        list(payload.get("images") or []),
        key=lambda image: (
            -float(image.get("total_samples_in_image") or 0),
            str(image.get("image") or ""),
        ),
    )
    for image in images:
        candidate_image = {
            "image": image.get("image"),
            "total_samples_in_image": image.get("total_samples_in_image"),
            "threshold_percent": image.get("threshold_percent"),
            "selected_instruction_count": image.get("selected_instruction_count"),
            "instruction_count": image.get("instruction_count"),
            "sampled_instruction_count": image.get("sampled_instruction_count"),
            "window_count": 0,
            "windows": [],
        }
        windows = sorted(
            list(image.get("windows") or []),
            key=lambda window: (
                -float(window.get("samples_in_window") or 0),
                str(window.get("start_address") or ""),
            ),
        )
        for window in windows:
            window_entry = {
                "start_address": window.get("start_address"),
                "end_address": window.get("end_address"),
                "samples_in_window": window.get("samples_in_window"),
                "mixed_disassembly_lines": window.get("mixed_disassembly_lines"),
            }
            candidate_image["windows"].append(window_entry)
            candidate_image["window_count"] = len(candidate_image["windows"])
            candidate_payload = {**result, "images": [*result["images"], candidate_image]}
            if compact_chars(candidate_payload) > budget_chars:
                candidate_image["windows"].pop()
                candidate_image["window_count"] = len(candidate_image["windows"])
                break
        if candidate_image["windows"]:
            result["images"].append(candidate_image)
    return result


def top_functions_coverage(rows: list[dict[str, Any]], kept: list[dict[str, Any]]) -> float | None:
    """Report how much of the original top-function sample mass was retained."""
    total = sum(float(row.get("self_samples") or 0) for row in rows)
    emitted = sum(float(row.get("self_samples") or 0) for row in kept)
    return safe_ratio(emitted, total)


def call_tree_coverage(payload: dict[str, Any], kept_payload: dict[str, Any]) -> float | None:
    """Report retained call-tree coverage using self samples from emitted rows."""
    total_samples = payload.get("total_samples")
    emitted = sum(float(row.get("self_samples") or 0) for row in (kept_payload.get("rows") or []))
    return safe_ratio(emitted, total_samples)


def disassembly_coverage(payload: dict[str, Any], kept_payload: dict[str, Any]) -> float | None:
    """Report retained disassembly coverage using emitted window sample counts."""
    total = payload.get("total_samples_all_images")
    emitted = 0.0
    for image in kept_payload.get("images") or []:
        for window in image.get("windows") or []:
            emitted += float(window.get("samples_in_window") or 0)
    return safe_ratio(emitted, total)


def section_report(
    *,
    name: str,
    original_value: Any,
    emitted_value: Any,
    quota_chars: int | None,
    fixed: bool,
    original_items: int | None = None,
    emitted_items: int | None = None,
    coverage_fraction: float | None = None,
) -> dict[str, Any]:
    original_chars = compact_chars(original_value)
    emitted_chars = compact_chars(emitted_value)
    retained_fraction = safe_ratio(emitted_chars, original_chars)
    return {
        "name": name,
        "fixed": fixed,
        "quota_chars": quota_chars,
        "original_chars": original_chars,
        "emitted_chars": emitted_chars,
        "original_items": original_items,
        "emitted_items": emitted_items,
        "retained_fraction": retained_fraction,
        "coverage_fraction": coverage_fraction,
        "trimmed": emitted_chars < original_chars,
    }


def budget_profiling_sections(
    *,
    summary: dict[str, Any],
    run_info: dict[str, Any],
    top_functions: list[dict[str, Any]],
    call_tree_hot_paths: dict[str, Any],
    source_hot_windows_json: dict[str, Any],
    disassembly_hot_windows_json: dict[str, Any],
) -> tuple[dict[str, Any], dict[str, Any]]:
    """Trim profiling sections so the final payload stays within budget.

    The summary and run metadata are treated as fixed inputs because later
    stages rely on them for context and debugging. The larger evidence sections
    are reduced independently, then the function returns both the emitted
    sections and a report that explains what was removed.
    """
    fixed_chars = compact_chars(summary) + compact_chars(run_info)
    remaining_budget = max(0, PROFILE_PAYLOAD_BUDGET_CHARS - fixed_chars)
    quotas = allocate_percentage_budgets(remaining_budget)

    top_functions_kept = budget_top_functions(top_functions, quotas["top_functions"])
    call_tree_kept = budget_call_tree_hot_paths(call_tree_hot_paths, quotas["call_tree_hot_paths"])
    source_kept = budget_source_hot_windows(source_hot_windows_json, quotas["source_hot_windows_json"])
    disassembly_kept = budget_disassembly_hot_windows(
        disassembly_hot_windows_json,
        quotas["disassembly_hot_windows_json"],
    )

    emitted_sections = {
        "summary": summary,
        "run_info": run_info,
        "top_functions": top_functions_kept,
        "call_tree_hot_paths": call_tree_kept,
        "source_hot_windows_json": source_kept,
        "disassembly_hot_windows_json": disassembly_kept,
    }
    reports = {
        "summary": section_report(
            name="summary",
            original_value=summary,
            emitted_value=summary,
            quota_chars=None,
            fixed=True,
            original_items=1,
            emitted_items=1,
        ),
        "run_info": section_report(
            name="run_info",
            original_value=run_info,
            emitted_value=run_info,
            quota_chars=None,
            fixed=True,
            original_items=1,
            emitted_items=1,
        ),
        "top_functions": section_report(
            name="top_functions",
            original_value=top_functions,
            emitted_value=top_functions_kept,
            quota_chars=quotas["top_functions"],
            fixed=False,
            original_items=len(top_functions),
            emitted_items=len(top_functions_kept),
            coverage_fraction=top_functions_coverage(top_functions, top_functions_kept),
        ),
        "call_tree_hot_paths": section_report(
            name="call_tree_hot_paths",
            original_value=call_tree_hot_paths,
            emitted_value=call_tree_kept,
            quota_chars=quotas["call_tree_hot_paths"],
            fixed=False,
            original_items=len(call_tree_hot_paths.get("rows") or []),
            emitted_items=len(call_tree_kept.get("rows") or []),
            coverage_fraction=call_tree_coverage(call_tree_hot_paths, call_tree_kept),
        ),
        "source_hot_windows_json": section_report(
            name="source_hot_windows_json",
            original_value=source_hot_windows_json,
            emitted_value=source_kept,
            quota_chars=quotas["source_hot_windows_json"],
            fixed=False,
            original_items=sum(len(file_entry.get("windows") or []) for file_entry in (source_hot_windows_json.get("files") or [])),
            emitted_items=sum(len(file_entry.get("windows") or []) for file_entry in (source_kept.get("files") or [])),
            coverage_fraction=None,
        ),
        "disassembly_hot_windows_json": section_report(
            name="disassembly_hot_windows_json",
            original_value=disassembly_hot_windows_json,
            emitted_value=disassembly_kept,
            quota_chars=quotas["disassembly_hot_windows_json"],
            fixed=False,
            original_items=sum(len(image.get("windows") or []) for image in (disassembly_hot_windows_json.get("images") or [])),
            emitted_items=sum(len(image.get("windows") or []) for image in (disassembly_kept.get("images") or [])),
            coverage_fraction=disassembly_coverage(disassembly_hot_windows_json, disassembly_kept),
        ),
    }
    budget_report = {
        "profiling_budget_chars": PROFILE_PAYLOAD_BUDGET_CHARS,
        "request_preflight_budget_chars": REQUEST_PREFLIGHT_BUDGET_CHARS,
        "fixed_section_chars": fixed_chars,
        "variable_budget_chars": remaining_budget,
        "variable_section_quotas": quotas,
        "sections": reports,
        "profiling_original_chars": sum(report["original_chars"] for report in reports.values()),
        "profiling_emitted_chars": sum(report["emitted_chars"] for report in reports.values()),
    }
    return emitted_sections, budget_report


def total_periodic_samples(sample_summary: Any) -> Any:
    if not isinstance(sample_summary, dict):
        return None
    rows = (((sample_summary.get("data") or {}).get("rows")) or [])
    if not rows:
        return None
    return clean_number(rows[0].get("total_periodic_samples"))


def build_profile_summary(test_id: str, created_at_utc: str, profiling: dict[str, Any]) -> dict[str, Any]:
    call_tree = profiling.get("call_tree_hot_paths") or {}
    return {
        "test_case_id": test_id,
        "created_at_utc": created_at_utc,
        "total_periodic_samples": profiling.get("total_periodic_samples"),
        "top_function_rows": len(profiling.get("top_functions") or []),
        "source_window_files": len((profiling.get("source_hot_windows_json") or {}).get("files", [])),
        "disassembly_images": len((profiling.get("disassembly_hot_windows_json") or {}).get("images", [])),
        "call_tree_nodes": call_tree.get("node_count", len(call_tree.get("rows", []))),
    }


def add_source_window_rendering(source_hot_windows: dict[str, Any]) -> dict[str, Any]:
    return source_hot_windows


def add_disassembly_rendering(disassembly_hot_windows: dict[str, Any]) -> dict[str, Any]:
    return disassembly_hot_windows


def build_profile_payload(
    test_id: str, prompt_text: str, profile_dir: Path
) -> tuple[dict[str, Any], list[Path], dict[str, Any]]:
    """Build the profiled REST payload from extracted Performix artefacts.

    This is the point where the broad extraction output is collapsed into the
    smaller allowlisted schema that the REST path exposes to the model. The
    function also applies the payload budget so later stages only see the
    bounded, normalised form.
    """
    inputs = profile_inputs(profile_dir)
    sample_summary_json = read_json(inputs["render_query_sample_summary.json"])
    created_at_utc = timestamp_utc()
    total_samples = total_periodic_samples(sample_summary_json)
    top_functions_payload = read_json(inputs["render_query_top_functions.json"])
    source_hot_windows_payload = read_json(inputs["source_hot_windows.json"])
    disassembly_hot_windows_payload = read_json(inputs["disassembly_hot_windows.json"])
    call_tree_input_payload = read_json(inputs["call_tree_hot_paths.json"])
    original_sections = {
        "run_info": simplify_run_info(read_json(inputs["run_info.json"])),
        "top_functions": simplify_top_functions(top_functions_payload),
        "source_hot_windows_json": add_source_window_rendering(
            trim_source_hot_windows(source_hot_windows_payload)
        ),
        "disassembly_hot_windows_json": add_disassembly_rendering(
            trim_disassembly_hot_windows(disassembly_hot_windows_payload)
        ),
    }
    call_tree_hot_paths = trim_call_tree_hot_paths(call_tree_input_payload)
    original_profiling = {
        "total_periodic_samples": total_samples,
        "summary": None,
        "run_info": original_sections["run_info"],
        "call_tree_hot_paths": call_tree_hot_paths,
        "top_functions": original_sections["top_functions"],
        "source_hot_windows_json": original_sections["source_hot_windows_json"],
        "disassembly_hot_windows_json": original_sections["disassembly_hot_windows_json"],
    }
    summary = build_profile_summary(test_id, created_at_utc, original_profiling)
    emitted_sections, budget_report = budget_profiling_sections(
        summary=summary,
        run_info=original_sections["run_info"],
        top_functions=original_sections["top_functions"],
        call_tree_hot_paths=call_tree_hot_paths,
        source_hot_windows_json=original_sections["source_hot_windows_json"],
        disassembly_hot_windows_json=original_sections["disassembly_hot_windows_json"],
    )
    profiling = {
        "total_periodic_samples": total_samples,
        "summary": emitted_sections["summary"],
        "run_info": emitted_sections["run_info"],
        "call_tree_hot_paths": emitted_sections["call_tree_hot_paths"],
        "top_functions": emitted_sections["top_functions"],
        "source_hot_windows_json": emitted_sections["source_hot_windows_json"],
        "disassembly_hot_windows_json": emitted_sections["disassembly_hot_windows_json"],
    }
    payload = {
        "test_case_id": test_id,
        "created_at_utc": created_at_utc,
        "prompt": build_prompt(prompt_text, profiled=True),
        "profiling": profiling,
    }
    budget_report["prompt_chars"] = len(payload["prompt"])
    budget_report["payload_compact_chars"] = compact_chars(payload)
    return payload, list(inputs.values()), budget_report
