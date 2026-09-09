#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Pre-record a Performix run artifact for one testcase."""

import argparse
import json
import posixpath
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import zipfile
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

sys.path.append(str(Path(__file__).resolve().parent))
sys.path.append(str(Path(__file__).resolve().parents[1]))
from modifications.modify_asct_core_to_core_latency import create_core_to_core_latency_asymmetry_run
from modifications.modify_asct_low_peak_bandwidth import create_low_peak_bandwidth_run
from recipe_support import PRERECORD_MODES, sampled_source_weight, source_files_query
from run_export_helper import (
    CommandFailure,
    export_run,
    get_cli_version,
    parse_recipe_run_id,
    run_cli,
    sha256_file,
)


@dataclass(frozen=True)
class Launch:
    command: str


@dataclass(frozen=True)
class AttachToPID:
    pid: int


@dataclass(frozen=True)
class SystemWide:
    pass


ProfilingTarget = Launch | AttachToPID | SystemWide


RUN_MODIFICATIONS = {
    "core_to_core_latency_asymmetry": {
        "recipe": "asct",
        "handler": create_core_to_core_latency_asymmetry_run,
    },
    "low_peak_bandwidth": {
        "recipe": "asct",
        "handler": create_low_peak_bandwidth_run,
    }
}


def run_modification_from_config(config: dict[str, Any], recipe: str) -> str | None:
    """Validate and return the resolved prerecord run modification."""

    name = config.get("run_modification")
    if name is None:
        return None
    if not isinstance(name, str):
        raise ValueError("prerecord run_modification must be a string")

    specification = RUN_MODIFICATIONS.get(name)
    if specification is None:
        raise ValueError(f"unsupported run modification: {name!r}")
    if recipe != specification["recipe"]:
        raise ValueError(
            f"run modification {name!r} requires recipe "
            f"{specification['recipe']!r}, not {recipe!r}"
        )
    return name


def materialize_run_artifact(
    source: Path,
    destination: Path,
    modification: str | None,
) -> None:
    """Create the final run archive."""

    if modification is None:
        destination.with_name("run-modification-report.json").unlink(missing_ok=True)
        shutil.copy2(source, destination)
        return

    RUN_MODIFICATIONS[modification]["handler"](source, destination)


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
    if rows is None:
        return []
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
    recipe: str,
    query: str,
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
        sampled_rows = query_render_rows(cli_bin, session_id, query)
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
                        "periodic_samples": sampled_source_weight(recipe, row),
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
                        "periodic_samples": sampled_source_weight(recipe, row),
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
    if failed:
        print(
            f"warning: could not fetch {len(failed)} sampled source file(s) "
            f"for run {run_id}",
            file=sys.stderr,
        )
    write_source_archive(source_dir, source_archive)
    return source_archive


def parse_args() -> argparse.Namespace:
    """Parse command-line arguments for a single pre-recording run."""

    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    parser.add_argument("--cli-bin", required=True, type=Path)
    parser.add_argument("--target", required=True)
    parser.add_argument("--recipe", required=True)
    profile_target = parser.add_mutually_exclusive_group()
    profile_target.add_argument("--workload-cmd")
    profile_target.add_argument("--pid", type=int)
    parser.add_argument("--timeout")
    parser.add_argument(
        "--prerecord",
        required=True,
        help="Resolved prerecord configuration as a JSON object.",
    )
    parser.add_argument("--params-json", default="[]")
    parser.add_argument("--ssh-target")
    parser.add_argument("--ssh-key", type=Path)
    parser.add_argument("--target-workload-root", type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument(
        "--artifactory-run-base",
        help="Optional Artifactory base path used to record published run locations.",
    )
    parser.add_argument(
        "--param",
        action="append",
        default=[],
        help="Recipe parameter in NAME=VALUE form. May be supplied more than once.",
    )
    return parser.parse_args()


def parse_recipe_params(raw_params: str) -> list[str]:
    """Parse resolved recipe parameters from a JSON array."""

    try:
        params = json.loads(raw_params)
    except json.JSONDecodeError as exc:
        raise ValueError("recipe parameters are not valid JSON") from exc
    if not isinstance(params, list) or not all(isinstance(param, str) for param in params):
        raise ValueError("recipe parameters must be a JSON array of strings")
    return params


def parse_prerecord_config(raw_config: str) -> dict[str, Any]:
    """Parse and validate one testcase's prerecord configuration."""

    try:
        config = json.loads(raw_config)
    except json.JSONDecodeError as exc:
        raise ValueError("prerecord configuration is not valid JSON") from exc
    if not isinstance(config, dict):
        raise ValueError("prerecord configuration must be an object")

    mode = config.get("mode")
    if not isinstance(mode, str) or mode not in PRERECORD_MODES:
        raise ValueError(f"unsupported prerecord mode: {mode}")
    config["mode"] = mode

    timeout_seconds = config.get("timeout_seconds")
    if timeout_seconds is not None and (
        not isinstance(timeout_seconds, int) or timeout_seconds <= 0
    ):
        raise ValueError("prerecord timeout_seconds must be a positive integer")

    setup = config.get("setup")
    cleanup = config.get("cleanup")
    if (setup is None) != (cleanup is None):
        raise ValueError("prerecord setup and cleanup must be paired")
    for name, command in (("setup", setup), ("cleanup", cleanup)):
        if command is not None and (
            not isinstance(command, list)
            or not command
            or not all(isinstance(part, str) and part for part in command)
        ):
            raise ValueError(f"prerecord {name} must be a command array")
    if mode == "system-wide" and setup is not None:
        raise ValueError("system-wide prerecord cannot use setup or cleanup lifecycle commands")
    return config


def run_remote_command(
    command: list[str],
    ssh_target: str,
    ssh_key: Path,
    target_workload_root: Path,
    *,
    check: bool,
) -> subprocess.CompletedProcess[str]:
    """Run a workload lifecycle command on the profiling target."""

    remote_command = (
        f"cd {shlex.quote(str(target_workload_root))} && {shlex.join(command)}"
    )
    process = subprocess.run(
        ["ssh", "-i", str(ssh_key), ssh_target, remote_command],
        text=True,
        capture_output=True,
        check=False,
    )
    if process.stdout:
        print(process.stdout, end="")
    if process.stderr:
        print(process.stderr, end="", file=sys.stderr)
    if check and process.returncode != 0:
        raise RuntimeError(
            f"target command failed with exit code {process.returncode}: "
            f"{shlex.join(command)}"
        )
    return process


def attach_pid_from_setup(output: str) -> int:
    """Return the PID reported by an attach-mode setup command."""

    matches = re.findall(r"^PID:([0-9]+)$", output, flags=re.MULTILINE)
    if not matches:
        raise ValueError("attach setup did not return PID:<pid>")
    return int(matches[-1])


def profiling_target_args(profiling_target: ProfilingTarget) -> list[str]:
    """Return CLI arguments for one profiling target."""

    if isinstance(profiling_target, Launch):
        return ["--workload", profiling_target.command]
    if isinstance(profiling_target, AttachToPID):
        return ["--pid", str(profiling_target.pid)]
    if isinstance(profiling_target, SystemWide):
        return ["--system-wide"]
    raise TypeError(f"unsupported profiling target: {profiling_target!r}")


def build_recipe_ready_command(
    cli_bin: Path,
    recipe: str,
    target: str,
    profiling_target: ProfilingTarget,
    recipe_params: list[str],
) -> list[str]:
    """Build the readiness command for one profiling target."""

    cmd = [str(cli_bin), "recipe", "ready", recipe]
    cmd.extend(profiling_target_args(profiling_target))
    cmd.extend(["--target", target])
    for param in recipe_params:
        cmd.extend(["--param", param])
    return cmd


def build_recipe_run_command(
    cli_bin: Path,
    recipe: str,
    target: str,
    profiling_target: ProfilingTarget,
    timeout: str | None,
    recipe_params: list[str],
) -> list[str]:
    """Build a recipe command for one profiling target."""

    cmd = [str(cli_bin), "recipe", "run", recipe]
    cmd.extend(profiling_target_args(profiling_target))
    cmd.extend(["--target", target, "--deploy-tools"])
    if timeout:
        cmd.extend(["--timeout", timeout])
    for param in recipe_params:
        cmd.extend(["--param", param])
    return cmd


def main() -> int:
    """Run the selected recipe, export the run, and write testcase metadata."""

    args = parse_args()
    cli_bin = args.cli_bin.expanduser().resolve()
    output_dir = args.output_dir.expanduser().resolve()
    try:
        recipe_params = parse_recipe_params(args.params_json) + list(args.param)
        prerecord = parse_prerecord_config(args.prerecord)
        run_modification = run_modification_from_config(prerecord, args.recipe)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    try:
        source_query = source_files_query(args.recipe, recipe_params)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    if not cli_bin.is_file():
        print(f"error: CLI binary not found: {cli_bin}", file=sys.stderr)
        return 1
    setup = prerecord.get("setup")
    cleanup = prerecord.get("cleanup")
    profile_mode = prerecord["mode"]
    if profile_mode == "launch" and args.workload_cmd is None:
        print("error: launch prerecord requires --workload-cmd", file=sys.stderr)
        return 1
    if profile_mode == "attach" and args.workload_cmd is not None:
        print("error: attach prerecord cannot use --workload-cmd", file=sys.stderr)
        return 1
    if profile_mode == "attach" and args.pid is None and setup is None:
        print("error: attach prerecord requires --pid or setup and cleanup commands", file=sys.stderr)
        return 1
    if profile_mode == "system-wide" and (args.workload_cmd is not None or args.pid is not None):
        print("error: system-wide prerecord does not accept --workload-cmd or --pid", file=sys.stderr)
        return 1
    if setup is not None:
        if not args.ssh_target or args.ssh_key is None or args.target_workload_root is None:
            print(
                "error: prerecord lifecycle commands require --ssh-target, "
                "--ssh-key, and --target-workload-root",
                file=sys.stderr,
            )
            return 1
        ssh_key = args.ssh_key.expanduser().resolve()
        if not ssh_key.is_file():
            print(f"error: SSH key not found: {ssh_key}", file=sys.stderr)
            return 1
    else:
        ssh_key = None

    setup_started = False
    try:
        if setup is not None:
            setup_started = True
            setup_process = run_remote_command(
                setup,
                args.ssh_target,
                ssh_key,
                args.target_workload_root,
                check=True,
            )
        if profile_mode == "launch":
            profiling_target: ProfilingTarget = Launch(args.workload_cmd)
        elif profile_mode == "attach":
            pid = attach_pid_from_setup(setup_process.stdout) if setup is not None else args.pid
            profiling_target = AttachToPID(pid)
        else:
            profiling_target = SystemWide()

        ready_cmd = build_recipe_ready_command(
            cli_bin,
            args.recipe,
            args.target,
            profiling_target,
            recipe_params,
        )
        run_cli(ready_cmd, cli_bin.parent)
        timeout = prerecord.get("timeout_seconds", args.timeout)
        cmd = build_recipe_run_command(
            cli_bin,
            args.recipe,
            args.target,
            profiling_target,
            str(timeout) if timeout is not None else None,
            recipe_params,
        )
        process = run_cli(cmd, cli_bin.parent)
    finally:
        if setup_started:
            cleanup_process = run_remote_command(
                cleanup,
                args.ssh_target,
                ssh_key,
                args.target_workload_root,
                check=False,
            )
            if cleanup_process.returncode != 0:
                print("warning: prerecord cleanup command failed", file=sys.stderr)

    run_id = parse_recipe_run_id(process.stdout + process.stderr)
    print_run_info(cli_bin, run_id)

    case_dir = output_dir / args.case
    case_dir.mkdir(parents=True, exist_ok=True)
    source_bundle = None
    if source_query is not None:
        source_bundle = collect_sampled_sources(
            cli_bin,
            run_id,
            case_dir,
            args.recipe,
            source_query,
        )

    with tempfile.TemporaryDirectory(prefix="ai-insights-export-") as tmp:
        exported = export_run(cli_bin, run_id, Path(tmp))
        latest = case_dir / "latest.zip"
        materialize_run_artifact(exported, latest, run_modification)

    metadata = {
        "testcase_id": args.case,
        "recipe": args.recipe,
        "target": args.target,
        "profile_mode": profile_mode,
        "workload_command": (
            profiling_target.command if isinstance(profiling_target, Launch) else None
        ),
        "pid": profiling_target.pid if isinstance(profiling_target, AttachToPID) else None,
        "recipe_params": recipe_params,
        "run_modification": run_modification,
        "run_id": run_id,
        "cli_version": get_cli_version(cli_bin),
        "timestamp_utc": datetime.now(timezone.utc).isoformat(),
        "archive_size_bytes": latest.stat().st_size,
        "archive_sha256": sha256_file(latest),
        "archive_path": str(latest),
    }
    if source_bundle is not None:
        metadata.update(
            {
                "source_archive_size_bytes": source_bundle.stat().st_size,
                "source_archive_sha256": sha256_file(source_bundle),
                "source_archive_path": str(source_bundle),
            }
        )
    if args.artifactory_run_base:
        artifactory_case_dir = f"{args.artifactory_run_base.rstrip('/')}/{args.case}"
        metadata.update(
            {
                "artifactory_run_base": args.artifactory_run_base.rstrip("/"),
                "artifactory_archive_path": f"{artifactory_case_dir}/latest.zip",
                "artifactory_metadata_path": f"{artifactory_case_dir}/metadata.json",
            }
        )
        if source_bundle is not None:
            metadata["artifactory_source_archive_path"] = (
                f"{artifactory_case_dir}/test_src.zip"
            )
    (case_dir / "metadata.json").write_text(json.dumps(metadata, indent=2), encoding="utf-8")
    print(str(latest))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
