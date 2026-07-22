# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List
from zipfile import ZipFile, BadZipFile
import json
import sys
import traceback


__all__ = [
    "ResultPayload",
    "ResultsSet",
    "eprintln",
    "ensure_unique_dir",
    "safe_extract_zip",
    "parse_payloads",
    "extract_and_parse_zip",
]


def eprintln(msg: str) -> None:
    print(msg, file=sys.stderr)


@dataclass(frozen=True)
class ResultPayload:
    results_root: Path
    metadata: Dict[str, Any]
    reports: Dict[str, Any]
    sysreport: Dict[str, Any]


@dataclass
class ResultsSet:
    benchmarking_run_id: str
    results: List[ResultPayload]
    errors: List[str]


def ensure_unique_dir(base: Path) -> Path:
    if not base.exists():
        return base
    i = 1
    while True:
        cand = base.parent / f"{base.name}_{i}"
        if not cand.exists():
            return cand
        i += 1


def safe_extract_zip(zip_path: Path, dest_dir: Path) -> None:
    dest_dir.mkdir(parents=True, exist_ok=True)
    try:
        with ZipFile(zip_path, "r") as zf:
            for member in zf.infolist():
                out_path = (dest_dir / member.filename).resolve()
                if not str(out_path).startswith(str(dest_dir.resolve())):
                    raise RuntimeError(
                        f"Blocked path traversal in zip: {member.filename}"
                    )
            zf.extractall(dest_dir)
    except BadZipFile as e:
        raise RuntimeError(f"Bad zip file: {zip_path}") from e


def parse_payloads(search_root: Path) -> List[ResultPayload]:
    payloads: List[ResultPayload] = []
    for meta_path in search_root.rglob("output/metadata.json"):
        try:
            results_root = meta_path.parent  # .../output
            with meta_path.open("r", encoding="utf-8") as f:
                metadata_obj = json.load(f)

            # Parse optional sysreport.txt located alongside metadata.json
            def parse_sysreport_file(path: Path) -> Dict[str, Any]:
                info: Dict[str, Any] = {}
                if not path.exists():
                    return info
                try:
                    with path.open("r", encoding="utf-8") as sf:
                        current_section: str | None = None
                        for line in sf:
                            s = line.strip()
                            if not s:
                                continue
                            if s in ("System hardware:", "OS configuration:"):
                                current_section = s[:-1]  # drop trailing colon
                                continue
                            if current_section == "System hardware" and s.startswith("CPU types:"):
                                info["cpu_types"] = s.split(":", 1)[1].strip()
                            elif current_section == "OS configuration" and s.startswith("Distribution:"):
                                info["distribution"] = s.split(":", 1)[1].strip()
                except Exception as ex:
                    eprintln(f"[WARN] Failed to parse sysreport: {path} ({ex})")
                return info

            sysreport_path = results_root / "sysreport.txt"
            sysreport_obj = parse_sysreport_file(sysreport_path)

            reports_dir = results_root / "reports"
            reports: Dict[str, Any] = {}
            if reports_dir.is_dir():
                for rf in sorted(reports_dir.glob("*.json")):
                    try:
                        with rf.open("r", encoding="utf-8") as f:
                            reports[rf.name] = json.load(f)
                    except Exception as ex:
                        eprintln(f"[WARN] Failed to parse report JSON: {rf} ({ex})")

            payloads.append(
                ResultPayload(
                    results_root=results_root,
                    metadata=metadata_obj,
                    reports=reports,
                    sysreport=sysreport_obj,
                )
            )
        except Exception as ex:
            eprintln(f"[WARN] Failed to parse payload under {meta_path}: {ex}")
            traceback.print_exc(file=sys.stderr)
    return payloads


def extract_and_parse_zip(zip_path: Path, work_dir: Path) -> ResultsSet:
    """Extract a top-level results zip and parse contained payloads.

    Replicates the script's logic for one artifact zip: safely extracts the
    outer zip, discovers and extracts any inner `results_*.zip`, and parses
    `metadata.json` + `reports/*.json` into `ResultPayload`.
    """
    results_set = ResultsSet(
        benchmarking_run_id=zip_path.name,
        results=[],
        errors=[],
    )

    try:
        outer_extract_dest = ensure_unique_dir(work_dir / zip_path.stem)
        safe_extract_zip(zip_path, outer_extract_dest)

        # Find inner results_*.zip and extract each (e.g. results_engine_overhead.zip)
        inner_zips = sorted(outer_extract_dest.rglob("results_*.zip"))
        if inner_zips:
            for iz in inner_zips:
                try:
                    inner_extract_dest = ensure_unique_dir(iz.parent / iz.stem)
                    safe_extract_zip(iz, inner_extract_dest)
                    results_set.results.extend(parse_payloads(inner_extract_dest))
                except Exception as ex_inner:
                    msg = f"Failed to extract/parse inner zip {iz}: {ex_inner}"
                    eprintln(f"[WARN] {msg}")
                    results_set.errors.append(msg)
        else:
            # Some CI artifacts package directories directly in the top-level zip.
            # Fall back to parsing payloads from the outer extracted directory.
            parsed = parse_payloads(outer_extract_dest)
            if parsed:
                results_set.results.extend(parsed)
            else:
                msg = f"No inner results_*.zip found and no payloads parsed in {outer_extract_dest}"
                eprintln(f"[WARN] {msg}")
                results_set.errors.append(msg)

    except Exception as ex:
        msg = f"Failed processing artifact zip {zip_path}: {ex}"
        eprintln(f"[ERROR] {msg}")
        traceback.print_exc(file=sys.stderr)
        results_set.errors.append(msg)

    return results_set
