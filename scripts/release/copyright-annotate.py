#!/usr/bin/env python3
# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Conservatively add SPDX copyright and license headers to first-party files.

First-party files are files authored for Performix by Arm and can use the
repository's default Arm copyright and Apache-2.0 license header.
Third-party files are files copied from, generated from, or distributed on
behalf of external projects. This script deliberately does not add Arm's
default header to those files because doing so could misstate ownership or
licensing; handle them with their existing notices, a `.license` sidecar file, or
REUSE.toml instead.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
HEADER_FILE_NAME = "copyright-license-header.txt"
TEMPLATE_NAME = "compact-spdx"
HEADER_FILE = REPO_ROOT / HEADER_FILE_NAME
COPYRIGHT_KEY = "SPDX-FileCopyrightText"
LICENSE_KEY = "SPDX-License-Identifier"

EXCLUDED_PREFIXES = (
    # These paths are not safe for blanket first-party annotation. Some contain
    # third-party or externally licensed content, where Arm's default header
    # would misstate ownership or licensing; others are outside this repo's
    # current copyright/license scope and should be handled separately.
    ".agents/",
    ".vscode/",
    "gui/",
    "LICENSES/",
    "core/license_terms/",
    "core/scripts/tpip/license-texts/",
)

EXCLUDED_EXACT = {
    "REUSE.toml",
    "core/LICENSE",
    "core/apap-cli/tools-builtin/sysutil-timeline/LICENSE",
    "core/package-lock.json",
}

GENERATED_SUFFIXES = (
    ".pb.go",
    "_grpc.pb.go",
)

DEFAULT_EXTENSIONS = {
    ".avif",
    ".css",
    ".gif",
    ".go",
    ".html",
    ".htm",
    ".ico",
    ".jpeg",
    ".jpg",
    ".jinja2",
    ".json5",
    ".js",
    ".jsx",
    ".md",
    ".png",
    ".proto",
    ".py",
    ".sh",
    ".sql",
    ".svg",
    ".toml",
    ".ts",
    ".tsx",
    ".webp",
    ".xml",
    ".xsd",
    ".xsl",
    ".yaml",
    ".yml",
}

HASH_STYLE_EXTENSIONS = {
    ".ps1",
    ".robot",
    ".resource",
}

HASH_STYLE_FILENAMES = {
    "bootstrap",
    "makefile",
    "requirements-dev.txt",
}

DEFAULT_STYLE_FILENAMES = {
    "go.mod",
}

DOT_LICENSE_FILENAMES = {
    "go.sum",
}


def git_files() -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "-z"],
        cwd=REPO_ROOT,
        check=True,
        stdout=subprocess.PIPE,
    )
    return [path for path in result.stdout.decode().split("\0") if path]


def is_in_scope(path: str) -> bool:
    if path in EXCLUDED_EXACT:
        return False
    if any(path.startswith(prefix) for prefix in EXCLUDED_PREFIXES):
        return False
    if any(path.endswith(suffix) for suffix in GENERATED_SUFFIXES):
        return False
    if path == "core/atperf-agent/systeminfo/armcpus/cpus.go":
        return False
    if path.startswith("core/clients/go/mocks/") and path.endswith(".go"):
        return False
    if "/regressiontests/test-data/" in path:
        return False
    if "/tests/fixtures/" in path:
        return False
    return True


def batch(items: list[str], size: int = 100) -> list[list[str]]:
    return [items[index : index + size] for index in range(0, len(items), size)]


def header_defaults() -> tuple[list[str], str]:
    copyrights: list[str] = []
    license_id = ""

    for line in HEADER_FILE.read_text(encoding="utf-8").splitlines():
        key, separator, value = line.partition(":")
        if not separator:
            continue
        if key == COPYRIGHT_KEY:
            copyrights.append(value.strip())
        elif key == LICENSE_KEY:
            license_id = value.strip()

    if not copyrights or not license_id:
        raise RuntimeError(f"Could not find SPDX defaults in {HEADER_FILE}")
    return copyrights, license_id


def annotate(
    paths: list[str],
    *,
    copyrights: list[str],
    license_id: str,
    style: str | None = None,
    force_dot_license: bool = False,
) -> None:
    if not paths:
        return
    base_command = [
        sys.executable,
        "-m",
        "reuse",
        "annotate",
        "--exclude-year",
        "--skip-existing",
        "--template",
        TEMPLATE_NAME,
    ]
    if force_dot_license:
        base_command.append("--force-dot-license")
    else:
        base_command.append("--fallback-dot-license")
    for copyright in copyrights:
        base_command.extend(["--copyright", copyright])
    base_command.extend(["--license", license_id])
    if style:
        base_command.extend(["--style", style])
    for group in batch(paths):
        subprocess.run([*base_command, *group], cwd=REPO_ROOT, check=True)


def main() -> int:
    copyrights, license_id = header_defaults()
    default_style: list[str] = []
    hash_style: list[str] = []
    dot_license: list[str] = []

    for path in git_files():
        if not is_in_scope(path):
            continue

        candidate = REPO_ROOT / path
        suffix = candidate.suffix.lower()
        filename = candidate.name.lower()

        if filename in DOT_LICENSE_FILENAMES:
            dot_license.append(path)
        elif suffix in HASH_STYLE_EXTENSIONS or filename in HASH_STYLE_FILENAMES:
            hash_style.append(path)
        elif suffix in DEFAULT_EXTENSIONS or filename in DEFAULT_STYLE_FILENAMES:
            default_style.append(path)

    annotate(default_style, copyrights=copyrights, license_id=license_id)
    annotate(hash_style, copyrights=copyrights, license_id=license_id, style="python")
    annotate(
        dot_license,
        copyrights=copyrights,
        license_id=license_id,
        force_dot_license=True,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
