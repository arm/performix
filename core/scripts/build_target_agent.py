#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import os
import shutil
import subprocess
import sys
import json

GORELEASER_INSTALL_PATH = "github.com/goreleaser/goreleaser/v2@latest"
REQUIRED_TOOLCHAIN = "go1.26.5+auto"

def parse_args():
    parser = argparse.ArgumentParser(
        description="Ensure goreleaser v2 is installed and run a release."
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

def go_install_dirs():
    """
    Directories where `go install` may drop binaries, in priority order:
      1) GOBIN (if set)
      2) <path>/bin for the first path in GOPATH
    """
    try:
        out = subprocess.check_output(["go", "env", "-json", "GOBIN", "GOPATH"], text=True)
        info = json.loads(out)
    except Exception:
        info = {"GOBIN": "", "GOPATH": ""}

    dirs = []
    gobin = info.get("GOBIN", "")
    if gobin:
        dirs.append(gobin)

    gopath = info.get("GOPATH", "")
    if gopath:
        first = gopath.split(os.pathsep)[0]  # go install uses the first entry
        if first:
            dirs.append(os.path.join(first, "bin"))

    return dirs

def find_goreleaser():
    """
    Checks for goreleaser in PATH and common `go install` directories.
    """
    exe = shutil.which("goreleaser")
    if exe:
        print(f"goreleaser found in PATH: {exe}")
        return exe

    for d in go_install_dirs():
        cand = os.path.join(d, "goreleaser")
        if os.path.isfile(cand) and os.access(cand, os.X_OK):
            print(f"goreleaser found at: {cand}")
            return cand
    return None

def install_goreleaser_with_go():
    """
    Installs goreleaser via `go install`.
    """
    env = os.environ.copy()
    env["GOTOOLCHAIN"] = REQUIRED_TOOLCHAIN
    try:
        subprocess.run(["go", "install", GORELEASER_INSTALL_PATH], check=True, env=env)
    except subprocess.CalledProcessError as e:
        print(f"Failed to install goreleaser (exit code {e.returncode})", file=sys.stderr)
        return None
    return find_goreleaser()

def ensure_goreleaser_installed():
    """
    Returns the path to the goreleaser executable, installing it if needed.
    """
    exe = find_goreleaser()
    if exe:
        return exe
    print("goreleaser not found - attempting install via `go install`")
    if not shutil.which("go"):
        print("go not found. Please install go and ensure it's on your PATH", file=sys.stderr)
        sys.exit(1)
    exe = install_goreleaser_with_go()
    if exe:
        print(f"goreleaser installed to {exe}")
        return exe
    print("goreleaser still not found. Please manually install goreleaser \
           and ensure it's on your PATH or a 'go install' directory", file=sys.stderr)
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
    goreleaser_path = ensure_goreleaser_installed()
    run_goreleaser(
        executable_path=goreleaser_path,
        config_path=args.config,
        snapshot=args.snapshot,
        no_inject=args.no_inject,
        no_sign=args.no_sign,
    )

if __name__ == "__main__":
    main()
