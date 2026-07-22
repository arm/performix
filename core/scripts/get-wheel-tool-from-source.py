#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import List, Tuple

from tool_configs import WHEEL_FROM_SOURCE_TOOL_REGISTRY
from utils.archive import create_tarball
from utils.fs import clean
from utils.paths import default_tools_dir


def log(tool, message: str) -> None:
    print(f"[{tool.tool_name}] {message}")


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Clone, build, and package wheel-based tools from source."
    )
    parser.add_argument(
        "tool_name",
        help=(
            "Tool name to build from source. Available: "
            f"{', '.join(sorted(WHEEL_FROM_SOURCE_TOOL_REGISTRY.keys()))}"
        ),
    )
    parser.add_argument(
        "--tools-dir",
        nargs="?",
        required=False,
        help="Destination tools directory (defaults to ../apap-cli/tools relative to the script).",
    )
    return parser.parse_args()


def run(tool, cmd: List[str]) -> None:
    cmd_str = " ".join(cmd)
    log(tool, f"Running: {cmd_str}")
    subprocess.run(cmd, check=True)


def resolve_ref(tool) -> str:
    if tool.source_ref:
        return tool.source_ref

    raise RuntimeError(f"Missing source ref for {tool.tool_name}.")


def create_build_venv(tool, venv_dir: Path) -> Path:
    run(tool, [sys.executable, "-m", "venv", str(venv_dir)])
    python = venv_dir / "bin" / "python"
    pip = venv_dir / "bin" / "pip"
    run(tool, [str(pip), "install", "build"])
    return python


def build_artifact(tool, repo_dir: Path, python: Path) -> Path:
    build_dir = repo_dir / tool.wheel_from_source_conf.source_subdir
    if not build_dir.is_dir():
        raise FileNotFoundError(f"Build directory not found: {build_dir}")

    log(tool, f"Running: {python} -m build --wheel (cwd={build_dir})")
    subprocess.run([str(python), "-m", "build", "--wheel"], cwd=build_dir, check=True)

    wheels = sorted((build_dir / "dist").glob("*.whl"))
    if len(wheels) != 1:
        raise RuntimeError(
            f"Expected exactly one wheel in {build_dir / 'dist'}, found {len(wheels)}"
        )
    return wheels[0]


def parse_wheel_identity(wheel_path: Path) -> Tuple[str, str]:
    parts = wheel_path.stem.split("-")
    if len(parts) < 5:
        raise RuntimeError(f"Unexpected wheel filename: {wheel_path.name}")
    distribution = "-".join(parts[:-4])
    version = parts[-4]
    return distribution, version


def package_wheel(tool, wheel_path: Path, *, tools_dir: Path) -> List[Path]:
    bundle_name = wheel_path.name
    version = tool.version

    bundle_dir = tools_dir / bundle_name / version
    payload_dir = bundle_dir / "__tmp"

    clean(payload_dir)
    bundle_dir.mkdir(parents=True, exist_ok=True)
    payload_dir.mkdir(parents=True, exist_ok=True)
    (payload_dir / bundle_name).write_bytes(wheel_path.read_bytes())

    archives: List[Path] = []
    for platform in tool.available_platforms:
        archive_path = bundle_dir / f"{bundle_name}-{platform.os}-{platform.arch}.tar.gz"
        create_tarball(payload_dir, archive_path)
        archives.append(archive_path)

    clean(payload_dir)
    return archives


def main() -> None:
    args = parse_arguments()
    tool_name = str(args.tool_name)
    tool = WHEEL_FROM_SOURCE_TOOL_REGISTRY.get(tool_name)
    if tool is None:
        available = ", ".join(sorted(WHEEL_FROM_SOURCE_TOOL_REGISTRY.keys()))
        raise ValueError(f"Unknown tool '{tool_name}'. Available: {available}")

    tool_base_dir = Path(args.tools_dir) if args.tools_dir else default_tools_dir()
    tool_base_dir.mkdir(parents=True, exist_ok=True)

    ref = resolve_ref(tool)

    with tempfile.TemporaryDirectory(prefix=f"{tool.tool_name}_") as temp_root:
        temp_root_path = Path(temp_root)
        repo_dir = temp_root_path / "repo"
        venv_dir = temp_root_path / "venv"

        run(tool, ["git", "clone", "--depth", "1", "--branch", ref, tool.wheel_from_source_conf.repo_url, str(repo_dir) ])

        python = create_build_venv(tool, venv_dir)
        artifact_path = build_artifact(tool, repo_dir, python)
        distribution, version = parse_wheel_identity(artifact_path)
        archives = package_wheel(tool, artifact_path, tools_dir=tool_base_dir)

    log(tool, f"Packaged {distribution} {version} into:\n" + "\n".join(str(path) for path in archives))


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"[Error] {e}")
        sys.exit(1)
