# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import shutil
import stat
from pathlib import Path


def clean(path: Path) -> None:
    """
    Delete a file or directory if it exists.

    - If `path` does not exist: no-op.
    - If `path` is a directory: deletes it recursively.
    - If `path` is a file/symlink: unlinks it.

    This is intentionally small and script-friendly (no logging, no prompts).
    """
    if not path.exists():
        return

    if path.is_dir():
        shutil.rmtree(path)
        return

    path.unlink()


def ensure_executable(path: Path) -> None:
    """
    Ensure that `path` is executable by setting the execute bit for user, group, and others.

    Args:
        path: File path to update.

    Raises:
        FileNotFoundError: If the path does not exist.
        ValueError: If the path is a directory.
    """
    if not path.exists():
        raise FileNotFoundError(path)

    if path.is_dir():
        raise ValueError(f"cannot mark directory as executable: {path}")

    current_mode = path.stat().st_mode
    path.chmod(current_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
