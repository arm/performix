#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path


def parse_args():
    parser = argparse.ArgumentParser(
        description="Run a GoReleaser v2 release."
    )
    parser.add_argument(
        "--snapshot",
        action="store_true",
        help="Include the --snapshot flag when running goreleaser"
    )
    parser.add_argument(
        "--no-inject",
        action="store_true",
        help="Set NO_INJECT=1 for the goreleaser run (to skip version injection)"
    )
    parser.add_argument(
        "--no-sign",
        action="store_true",
        help="Set NO_SIGN=1 for the goreleaser run (to skip signing hooks)"
    )
    parser.add_argument(
        "--config",
        required=True,
        help="Path to the goreleaser YAML config file"
    )
    return parser.parse_args()


def require_goreleaser():
    """Return the GoReleaser path, or exit if it is not available."""
    exe = shutil.which("goreleaser")
    if exe:
        print(f"goreleaser found in PATH: {exe}")
        return exe

    print(
        "goreleaser not found in PATH. Install the repository tools with "
        "`mise install`, or install GoReleaser separately.",
        file=sys.stderr,
    )
    sys.exit(1)


def run_goreleaser(executable_path, config_path, snapshot, no_inject, no_sign):
    """
    Invoke goreleaser, optionally with --snapshot.
    """
    config_path = os.path.abspath(config_path)
    repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
    cmd = [
        executable_path,
        "release",
        "-f", os.path.relpath(config_path, repo_root),
        "--clean",
        "--skip", "announce,publish,nfpm",
    ]
    if snapshot:
        cmd.append("--snapshot")

    env = os.environ.copy()
    env["PERFORMIX_PYTHON_EXECUTABLE"] = Path(sys.executable).as_posix()
    if no_inject:
        env["NO_INJECT"] = "1"
    if no_sign:
        env["NO_SIGN"] = "1"

    print(f"Running: {' '.join(cmd)}")
    try:
        subprocess.run(cmd, check=True, cwd=repo_root, env=env)
    except subprocess.CalledProcessError as e:
        print(f"goreleaser failed (exit code {e.returncode})", file=sys.stderr)
        sys.exit(e.returncode)


def main():
    args = parse_args()
    goreleaser_path = require_goreleaser()
    run_goreleaser(
        executable_path=goreleaser_path,
        config_path=args.config,
        snapshot=args.snapshot,
        no_inject=args.no_inject,
        no_sign=args.no_sign,
    )


if __name__ == "__main__":
    main()
