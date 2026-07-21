#!/usr/bin/env python3
# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Check copyright and license metadata for files planned for open source."""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]

EXCLUDED_PREFIXES = (
    ".agents/",
    ".release-worktrees/",
    ".vscode/",
    "gui/",
    "core/license_terms/",
)


def run(command: list[str], *, cwd: Path = REPO_ROOT) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, cwd=cwd, check=False, text=True)


def git_files() -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=REPO_ROOT,
        check=True,
        stdout=subprocess.PIPE,
    )
    return [path for path in result.stdout.decode().split("\0") if path]


def scoped_files() -> list[str]:
    paths = git_files()
    return [
        path
        for path in paths
        if not path.startswith(EXCLUDED_PREFIXES)
    ]


def copy_scope(destination: Path) -> None:
    for relative in scoped_files():
        source = REPO_ROOT / relative
        if not source.exists():
            continue
        if source.is_dir():
            continue
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        if source.is_symlink():
            os.symlink(os.readlink(source), target)
        else:
            shutil.copy2(source, target)


def run_in_scope(reuse_args: list[str]) -> int:
    with tempfile.TemporaryDirectory(prefix="performix-copyright-lint-") as temp_dir:
        scope_root = Path(temp_dir)
        copy_scope(scope_root)
        command = [
            sys.executable,
            "-m",
            "reuse",
            "--no-multiprocessing",
            "--root",
            str(scope_root),
            *reuse_args,
        ]
        result = run(command, cwd=scope_root)
        if result.returncode != 0 and reuse_args[:1] == ["lint"]:
            print(
                "\nCopyright/license lint failed for the files planned for open source. "
                "Run `task copyright:annotate`, review the diff, and "
                "manually handle any third-party or non-commentable files left over.",
                file=sys.stderr,
            )
        return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    subparsers.add_parser("list", help="List tracked files in the copyright/license scope")
    subparsers.add_parser("lint", help="Run copyright/license lint against the scoped files")

    args = parser.parse_args()

    if args.command == "list":
        for path in scoped_files():
            print(path)
        return 0

    if args.command == "lint":
        return run_in_scope(["lint"])

    raise AssertionError(f"unhandled command: {args.command}")


if __name__ == "__main__":
    raise SystemExit(main())
