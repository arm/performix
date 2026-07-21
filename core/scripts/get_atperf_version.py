#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Print the atperf version
"""

import shutil
import subprocess
import sys
from pathlib import Path

def ensure_go():
    if shutil.which("go") is None:
        print("go not found. Please install go and ensure it's on your PATH", file=sys.stderr)
        sys.exit(1)

def get_version():
    script_dir = Path(__file__).resolve().parent
    ver_dir = (script_dir / ".." / "atperf-version").resolve()

    if not ver_dir.is_dir():
        print(f"expected version directory not found: {ver_dir}", file=sys.stderr)
        sys.exit(1)

    try:
        out = subprocess.check_output(
            ["go", "run", "."],
            cwd=str(ver_dir),
            text=True,
        )
    except subprocess.CalledProcessError as e:
        print("failed to run 'go run .' in:", ver_dir, file=sys.stderr)
        print(e.output, file=sys.stderr, end="")
        sys.exit(e.returncode)

    version = out.strip()
    if not version:
        print("version output was empty", file=sys.stderr)
        sys.exit(1)

    return version

def main():
    ensure_go()
    version = get_version()
    print(version)

if __name__ == "__main__":
    main()
