#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import fnmatch
import subprocess
import sys
from pathlib import Path

"""
cover.py runs `go test` for all packages in a Go module, produces a coverage
profile, and post-processes it to exclude generated and mock code.

The script executes `go test ./...` with `-coverpkg=./...` so coverage is
computed across the entire module, not just the tested packages. An optional
`-race` flag enables Go’s race detector during testing.

After test execution, the generated coverprofile is filtered to remove entries
matching predefined glob patterns (e.g. mocks and generated files), ensuring
coverage metrics better reflect production code.

Usage:
    cover.py <coverprofile-path> [-race]
"""

# These are matched against the filename portion of each coverage entry
EXCLUDE_GLOBS = [
    "**/mocks.go*",
    "**/mock.go*",
    "**/mocks/*",
    "**/mocks_*.go*",
    "**/mock_*.go*",
    "**/*_mocks.go*",
    "**/*_mock.go*",
]


def excluded_coverage_path(path: str) -> bool:
    return any(fnmatch.fnmatch(path, pattern) for pattern in EXCLUDE_GLOBS)


def filter_coverprofile(coverprofile: Path) -> None:
    if not coverprofile.exists():
        print(f"coverprofile not found: {coverprofile}", file=sys.stderr)
        sys.exit(1)

    lines = coverprofile.read_text(encoding="utf-8").splitlines(True)
    if not lines:
        return

    out = []

    for i, line in enumerate(lines):
        if any(fnmatch.fnmatch(line, pattern) for pattern in EXCLUDE_GLOBS):
            continue

        out.append(line)

    coverprofile.write_text("".join(out), encoding="utf-8")


def main():
    if len(sys.argv) > 3:
        print("usage: cover.py <coverprofile-path>", file=sys.stderr)
        sys.exit(2)

    coverprofile = Path(sys.argv[1]).resolve()
    enable_race = len(sys.argv) == 3 and sys.argv[2] == "-race"

    if len(sys.argv) == 3 and not enable_race:
        print("unknown option: expected -race", file=sys.stderr)
        sys.exit(2)

    coverprofile.parent.mkdir(parents=True, exist_ok=True)

    cmd = [
        "go",
        "test",
        "-tags=duckdb_arrow",
        "./...",
        f"-coverprofile={coverprofile}",
        "-coverpkg=./...",  # ./.. = all packages in this module
    ]

    if enable_race:
        cmd.insert(2, "-race")

    try:
        subprocess.run(cmd, check=True)
    except subprocess.CalledProcessError as e:
        sys.exit(e.returncode)

    # Post-filter the generated coverage profile using EXCLUDE_GLOBS.
    filter_coverprofile(coverprofile)


if __name__ == "__main__":
    main()
