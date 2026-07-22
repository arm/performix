#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

'''
Python script to generate a Third-Party-Intellectual-Property (TPIP) report for
a Go project using `go-licenses`.
'''

import argparse
import os
import shutil
import subprocess
import sys
from datetime import datetime
from pathlib import Path


def log(message: str) -> None:
    print(f"[generate-tpip-report] {message}")



def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate TPIP report for a repository.")
    parser.add_argument(
        "repo",
        nargs="?",
        default=".",
        help="Path to the repository root (default: current directory).",
    )
    return parser.parse_args()


def resolve_go_licenses() -> str:
    '''
    Attempts to find the go-licenses binary by (1st) checking the system PATH,
    then (2nd) checking the GOBIN and GOPATH environment variables.

    :return: Path to the go-licenses executable, or empty string if not found.
    :rtype: str
    '''
    path = shutil.which("go-licenses")
    if path:
        return path

    try:
        out = subprocess.check_output(["go", "env", "GOBIN", "GOPATH"], text=True).splitlines()
        gobin, gopath = out[0].strip(), out[1].strip()
    except (OSError, subprocess.CalledProcessError, ValueError):
        log("go env failed; GOBIN=<unavailable> GOPATH=<unavailable>")
        return ""

    search_dirs = [gobin] if gobin else []
    for p in gopath.split(os.pathsep):
        if p:
            search_dirs.append(str(Path(p) / "bin"))

    for d in search_dirs:
        found = shutil.which("go-licenses", path=d)
        if found:
            return found

    log(f"go-licenses not found; GOBIN='{gobin}' GOPATH='{gopath}'")
    return ""


def main() -> int:
    args = parse_args()
    root_dir = Path(args.repo).resolve()

    report_path = Path(
        os.getenv("TPIP_REPORT_PATH", str(root_dir / "license_terms" / "third_party_licenses.txt"))
    )
    template_path = Path(
        os.getenv("TPIP_TEMPLATE_PATH", str(root_dir / "scripts" / "templates" / "tpip-license.template"))
    )
    ignore_path = os.getenv("TPIP_IGNORE_PATH", "github.com/Arm-Debug/apap-cli")
    apap_cli_dir = Path(os.getenv("TPIP_APAP_CLI_DIR", str(root_dir / "apap-cli")))

    if not apap_cli_dir.exists():
        raise RuntimeError(f"apap-cli directory not found at {apap_cli_dir}")
    if not template_path.exists():
        raise RuntimeError(f"TPIP template not found at {template_path}")

    go_licenses = resolve_go_licenses()
    if not go_licenses:
        message = "go-licenses not found in PATH or Go bin dirs; "
        print(message + "cannot generate TPIP report.", file=sys.stderr)
        return 1

    report_path.parent.mkdir(parents=True, exist_ok=True)

    cmd = [
        go_licenses,
        "report",
        ".",
        "--ignore",
        ignore_path,
        "--template",
        str(template_path),
    ]
    log(f"Running: {' '.join(cmd)} (cwd={apap_cli_dir})")
    log(f"Generating TPIP report at {report_path}")
    with report_path.open("w", encoding="utf-8") as report_file:
        result = subprocess.run(
            cmd,
            cwd=str(apap_cli_dir),
            stdout=report_file,
            stderr=subprocess.PIPE,
            text=True,
        )
    if result.returncode != 0:
        print(result.stderr, file=sys.stderr)
        return result.returncode

    timestamp = datetime.now().strftime("%Y/%m/%d %T")
    with report_path.open("a", encoding="utf-8") as handle:
        handle.write(f"{timestamp}\n")
    
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
        log("Success")
    except Exception as exc:
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)
