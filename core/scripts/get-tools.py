#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Retrieve and package credential-free tools for APX builds from open-source code."""

from __future__ import annotations

import argparse
import concurrent.futures
import os
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
from collections.abc import Sequence
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
DEFAULT_TOOLS_DIR = SCRIPT_DIR.parent / "apap-cli" / "tools"
INTERNAL_SCRIPT = SCRIPT_DIR / "get-tools-internal.py"

SYSUTIL_TOOL_NAME = "sysutil-timeline"
SYSUTIL_TARGETS = (("Linux", "aarch64"), ("Linux", "x86_64"))
PARQUET_TO_JSON_TOOL_NAME = "parquet-to-json"

TARGET_AGENT_VARIANTS = (
    ("Android", "aarch64", "android-arm64"),
    ("Linux", "aarch64", "linux-arm64"),
    ("Linux", "x86_64", "linux-amd64"),
    ("Windows", "aarch64", "windows-arm64"),
    ("Windows", "x86_64", "windows-amd64"),
    ("Darwin", "aarch64", "darwin-arm64"),
    ("Darwin", "x86_64", "darwin-amd64"),
)

PUBLIC_TOOLS = ["target_agent", SYSUTIL_TOOL_NAME, PARQUET_TO_JSON_TOOL_NAME]

RELEASE_TARGETS = [
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("windows", "amd64"),
    ("windows", "arm64"),
]
RELEASE_ARCH_NAMES = {"amd64": "x86_64", "arm64": "aarch64"}


# ------------------------------------------------------------------------------
# Common helpers
# ------------------------------------------------------------------------------


def is_snapshot_build() -> bool:
    """Return whether built-in tool bundles should use the snapshot engine version.

    GoReleaser snapshot engine binaries use ``<version>-dev``. Workflows set
    ``PERFORMIX_SNAPSHOT_BUILD`` from the same condition that controls the
    engine's GoReleaser ``--snapshot`` argument, so built-in tools are packaged
    under the version exposed at runtime as ``performix.engineVersion``.
    """
    value = os.environ.get("PERFORMIX_SNAPSHOT_BUILD")
    if value is not None:
        return value.strip().lower() in ("1", "true", "yes", "on")

    # Keep SNAPSHOT_ARG as a fallback for older callers.
    return bool(os.environ.get("SNAPSHOT_ARG", "").strip())


def get_engine_version() -> str:
    """Resolve the engine version used for deployable bundle directories."""
    version = os.environ.get("PERFORMIX_ENGINE_VERSION", "").strip()
    if not version:
        version = subprocess.check_output(
            [sys.executable, SCRIPT_DIR / "get_atperf_version.py"], text=True
        ).strip()
    if not version:
        raise RuntimeError("Could not determine Performix engine version")
    if is_snapshot_build():
        version = f"{version}-dev"
    return version


def _get_builtin_tool_source(tool_name: str, required_file: str) -> Path:
    """Return an in-tree built-in tool directory after validating its source."""
    source_dir = SCRIPT_DIR.parent / "apap-cli" / "tools-builtin" / tool_name
    source_file = source_dir / required_file
    if not source_file.exists():
        raise FileNotFoundError(
            f"{required_file} not found for {tool_name} at {source_file}"
        )
    return source_dir


# ------------------------------------------------------------------------------
# Built-in Go tools
# ------------------------------------------------------------------------------


def package_builtin_go_tool(
    source_relative_dir: Path,
    tool_name: str,
    variants: list[tuple[str, str]],
    tools_dir: Path,
) -> None:
    """
    Builds all variants of a built-in Go tool locally and packages them. The tool will be versioned
    according to the current Performix engine version. Tools are built using `-trimpath -ldflags "-s -w"`
    which strips debug info to reduce the tarball size.
    """
    source_dir = SCRIPT_DIR.parent / "apap-cli" / "tools-builtin" / source_relative_dir
    source_file = source_dir / "main.go"
    if not source_file.exists():
        raise FileNotFoundError(f"main.go not found for {tool_name} at {source_file}")

    if not shutil.which("go"):
        raise RuntimeError(f"go is required to build the {tool_name} tool")

    version = get_engine_version()
    tool_dst_dir = tools_dir / tool_name / version
    tool_dst_dir.mkdir(parents=True, exist_ok=True)

    for os_name, arch in variants:
        archive_name = f"{tool_name}-{os_name}-{arch}.tar.gz"
        output_file = tool_dst_dir / archive_name

        if arch == "aarch64":
            goarch = "arm64"
        elif arch == "x86_64":
            goarch = "amd64"
        else:
            raise RuntimeError(f"unknown arch {arch}: expected either aarch64 or x86_64")
        goos = os_name.lower()

        print(f"[{tool_name}] Creating {archive_name} …")

        with tempfile.TemporaryDirectory() as tmp_dir:
            binary_name = tool_name
            if goos == "windows":
                binary_name += ".exe"
            binary = Path(tmp_dir) / binary_name
            env = os.environ.copy()
            env.update({"CGO_ENABLED": "0", "GOOS": goos, "GOARCH": goarch})
            subprocess.run(
                ["go", "build", "-trimpath", "-ldflags", "-s -w", "-o", str(binary), "."],
                cwd=source_dir,
                env=env,
                check=True,
            )

            binary.chmod(
                binary.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH
            )
            with tarfile.open(output_file, "w:gz") as tf:
                info = tf.gettarinfo(str(binary), arcname=binary_name)
                info.mode = 0o755
                with binary.open("rb") as fileobj:
                    tf.addfile(info, fileobj)

        print(f"[{tool_name}] Package created: {output_file}")


# ------------------------------------------------------------------------------
# sysutil-timeline
# ------------------------------------------------------------------------------


def package_sysutil_timeline(tools_dir: Path) -> tuple[Path, ...]:
    """Create the System Utilization collector bundles for Linux targets."""
    source_dir = _get_builtin_tool_source(
        SYSUTIL_TOOL_NAME,
        "sysutil-timeline.py",
    )
    version = get_engine_version()
    destination_dir = tools_dir / SYSUTIL_TOOL_NAME / version
    destination_dir.mkdir(parents=True, exist_ok=True)

    _EXCLUDE_NAMES = {"__pycache__", ".pytest_cache", "env", "tests"}

    def archive_filter(info: tarfile.TarInfo) -> tarfile.TarInfo | None:
        parts = Path(info.name).parts
        if any(
            part in _EXCLUDE_NAMES or part.endswith(".pyc")
            for part in parts
        ):
            return None
        return info

    archives: list[Path] = []
    for os_name, arch in SYSUTIL_TARGETS:
        archive_name = f"{SYSUTIL_TOOL_NAME}-{os_name}-{arch}.tar.gz"
        output_file = destination_dir / archive_name
        print(f"[{SYSUTIL_TOOL_NAME}] Creating {archive_name} …")
        with tarfile.open(output_file, "w:gz") as archive:
            for entry in sorted(source_dir.iterdir()):
                archive.add(
                    entry,
                    arcname=entry.name,
                    filter=archive_filter,
                )
        print(f"[{SYSUTIL_TOOL_NAME}] Package created: {output_file}")
        archives.append(output_file)

    return tuple(archives)


# ------------------------------------------------------------------------------
# parquet-to-json
# ------------------------------------------------------------------------------


def package_parquet_to_json(tools_dir: Path) -> None:
    """
    Builds all parquet_to_json variants locally and packages them.
    """
    variants = [
        ("Linux", "aarch64"),
        ("Linux", "x86_64"),
        ("Windows", "aarch64"),
        ("Windows", "x86_64"),
        ("Darwin", "aarch64"),
        ("Darwin", "x86_64"),
    ]
    package_builtin_go_tool(
        Path(PARQUET_TO_JSON_TOOL_NAME),
        PARQUET_TO_JSON_TOOL_NAME,
        variants,
        tools_dir,
    )


# ------------------------------------------------------------------------------
# target_agent
# ------------------------------------------------------------------------------


def package_target_agent(tools_dir: Path) -> None:
    """Build and package all target-agent variants using GoReleaser."""
    if os.getenv("SKIP_APX_AGENT_BUILD"):
        print("[target_agent] Skipping APX agent build")
        return

    agent_binary = subprocess.check_output(
        [
            sys.executable,
            SCRIPT_DIR / "terminology" / "terminology.py",
            "get_agent_binary_name",
        ],
        text=True,
    ).strip()

    artifacts_dir = SCRIPT_DIR.parent / f"{agent_binary}-artifacts"
    if not artifacts_dir.is_dir():
        subprocess.run(
            [
                sys.executable,
                SCRIPT_DIR / "build_target_agent.py",
                "--config",
                SCRIPT_DIR.parent / ".goreleaser-agent.yml",
                "--no-inject",
                "--no-sign",
                "--snapshot",
            ],
            check=True,
        )
        dist_dir = SCRIPT_DIR.parent.parent / "dist"
        dist_dir.rename(artifacts_dir)

    tools_dir.mkdir(parents=True, exist_ok=True)
    target_agent_version = os.environ.get("TARGET_AGENT_VERSION", "")
    snapshot_build = is_snapshot_build()
    if not target_agent_version:
        target_agent_version = subprocess.check_output(
            [sys.executable, SCRIPT_DIR / "get_atperf_version.py"], text=True
        ).strip()
    elif snapshot_build:
        target_agent_version = f"{target_agent_version}-dev"

    for os_name, arch, artifact_target in TARGET_AGENT_VARIANTS:
        subprocess.run(
            [
                sys.executable,
                SCRIPT_DIR / "bundle_tool.py",
                "--tool-name",
                agent_binary,
                "--version",
                target_agent_version,
                "--os",
                os_name,
                "--arch",
                arch,
                "--source",
                artifacts_dir / f"{agent_binary}-{artifact_target}.tar.gz",
                "--tools-dir",
                tools_dir,
            ],
            check=True,
        )

    shutil.rmtree(artifacts_dir)


# ------------------------------------------------------------------------------
# Release staging
# ------------------------------------------------------------------------------


def prepare_release_tool_dirs(tools_dir: Path) -> None:
    """Create the host-specific tool trees consumed by release packaging."""
    for goos, goarch in RELEASE_TARGETS:
        release_dir = tools_dir.with_name(f"{tools_dir.name}-{goos}-{goarch}")
        shutil.rmtree(release_dir, ignore_errors=True)

        # Tools that match the suffixes are included for all platforms
        suffixes = {
            "-Android-aarch64.tar.gz",
            "-Linux-aarch64.tar.gz",
            "-Linux-x86_64.tar.gz",
            "-Windows-aarch64.tar.gz",
            f"-{goos.title()}-{RELEASE_ARCH_NAMES[goarch]}.tar.gz",
        }

        def ignore_incompatible_bundles(
            directory: str, names: list[str]
        ) -> set[str]:
            if "license_terms" in Path(directory).parts:
                return set()
            return {
                name
                for name in names
                if name.endswith(".tar.gz")
                and not any(name.endswith(suffix) for suffix in suffixes)
            }

        shutil.copytree(
            tools_dir,
            release_dir,
            ignore=ignore_incompatible_bundles,
        )
        print(f"[get-tools] Prepared release tools: {release_dir}")


# ------------------------------------------------------------------------------
# Tool orchestration
# ------------------------------------------------------------------------------


def package_tool(
    tools_dir: Path,
    tool: str,
) -> None:
    """Package one public tool."""
    if tool == "target_agent":
        package_target_agent(tools_dir)
    elif tool == SYSUTIL_TOOL_NAME:
        package_sysutil_timeline(tools_dir)
    elif tool == PARQUET_TO_JSON_TOOL_NAME:
        package_parquet_to_json(tools_dir)
    else:
        raise ValueError(f"Unknown public tool: {tool}")


def prepare_tools(
    tools_dir: Path,
    tools: Sequence[str],
) -> None:
    """Package selected public tools in parallel."""
    errors: list[str] = []

    with concurrent.futures.ThreadPoolExecutor() as pool:
        futures = {
            pool.submit(package_tool, tools_dir, tool): tool
            for tool in tools
        }

        for future in concurrent.futures.as_completed(futures):
            try:
                future.result()
            except Exception as exc:
                tool = futures[future]
                errors.append(f"{tool}: {exc}")

    if errors:
        raise RuntimeError("\n".join(errors))


def validate_internal_tools(
    tools: Sequence[str],
    include_pre_release: bool,
) -> None:
    """Validate internal tool names before any packaging starts."""
    command = [
        sys.executable,
        INTERNAL_SCRIPT,
        "--validate-only",
    ]
    if include_pre_release:
        command.append("--pre-release")
    command.extend(tools)

    result = subprocess.run(command, check=False)
    if result.returncode != 0:
        raise SystemExit(result.returncode)


# ------------------------------------------------------------------------------
# Arg parse & main
# ------------------------------------------------------------------------------


def _build_argument_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Package the public tools required for APX builds."
        ),
        epilog=f"Public tools: {' '.join(PUBLIC_TOOLS)}",
    )
    parser.add_argument(
        "--dest",
        type=Path,
        default=DEFAULT_TOOLS_DIR,
        help="destination tools directory (default: %(default)s)",
    )
    if INTERNAL_SCRIPT.exists():
        parser.add_argument(
            "--pre-release",
            action="store_true",
            help="include internal pre-release tools when no tool names are given",
        )
    parser.add_argument(
        "tools",
        nargs="*",
        metavar="TOOL",
        help="specific tool names to package",
    )
    return parser


def main(argv: list[str] | None = None) -> None:
    args = _build_argument_parser().parse_args(argv)
    requested_tools = list(args.tools)
    public_tools = (
        [tool for tool in requested_tools if tool in PUBLIC_TOOLS]
        if requested_tools
        else list(PUBLIC_TOOLS)
    )
    internal_tools = [
        tool for tool in requested_tools if tool not in PUBLIC_TOOLS
    ]

    if internal_tools and not INTERNAL_SCRIPT.exists():
        print(
            f"Unknown public tool(s): {' '.join(internal_tools)}",
            file=sys.stderr,
        )
        print(
            f"Public tools: {' '.join(PUBLIC_TOOLS)}",
            file=sys.stderr,
        )
        raise SystemExit(1)

    run_internal = INTERNAL_SCRIPT.exists() and (
        not requested_tools or internal_tools
    )
    include_pre_release = getattr(args, "pre_release", False)
    if run_internal:
        validate_internal_tools(internal_tools, include_pre_release)

    print(
        f"[get-tools] Preparing {len(public_tools)} public tool(s): "
        f"{' '.join(public_tools)}"
    )
    print(f"[get-tools] Destination: {args.dest}")

    errors: list[str] = []
    with concurrent.futures.ThreadPoolExecutor() as pool:
        futures: dict[concurrent.futures.Future, str] = {}
        if public_tools:
            futures[
                pool.submit(prepare_tools, args.dest, public_tools)
            ] = "public tools"

        if run_internal:
            internal_command = [
                sys.executable,
                INTERNAL_SCRIPT,
                "--dest",
                str(args.dest),
            ]
            if include_pre_release:
                internal_command.append("--pre-release")
            internal_command.extend(internal_tools)
            futures[
                pool.submit(subprocess.run, internal_command, check=True)
            ] = "internal tools"

        for future in concurrent.futures.as_completed(futures):
            try:
                future.result()
            except Exception as exc:
                errors.append(f"{futures[future]}: {exc}")

    if errors:
        print("\n[get-tools] The following tools failed:", file=sys.stderr)
        print("\n".join(errors), file=sys.stderr)
        raise SystemExit(1)

    prepare_release_tool_dirs(args.dest)
    print("[get-tools] All done.")


if __name__ == "__main__":
    main()
