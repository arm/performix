#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Wrap a harness payload in the OpenAI Responses API shape."""

from __future__ import annotations

import json
from typing import Any

DEFAULT_MAX_REQUEST_CHARS = 400_000


def require_object(value: Any, context: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"{context} must be a JSON object")
    return value


def require_array(value: Any, context: str) -> list[Any]:
    if not isinstance(value, list):
        raise ValueError(f"{context} must be a JSON array")
    return value


def require_field(mapping: dict[str, Any], key: str, context: str) -> Any:
    if key not in mapping:
        raise ValueError(f"{context} is missing required field '{key}'")
    return mapping[key]


def require_text(value: Any, field_name: str) -> str:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{field_name} must be a non-empty string")
    return value


def add_block(content: list[dict[str, str]], text: str) -> None:
    if text:
        content.append({"type": "input_text", "text": text})


def compact_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def percent_of_total(samples: Any, total_samples: Any) -> str:
    if not isinstance(samples, (int, float)) or not isinstance(total_samples, (int, float)) or total_samples <= 0:
        return "unknown"
    return f"{(float(samples) / float(total_samples)) * 100.0:.2f}%"


def nonblank_rendered_lines(lines: Any) -> list[str]:
    return [str(line) for line in lines or [] if str(line).strip()]


def render_source_entries(entries: list[dict[str, Any]]) -> list[str]:
    return [
        f"Source file: {entry['path']}\n{entry['content']}"
        for entry in entries
        if isinstance(entry.get("path"), str) and isinstance(entry.get("content"), str)
    ]




def render_source_windows(source_hot_windows: dict[str, Any], total_samples: Any) -> list[str]:
    blocks: list[str] = []
    for file_index, file_entry in enumerate(require_array(source_hot_windows.get("files", []), "profiling.source_hot_windows_json.files")):
        file_entry = require_object(file_entry, f"profiling.source_hot_windows_json.files[{file_index}]")
        path = require_field(file_entry, "path", f"profiling.source_hot_windows_json.files[{file_index}]")
        windows = require_array(
            file_entry.get("windows", []),
            f"profiling.source_hot_windows_json.files[{file_index}].windows",
        )
        for window_index, window in enumerate(windows):
            window = require_object(
                window,
                f"profiling.source_hot_windows_json.files[{file_index}].windows[{window_index}]",
            )
            start_line = require_field(
                window,
                "window_start",
                f"profiling.source_hot_windows_json.files[{file_index}].windows[{window_index}]",
            )
            end_line = require_field(
                window,
                "window_end",
                f"profiling.source_hot_windows_json.files[{file_index}].windows[{window_index}]",
            )
            samples = require_field(
                window,
                "samples_in_window",
                f"profiling.source_hot_windows_json.files[{file_index}].windows[{window_index}]",
            )
            lines = nonblank_rendered_lines(
                require_array(
                    require_field(
                        window,
                        "source_lines",
                        f"profiling.source_hot_windows_json.files[{file_index}].windows[{window_index}]",
                    ),
                    f"profiling.source_hot_windows_json.files[{file_index}].windows[{window_index}].source_lines",
                )
            )
            if not lines:
                continue
            header = f"Source hot window for file: {path}"
            header += f" | lines={start_line}..{end_line}"
            header += (
                f" | samples_in_window={samples}"
                f" ({percent_of_total(samples, total_samples)} of total_periodic_samples)"
            )
            blocks.append(
                "\n".join(
                    [
                        header,
                        "# Layout: source as 'samples  line: text'",
                        *lines,
                    ]
                )
            )
    return blocks


def render_disassembly_images(disassembly_hot_windows: dict[str, Any], total_capture_samples: Any) -> list[str]:
    blocks: list[str] = []
    for image_index, image in enumerate(require_array(disassembly_hot_windows.get("images", []), "profiling.disassembly_hot_windows_json.images")):
        image = require_object(image, f"profiling.disassembly_hot_windows_json.images[{image_index}]")
        image_name = require_field(image, "image", f"profiling.disassembly_hot_windows_json.images[{image_index}]")
        total_samples_in_image = image.get("total_samples_in_image")
        windows = require_array(
            image.get("windows", []),
            f"profiling.disassembly_hot_windows_json.images[{image_index}].windows",
        )
        for window_index, window in enumerate(windows):
            window = require_object(
                window,
                f"profiling.disassembly_hot_windows_json.images[{image_index}].windows[{window_index}]",
            )
            lines = nonblank_rendered_lines(
                require_array(
                    require_field(
                        window,
                        "mixed_disassembly_lines",
                        f"profiling.disassembly_hot_windows_json.images[{image_index}].windows[{window_index}]",
                    ),
                    f"profiling.disassembly_hot_windows_json.images[{image_index}].windows[{window_index}].mixed_disassembly_lines",
                )
            )
            if not lines:
                continue
            header = f"Disassembly hot window for image: {image_name}"
            if total_samples_in_image is not None:
                header += (
                    f" | total_samples_in_image={total_samples_in_image}"
                    f" ({percent_of_total(total_samples_in_image, total_capture_samples)} of total_periodic_samples)"
                )
            start_address = require_field(
                window,
                "start_address",
                f"profiling.disassembly_hot_windows_json.images[{image_index}].windows[{window_index}]",
            )
            end_address = require_field(
                window,
                "end_address",
                f"profiling.disassembly_hot_windows_json.images[{image_index}].windows[{window_index}]",
            )
            header += f" | window={start_address}..{end_address}"
            samples_in_window = require_field(
                window,
                "samples_in_window",
                f"profiling.disassembly_hot_windows_json.images[{image_index}].windows[{window_index}]",
            )
            header += (
                f" | samples_in_window={samples_in_window}"
                f" ({percent_of_total(samples_in_window, total_capture_samples)} of total_periodic_samples)"
            )
            blocks.append(
                "\n".join(
                    [
                        header,
                        "# Layout: source as 'samples  line: text'; instruction as 'samples  address  disassembly'",
                        *lines,
                    ]
                )
            )
    return blocks


def build_request(payload: dict[str, Any], model: str) -> dict[str, Any]:
    prompt = require_text(payload.get("prompt"), "prompt")
    content = [{"type": "input_text", "text": prompt}]
    profiling = payload.get("profiling")
    if isinstance(profiling, dict):
        summary = profiling.get("summary")
        total_periodic_samples = None
        if summary is not None:
            summary = require_object(summary, "profiling.summary")
            require_field(summary, "total_periodic_samples", "profiling.summary")
            add_block(content, "Payload summary:\n" + compact_json(summary))
            total_periodic_samples = summary.get("total_periodic_samples")
        run_info = profiling.get("run_info")
        if run_info is not None:
            add_block(content, "profiling.run_info:\n" + compact_json(run_info))
        top_functions = profiling.get("top_functions")
        if top_functions is not None:
            for index, row in enumerate(require_array(top_functions, "profiling.top_functions")):
                row = require_object(row, f"profiling.top_functions[{index}]")
                for key in ("function", "image", "node_type", "self_samples", "self_percent"):
                    require_field(row, key, f"profiling.top_functions[{index}]")
            add_block(content, "profiling.top_functions:\n" + compact_json(top_functions))
        call_tree = profiling.get("call_tree_hot_paths") or {}
        if call_tree:
            call_tree = require_object(call_tree, "profiling.call_tree_hot_paths")
            for index, row in enumerate(require_array(call_tree.get("rows", []), "profiling.call_tree_hot_paths.rows")):
                row = require_object(row, f"profiling.call_tree_hot_paths.rows[{index}]")
                for key in ("id", "depth", "label", "self_samples", "total_samples"):
                    require_field(row, key, f"profiling.call_tree_hot_paths.rows[{index}]")
            add_block(content, "profiling.call_tree_hot_paths:\n" + compact_json(call_tree))
        for block in render_source_windows(require_object(profiling.get("source_hot_windows_json") or {}, "profiling.source_hot_windows_json"), total_periodic_samples):
            add_block(content, block)
        for block in render_disassembly_images(
            require_object(profiling.get("disassembly_hot_windows_json") or {}, "profiling.disassembly_hot_windows_json"),
            total_periodic_samples,
        ):
            add_block(content, block)

    sources = payload.get("sources")
    if isinstance(sources, list):
        for block in render_source_entries(sources):
            add_block(content, block)
    return {
        "model": model,
        "input": [{"role": "user", "content": content}],
    }
