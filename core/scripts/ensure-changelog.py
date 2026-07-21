#!/usr/bin/env python3
# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Ensure a root changelog exists for local GoReleaser builds."""

import sys
from pathlib import Path


def log(message: str) -> None:
    print(f"[ensure-changelog] {message}")


def ensure_changelog(root_dir: Path) -> None:
    changelog_path = root_dir / "CHANGELOG.md"
    if changelog_path.exists():
        if changelog_path.is_dir():
            raise RuntimeError(
                f"CHANGELOG.md exists but is a directory: {changelog_path}"
            )
        log(f"CHANGELOG.md already exists at {changelog_path}")
        return

    changelog_path.write_text("", encoding="utf-8")
    log(f"Created empty CHANGELOG.md at {changelog_path}")


def main() -> None:
    root_dir = Path(__file__).resolve().parents[2]
    ensure_changelog(root_dir)


if __name__ == "__main__":
    try:
        main()
        log("Success")
    except Exception as exc:  # pragma: no cover - defensive top-level guard
        print(f"Error: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc
