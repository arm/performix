# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import tarfile
import zipfile
from pathlib import Path


def extract_zip(zip_path: Path, dst_dir: Path) -> None:
    """
    Extract a .zip archive to `dst_dir`.

    Creates `dst_dir` if it doesn't exist.

    Args:
        zip_path: Path to the .zip file.
        dst_dir: Destination directory to extract into.
    """
    dst_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(zip_path, "r") as archive:
        archive.extractall(dst_dir)


def extract_tarball(tarball_path: Path, dst_dir: Path) -> None:
    """
    Extract a .tar.gz archive to `dst_dir`.

    Creates `dst_dir` if it doesn't exist.

    Args:
        tarball_path: Path to the .tar.gz file.
        dst_dir: Destination directory to extract into.
    """
    dst_dir.mkdir(parents=True, exist_ok=True)
    with tarfile.open(tarball_path, "r:gz") as archive:
        archive.extractall(dst_dir)


def create_tarball(source_dir: Path, out_archive_path: Path) -> None:
    """
    Create a gzipped tarball from the immediate contents of `source_dir`.

    The resulting archive contains each direct child of `source_dir` at the
    archive root (i.e., it does NOT nest everything under a top-level folder).

    If `out_archive_path` already exists, it will be overwritten.

    Args:
        source_dir: Directory whose direct children should be archived.
        out_archive_path: Output .tar.gz path.
    """
    if out_archive_path.exists():
        out_archive_path.unlink()

    with tarfile.open(out_archive_path, "w:gz") as archive:
        for entry in sorted(source_dir.iterdir()):
            archive.add(entry, arcname=entry.name)
