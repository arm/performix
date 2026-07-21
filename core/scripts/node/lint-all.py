#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import subprocess
import sys
from pathlib import Path


def run(cmd):
    """Run a shell command and stream output."""
    project_dir = Path(__file__).resolve().parents[2]
    result = subprocess.run(cmd, cwd=project_dir)
    if result.returncode != 0:
        sys.exit(result.returncode)


def main():
    parser = argparse.ArgumentParser(description="Run formatter for JavaScript files.")
    parser.add_argument(
        "--fix", action="store_true", help="Run format instead of format:check"
    )
    args = parser.parse_args()

    if args.fix:
        run(["npm", "run", "format"])
    else:
        run(["npm", "run", "format:check"])


if __name__ == "__main__":
    main()
