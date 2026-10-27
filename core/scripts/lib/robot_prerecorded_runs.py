# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Select and validate pre-recorded runs for Robot tests.

Used by run-robot.py to choose which runs to download, and by the PrerecordedRuns library
to find a run for the test target and check its workload and profiling settings.
"""

import json
import re
from pathlib import Path

MANIFEST = Path(__file__).resolve().parents[1] / "ai-insights-evaluation" / "robot_prerecorded_runs.json"
TARGET_PLATFORMS = {"Linux": "linux", "Windows": "windows"}
TARGETS = {("linux", "aarch64"), ("linux", "x86-64"), ("windows", "aarch64")}
SAFE_ID = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$")


def validate_robot_case_identity(case):
    """Check the pre-recorded run's ID, platform and architecture."""
    platform, arch = case.get("platform"), case.get("arch")
    if (platform, arch) not in TARGETS:
        raise ValueError(f"Unsupported Robot capture target: {platform!r}/{arch!r}")
    logical_id = case.get("logical_id")
    if not isinstance(logical_id, str) or not SAFE_ID.fullmatch(logical_id):
        raise ValueError(f"Invalid Robot logical_id: {logical_id!r}")
    expected = f"{logical_id}-{platform}-{arch}"
    if case.get("id") != expected:
        raise ValueError(f"Robot run ID must be {expected}")


def _catalog(manifest):
    try:
        entries = json.loads(Path(manifest).read_text(encoding="utf-8"))["tests"]
        if not isinstance(entries, list):
            raise ValueError("tests must be a list")
        ids = set()
        for entry in entries:
            validate_robot_case_identity(entry)
            if entry["id"] in ids:
                raise ValueError(f"Duplicate Robot run ID: {entry['id']}")
            ids.add(entry["id"])
        return entries
    except (OSError, KeyError, TypeError, ValueError) as exc:
        raise RuntimeError(f"Invalid Robot pre-recorded run catalog {manifest}: {exc}") from exc


def _target(target_os, target_arch):
    platform = TARGET_PLATFORMS.get(target_os)
    if (platform, target_arch) not in TARGETS:
        raise RuntimeError(f"No pre-recorded runs for target {target_os!r}/{target_arch!r}")
    return platform, target_arch


def _uses_suite(entry, suites):
    if not suites:
        return True
    core = MANIFEST.parents[2]
    for suite in suites:
        selected = Path(str(suite).replace("\\", "/"))
        if not selected.is_absolute() and selected.parts and selected.parts[0] == "core":
            selected = selected.relative_to("core")
        selected = (core / selected).resolve()
        for consumer in entry["suites"]:
            consumer = (core / "robot" / "tests" / consumer).resolve()
            if selected == consumer or selected in consumer.parents or consumer in selected.parents:
                return True
    return False


def robot_prerecorded_run_ids(target, suites=None, manifest=MANIFEST):
    """Return pre-recorded run IDs for the target platform and selected test suites."""
    entries = [entry for entry in _catalog(manifest) if _uses_suite(entry, suites)]
    if not entries:
        return []  # Suites that only capture live runs need no pre-recorded runs.
    platform, arch = _target(target.get("os"), target.get("arch"))
    selected = [entry["id"] for entry in entries if (entry["platform"], entry["arch"]) == (platform, arch)]
    return selected


def resolve_robot_prerecorded_run(logical_id, target_os, target_arch, manifest=MANIFEST):
    """Find a pre-recorded run for the target's OS and architecture, not the CLI host's."""
    platform, arch = _target(target_os, target_arch)
    run_id = f"{logical_id}-{platform}-{arch}"
    if not any(entry["id"] == run_id for entry in _catalog(manifest)):
        raise RuntimeError(f"No pre-recorded run {logical_id!r} for {target_os}/{target_arch}")
    return run_id


def validate_robot_prerecorded_metadata(metadata_path, run_id, workload_command):
    """Check that the pre-recorded run matches the expected ID, workload and profiling settings."""
    metadata = json.loads(Path(metadata_path).read_text(encoding="utf-8"))
    if metadata.get("testcase_id") != run_id:
        raise RuntimeError(f"Pre-recorded metadata does not match {run_id}")
    case = next(entry for entry in _catalog(MANIFEST) if entry["id"] == run_id)
    # Robot's live command is quoted as one CLI argument; metadata stores its value.
    command = workload_command.strip().strip('"')
    if command != case["workload_command"] or metadata.get("workload_command") != command:
        raise RuntimeError(f"Pre-recorded workload does not match {command!r}; use --live-recipe-runs for a custom workload")
    recipe = metadata.get("recipe", "").replace("\\", "/")
    recipe_matches = recipe == case["recipe"] or (
        case["recipe"].startswith("core/") and recipe.endswith("/" + case["recipe"])
    )
    if (not recipe_matches or metadata.get("recipe_params") != case.get("recipe_params", [])
            or metadata.get("profile_mode") != "launch" or metadata.get("run_modification") is not None):
        raise RuntimeError(f"Pre-recorded recipe configuration does not match {run_id}; record the run again")
