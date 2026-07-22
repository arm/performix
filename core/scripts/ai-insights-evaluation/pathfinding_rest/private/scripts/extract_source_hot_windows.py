#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Parse rendered top-source-line JSON and emit merged hot-source windows.
# Used to add source-level context to the LLM payload without passing full files.
"""Extract compact hot-source windows from rendered top source-line JSON.

The script:
1. Loads render_query_top_source_lines.json.
2. Uses fetched source contents keyed by source_file_id.
3. Keeps source lines until cumulative periodic samples reach threshold-percent.
4. Expands each selected line to +/- context source lines.
5. Widens very dominant windows to preserve nearby context where useful.
6. Merges overlapping windows per file and emits compact JSON.

The source-line query may include both caller and inlined-callee views of the
same sampled work. The payload also includes disassembly hot windows, which are
the better tie-breaker when exact inline attribution matters.

Example:
  python3 extract_source_hot_windows.py \
    --threshold-percent 99 \
    --context 10 \
    --hot-window-threshold-percent 5 \
    --hot-window-context 100 \
    --total-profile-samples 12345 \
    /tmp/render_query_top_source_lines.json \
    /tmp/source_contents.json
"""

from __future__ import annotations

import argparse
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Dict, List, Optional, Tuple


@dataclass
class HotLine:
    source_file_id: int
    source_path: str
    line_no: int
    function_name: str
    inlined: bool
    periodic_samples: int


def _parse_int(value: object) -> Optional[int]:
    if value is None:
        return None
    text = str(value).strip()
    if not text:
        return None
    try:
        return int(float(text))
    except ValueError:
        return None


def _load_rows(path: str) -> List[HotLine]:
    with open(path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)

    rows = payload.get("data", {}).get("rows", [])
    hot_lines: List[HotLine] = []
    for row in rows:
        source_file_id = _parse_int(row.get("source_file_id"))
        line_no = _parse_int(row.get("line_no"))
        periodic_samples = _parse_int(row.get("periodic_samples")) or 0
        source_path = (row.get("target_location") or row.get("source_path") or "").strip()
        if (
            source_file_id is None
            or not source_path
            or line_no is None
            or line_no <= 0
            or periodic_samples <= 0
        ):
            continue
        hot_lines.append(
            HotLine(
                source_file_id=source_file_id,
                source_path=source_path,
                line_no=line_no,
                function_name=(row.get("function_name") or "").strip(),
                inlined=((row.get("inlined") or "").strip() == "I"),
                periodic_samples=periodic_samples,
            )
        )
    hot_lines.sort(key=lambda item: item.periodic_samples, reverse=True)
    return hot_lines


def _merge_windows(windows: List[Tuple[int, int]]) -> List[Tuple[int, int]]:
    if not windows:
        return []
    windows = sorted(windows)
    merged = [windows[0]]
    for start, end in windows[1:]:
        last_start, last_end = merged[-1]
        if start <= last_end + 1:
            merged[-1] = (last_start, max(last_end, end))
        else:
            merged.append((start, end))
    return merged


def _window_samples(
    start: int,
    end: int,
    file_hot_lines_by_line: Dict[int, HotLine],
) -> int:
    return sum(
        item.periodic_samples
        for line_no, item in file_hot_lines_by_line.items()
        if start <= line_no <= end
    )


def _select_hot_lines(rows: List[HotLine], threshold_percent: float) -> List[HotLine]:
    total_samples = sum(row.periodic_samples for row in rows)
    if total_samples <= 0:
        return []
    target = total_samples * (threshold_percent / 100.0)

    buckets: List[List[HotLine]] = []
    current_sample_value: Optional[int] = None
    current_rows: List[HotLine] = []
    for row in rows:
        if current_sample_value is None or row.periodic_samples != current_sample_value:
            if current_rows:
                buckets.append(current_rows)
            current_sample_value = row.periodic_samples
            current_rows = [row]
        else:
            current_rows.append(row)
    if current_rows:
        buckets.append(current_rows)

    selected: List[HotLine] = []
    running = 0
    for bucket_rows in buckets:
        bucket_total = sum(row.periodic_samples for row in bucket_rows)
        if running >= target:
            break
        if running + bucket_total > target and running > 0 and len(bucket_rows) > 1:
            break
        selected.extend(bucket_rows)
        running += bucket_total
    return selected


def _format_source_lines(
    start: int,
    end: int,
    source_lines: List[str],
    file_hot_lines_by_line: Dict[int, HotLine],
) -> List[str]:
    sample_width = 8
    current_function = ""
    rendered: List[str] = []
    for line_no in range(start, end + 1):
        hot_line = file_hot_lines_by_line.get(line_no)
        function_name = hot_line.function_name if hot_line is not None else ""
        if function_name and function_name != current_function:
            if rendered and rendered[-1] != "":
                rendered.append("")
            rendered.append(f"Symbol: {function_name}")
            current_function = function_name

        sample_field = " " * sample_width
        if hot_line is not None:
            sample_field = f"{hot_line.periodic_samples:>{sample_width}d}"
        inlined_note = "  [inlined]" if hot_line is not None and hot_line.inlined else ""
        rendered.append(f"{sample_field}  {line_no:>6d}:      {source_lines[line_no - 1]}{inlined_note}")
    return rendered


def _load_source_contents(path: str) -> Dict[int, Dict[str, str]]:
    with open(path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)
    contents = payload.get("source_contents", [])
    return {
        int(item["source_file_id"]): {
            "path": item.get("path", ""),
            "content": item.get("content", ""),
        }
        for item in contents
        if item.get("source_file_id") is not None
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--threshold-percent", type=float, default=99.0)
    parser.add_argument("--context", type=int, default=10)
    parser.add_argument("--hot-window-threshold-percent", type=float, default=5.0)
    parser.add_argument("--hot-window-context", type=int, default=100)
    parser.add_argument("--total-profile-samples", type=int, default=0)
    parser.add_argument("source_lines_json", help="render_query_top_source_lines.json input file")
    parser.add_argument("source_contents_json", help="Source contents fetched via load_source_content")
    args = parser.parse_args()

    hot_lines = _load_rows(args.source_lines_json)
    source_contents_by_id = _load_source_contents(args.source_contents_json)
    total_samples = sum(item.periodic_samples for item in hot_lines)
    selected_lines = _select_hot_lines(hot_lines, args.threshold_percent)

    windows_by_file: Dict[int, List[Tuple[int, int]]] = {}
    hot_lines_by_file: Dict[int, List[HotLine]] = {}
    for item in selected_lines:
        windows_by_file.setdefault(item.source_file_id, []).append(
            (max(1, item.line_no - args.context), item.line_no + args.context)
        )
        hot_lines_by_file.setdefault(item.source_file_id, []).append(item)

    files_out: List[Dict[str, object]] = []
    missing_source_file_ids: List[int] = []

    for source_file_id, windows in sorted(windows_by_file.items()):
        source_meta = source_contents_by_id.get(source_file_id)
        if source_meta is None:
            missing_source_file_ids.append(source_file_id)
            continue

        source_path = hot_lines_by_file[source_file_id][0].source_path
        source_lines = source_meta.get("content", "").splitlines()
        file_hot_lines = hot_lines_by_file[source_file_id]
        file_hot_lines_by_line = {item.line_no: item for item in file_hot_lines}
        merged = _merge_windows(windows)

        # Most hot regions only need compact context. Very dominant windows
        # get a wider expansion so nearby comments, #ifdefs, and helper code
        # are visible without inflating every window in the profile.
        hot_window_sample_threshold = 0.0
        if args.total_profile_samples > 0:
            hot_window_sample_threshold = (
                args.total_profile_samples * (args.hot_window_threshold_percent / 100.0)
            )
        widened_windows: List[Tuple[int, int]] = []
        for start, end in merged:
            samples_in_window = _window_samples(start, end, file_hot_lines_by_line)
            if samples_in_window >= hot_window_sample_threshold > 0:
                extra_context = max(args.hot_window_context - args.context, 0)
                widened_windows.append((max(1, start - extra_context), end + extra_context))
            else:
                widened_windows.append((start, end))
        merged = _merge_windows(widened_windows)

        windows_out: List[Dict[str, object]] = []
        for start, end in merged:
            capped_end = min(end, len(source_lines))
            if start > capped_end:
                continue
            samples_in_window = _window_samples(start, capped_end, file_hot_lines_by_line)
            source_lines_out = _format_source_lines(
                start=start,
                end=capped_end,
                source_lines=source_lines,
                file_hot_lines_by_line=file_hot_lines_by_line,
            )
            windows_out.append(
                {
                    "window_start": start,
                    "window_end": capped_end,
                    "samples_in_window": samples_in_window,
                    "source_lines": source_lines_out,
                }
            )

        files_out.append(
            {
                "source_file_id": source_file_id,
                "path": source_path,
                "total_samples_in_file": sum(item.periodic_samples for item in file_hot_lines),
                "window_count": len(windows_out),
                "windows": windows_out,
            }
        )

    files_out.sort(
        key=lambda item: (
            -int(item.get("total_samples_in_file", 0)),
            str(item.get("path", "")),
        )
    )

    payload = {
        "created_at_utc": datetime.now(timezone.utc).isoformat(),
        "threshold_percent": args.threshold_percent,
        "context_lines": args.context,
        "hot_window_threshold_percent": args.hot_window_threshold_percent,
        "hot_window_context_lines": args.hot_window_context,
        "total_profile_samples": args.total_profile_samples,
        "total_source_line_samples": total_samples,
        "selected_source_line_count": len(selected_lines),
        "missing_source_file_ids": sorted(set(missing_source_file_ids)),
        "files": files_out,
    }
    print(json.dumps(payload, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
