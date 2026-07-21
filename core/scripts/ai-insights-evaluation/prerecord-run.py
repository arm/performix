#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Pre-record a Performix run artefact for one AI Insights testcase."""

from __future__ import annotations

import argparse
import json
import posixpath
import re
import shutil
import sys
import tempfile
import zipfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

sys.path.append(str(Path(__file__).resolve().parents[1]))
from run_export_helper import (
    CommandFailure,
    export_run,
    get_cli_version,
    parse_recipe_run_id,
    run_cli,
    sha256_file,
)


HARNESS_DIR = Path(__file__).resolve().parent
DEFAULT_MANIFEST = HARNESS_DIR / "ai_insights_evaluation.json"

SAMPLED_SOURCE_FILES_QUERY = """
SELECT
  p.source_file_id,
  COALESCE(sf.target_location, '') AS target_location,
  COALESCE(sf.host_location, '') AS host_location,
  SUM(p.periodic_samples) AS periodic_samples
FROM periodic_samples p
LEFT JOIN source_files sf ON sf.source_file_id = p.source_file_id
WHERE p.source_file_id IS NOT NULL
  AND p.periodic_samples > 0
GROUP BY p.source_file_id, sf.target_location, sf.host_location
ORDER BY periodic_samples DESC, p.source_file_id
"""


def load_testcase_config(manifest_path: Path, case_id: str) -> dict[str, Any]:
    """Load the manifest entry for the testcase being pre-recorded."""

    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ValueError(f"manifest not found: {manifest_path}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"manifest is not valid JSON: {manifest_path}") from exc

    for test_case in manifest.get("tests", []):
        if test_case.get("id") == case_id:
            return test_case
    raise ValueError(f"testcase {case_id!r} was not found in {manifest_path}")


def recipe_params_from_config(test_case: dict[str, Any]) -> list[str]:
    """Return recipe parameters from the testcase manifest entry."""

    recipe_params = test_case.get("recipe_params", [])
    if not isinstance(recipe_params, list) or not all(
        isinstance(param, str) for param in recipe_params
    ):
        raise ValueError(
            f"recipe_params for testcase {test_case.get('id')!r} must be a list of strings"
        )
    return list(recipe_params)


def render_run(cli_bin: Path, run_id: str) -> str:
    """Render a run and return the render session id."""

    process = run_cli([str(cli_bin), "run", "render", run_id, "--json"], cli_bin.parent)
    try:
        return json.loads(process.stdout)["data"]["invocation"]["session_id"]
    except (json.JSONDecodeError, KeyError, TypeError) as exc:
        raise ValueError(f"could not parse render session id for run {run_id}") from exc


def close_render_session(cli_bin: Path, session_id: str) -> None:
    """Close a render session, ignoring failures during cleanup."""

    try:
        run_cli([str(cli_bin), "render", "close", session_id], cli_bin.parent)
    except CommandFailure as exc:
        print(f"warning: failed to close render session {session_id}: {exc}", file=sys.stderr)


def query_render_rows(cli_bin: Path, session_id: str, query: str) -> list[dict[str, Any]]:
    """Run a render SQL query and return JSON object rows."""

    process = run_cli(
        [str(cli_bin), "render", "query", session_id, query, "--json"],
        cli_bin.parent,
    )
    try:
        rows = json.loads(process.stdout)["data"]["rows"]
    except (json.JSONDecodeError, KeyError, TypeError) as exc:
        raise ValueError(f"could not parse render query rows for session {session_id}") from exc
    if not isinstance(rows, list):
        raise ValueError(
            f"render query rows are not a list for session {session_id}: rows={rows!r}"
        )
    return rows


def print_run_info(cli_bin: Path, run_id: str) -> None:
    """Print run metadata after profiling, before source sidecar collection."""

    process = run_cli([str(cli_bin), "run", "info", run_id, "--json"], cli_bin.parent)
    print(f"===== apx run info {run_id} --json =====")
    print(process.stdout.rstrip())
    if process.stderr:
        print("===== apx run info stderr =====", file=sys.stderr)
        print(process.stderr.rstrip(), file=sys.stderr)
    print("===== end apx run info =====")


def sql_string(value: str) -> str:
    """Return a single-quoted SQL string literal."""

    return "'" + value.replace("'", "''") + "'"


def source_content_query(run_id: str, source_file_id: int) -> str:
    """Build the SQL query used to fetch a source file body from a run."""

    return f"SELECT load_source_content({sql_string(run_id)}, {source_file_id}) AS content"


def safe_source_relative_path(row: dict[str, Any], used_paths: set[str]) -> str:
    """Map a source_files row to a safe path beneath test_src."""

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
        path = f"source_{source_file_id}"

    candidate = path
    if candidate in used_paths:
        stem = posixpath.basename(path) or f"source_{source_file_id}"
        candidate = posixpath.join(f"source_{source_file_id}", stem)
    used_paths.add(candidate)
    return candidate


def write_source_archive(source_dir: Path, archive: Path) -> None:
    """Create a zip archive containing the test_src directory."""

    if archive.exists():
        archive.unlink()
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as archive_zip:
        for path in sorted(source_dir.rglob("*")):
            archive_zip.write(path, path.relative_to(source_dir.parent))


def collect_sampled_sources(
    cli_bin: Path,
    run_id: str,
    case_dir: Path,
) -> Path:
    """Fetch sampled source files from the rendered run into test_src.zip.

    The prerecording job runs while the profiling target and its checkout still
    exist. Fetching through load_source_content at this point keeps the later AI
    Insights evaluation independent of any source paths on the pytest runner.
    """

    source_dir = case_dir / "test_src"
    source_archive = case_dir / "test_src.zip"
    if source_dir.exists():
        shutil.rmtree(source_dir)
    source_dir.mkdir(parents=True)

    session_id = render_run(cli_bin, run_id)
    fetched: list[dict[str, Any]] = []
    failed: list[dict[str, Any]] = []
    sampled_rows: list[dict[str, Any]] = []
    try:
        sampled_rows = query_render_rows(cli_bin, session_id, SAMPLED_SOURCE_FILES_QUERY)
        used_paths: set[str] = set()
        for row in sampled_rows:
            source_file_id = int(row["source_file_id"])
            try:
                content_rows = query_render_rows(
                    cli_bin,
                    session_id,
                    source_content_query(run_id, source_file_id),
                )
                content = content_rows[0].get("content") if content_rows else None
                if content is None:
                    raise ValueError("load_source_content returned no content")

                rel_path = safe_source_relative_path(row, used_paths)
                output_path = source_dir / rel_path
                output_path.parent.mkdir(parents=True, exist_ok=True)
                output_path.write_text(str(content), encoding="utf-8")
                fetched.append(
                    {
                        "source_file_id": source_file_id,
                        "target_location": row.get("target_location") or "",
                        "host_location": row.get("host_location") or "",
                        "periodic_samples": row.get("periodic_samples"),
                        "relative_path": rel_path,
                        "size_bytes": output_path.stat().st_size,
                    }
                )
            except Exception as exc:
                failed.append(
                    {
                        "source_file_id": source_file_id,
                        "target_location": row.get("target_location") or "",
                        "host_location": row.get("host_location") or "",
                        "periodic_samples": row.get("periodic_samples"),
                        "error": str(exc),
                    }
                )
    finally:
        close_render_session(cli_bin, session_id)

    manifest = {
        "run_id": run_id,
        "sampled_source_count": len(fetched) + len(failed),
        "fetched_source_count": len(fetched),
        "failed_source_count": len(failed),
        "fetched": fetched,
        "failed": failed,
    }
    (source_dir / "manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    if not sampled_rows:
        raise RuntimeError(f"no sampled source files found for run {run_id}")
    if not fetched:
        raise RuntimeError(f"could not fetch any sampled source files for run {run_id}")
    write_source_archive(source_dir, source_archive)
    return source_archive


def parse_args() -> argparse.Namespace:
    """Parse command-line arguments for a single pre-recording run."""

    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    parser.add_argument("--cli-bin", required=True, type=Path)
    parser.add_argument("--target", required=True)
    parser.add_argument("--recipe", required=True)
    parser.add_argument("--workload-cmd", required=True)
    parser.add_argument("--source-root", type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument(
        "--artifactory-run-base",
        help="Optional Artifactory base path used to record published run locations.",
    )
    parser.add_argument(
        "--manifest",
        default=DEFAULT_MANIFEST,
        type=Path,
        help="AI Insights evaluation manifest containing per-testcase recipe parameters.",
    )
    parser.add_argument(
        "--param",
        action="append",
        default=[],
        help="Recipe parameter in NAME=VALUE form. May be supplied more than once.",
    )
    return parser.parse_args()


def main() -> int:
    """Run the selected recipe, export the run, and write testcase metadata."""

    args = parse_args()
    cli_bin = args.cli_bin.expanduser().resolve()
    manifest_path = args.manifest.expanduser().resolve()
    source_root = args.source_root.expanduser().resolve() if args.source_root else None
    output_dir = args.output_dir.expanduser().resolve()
    try:
        recipe_params = recipe_params_from_config(
            load_testcase_config(manifest_path, args.case)
        ) + args.param
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    if not cli_bin.is_file():
        print(f"error: CLI binary not found: {cli_bin}", file=sys.stderr)
        return 1
    if source_root is not None and not source_root.is_dir():
        print(f"error: source root not found: {source_root}", file=sys.stderr)
        return 1

    cmd = [
        str(cli_bin),
        "recipe",
        "run",
        args.recipe,
        "--workload",
        args.workload_cmd,
        "--target",
        args.target,
        "--deploy-tools",
    ]
    if source_root is not None:
        cmd.extend(["--source", str(source_root)])
    for param in recipe_params:
        cmd.extend(["--param", param])
    process = run_cli(cmd, cli_bin.parent)
    run_id = parse_recipe_run_id(process.stdout + process.stderr)
    print_run_info(cli_bin, run_id)

    case_dir = output_dir / args.case
    case_dir.mkdir(parents=True, exist_ok=True)
    source_bundle = collect_sampled_sources(
        cli_bin,
        run_id,
        case_dir,
    )

    with tempfile.TemporaryDirectory(prefix="ai-insights-export-") as tmp:
        exported = export_run(cli_bin, run_id, Path(tmp))
        latest = case_dir / "latest.zip"
        shutil.copy2(exported, latest)

    metadata = {
        "testcase_id": args.case,
        "recipe": args.recipe,
        "target": args.target,
        "workload_command": args.workload_cmd,
        "recipe_params": recipe_params,
        "manifest_path": str(manifest_path),
        "source_root": str(source_root) if source_root is not None else None,
        "run_id": run_id,
        "cli_version": get_cli_version(cli_bin),
        "timestamp_utc": datetime.now(timezone.utc).isoformat(),
        "archive_size_bytes": latest.stat().st_size,
        "archive_sha256": sha256_file(latest),
        "archive_path": str(latest),
        "source_archive_size_bytes": source_bundle.stat().st_size,
        "source_archive_sha256": sha256_file(source_bundle),
        "source_archive_path": str(source_bundle),
    }
    if args.artifactory_run_base:
        artifactory_case_dir = f"{args.artifactory_run_base.rstrip('/')}/{args.case}"
        metadata.update(
            {
                "artifactory_run_base": args.artifactory_run_base.rstrip("/"),
                "artifactory_archive_path": f"{artifactory_case_dir}/latest.zip",
                "artifactory_source_archive_path": f"{artifactory_case_dir}/test_src.zip",
                "artifactory_metadata_path": f"{artifactory_case_dir}/metadata.json",
            }
        )
    (case_dir / "metadata.json").write_text(json.dumps(metadata, indent=2), encoding="utf-8")
    print(str(latest))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
