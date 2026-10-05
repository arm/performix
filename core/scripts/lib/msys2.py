# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Helpers for launching commands in a predictable MSYS2 UCRT64 environment."""

from __future__ import annotations

import ntpath
import os
import platform
from collections.abc import Mapping
from pathlib import Path


MSYS2_ROOT_ENV = "MSYS2_ROOT"
MISE_BASH_PATH_ENV = "MISE_BASH_PATH"
DEFAULT_MSYS2_ROOTS = (
    Path(r"C:\msys64"),
    Path(r"C:\tools\msys64"),
)
REQUIRED_UCRT64_FILES = (
    Path("usr/bin/bash.exe"),
    Path("usr/bin/find.exe"),
    Path("usr/bin/sort.exe"),
    Path("ucrt64/bin/gcc.exe"),
)


def is_windows_host() -> bool:
    """Return whether the current host uses Windows process semantics."""
    system = platform.system().lower()
    return system.startswith(("mingw", "msys", "cygwin", "windows"))


def _root_from_bash_path(bash_path: str) -> Path | None:
    bash = Path(bash_path).expanduser()
    path_suffix = (
        bash.name.lower(),
        bash.parent.name.lower(),
        bash.parent.parent.name.lower(),
    )
    if path_suffix == ("bash.exe", "bin", "usr"):
        return bash.parents[2]
    return None


def _is_msys2_ucrt64_root(root: Path) -> bool:
    return all((root / relative_path).is_file() for relative_path in REQUIRED_UCRT64_FILES)


def _missing_ucrt64_files(root: Path) -> list[Path]:
    return [
        relative_path
        for relative_path in REQUIRED_UCRT64_FILES
        if not (root / relative_path).is_file()
    ]


def find_msys2_root(environment: Mapping[str, str] | None = None) -> Path:
    """Locate an MSYS2 installation containing the UCRT64 environment."""
    environment = os.environ if environment is None else environment

    configured_root = environment.get(MSYS2_ROOT_ENV, "").strip()
    if configured_root:
        root = Path(configured_root).expanduser()
        if _is_msys2_ucrt64_root(root):
            return root
        missing = ", ".join(str(path).replace("\\", "/") for path in _missing_ucrt64_files(root))
        raise RuntimeError(
            f"{MSYS2_ROOT_ENV} points to {root}, but that directory does not "
            f"contain the required UCRT64 files: {missing}"
        )

    configured_bash = environment.get(MISE_BASH_PATH_ENV, "").strip()
    if configured_bash:
        root = _root_from_bash_path(configured_bash)
        if root is not None and _is_msys2_ucrt64_root(root):
            return root

    for root in DEFAULT_MSYS2_ROOTS:
        if _is_msys2_ucrt64_root(root):
            return root

    raise RuntimeError(
        "MSYS2 UCRT64 was not found. Install MSYS2 at C:\\msys64 or set "
        f"{MSYS2_ROOT_ENV} to the MSYS2 installation directory"
    )


def find_msys2_bash(environment: Mapping[str, str] | None = None) -> Path:
    """Return bash.exe from a validated MSYS2 UCRT64 installation."""
    return find_msys2_root(environment) / "usr" / "bin" / "bash.exe"


def to_msys2_path(path: str | os.PathLike[str]) -> str:
    """Convert a Windows path to the form expected by MSYS2 programs."""
    value = os.fspath(path)
    drive, tail = ntpath.splitdrive(value)
    tail = tail.replace("\\", "/")

    if drive.startswith("\\\\"):
        unc_drive = drive.replace("\\", "/")
        return f"{unc_drive}{tail}"
    if len(drive) == 2 and drive[1] == ":" and tail.startswith("/"):
        return f"/{drive[0].lower()}{tail}"
    return value.replace("\\", "/")


def _normalise_windows_path(path: str) -> str:
    return path.replace("/", "\\").rstrip("\\").casefold()


def ucrt64_environment(
    environment: Mapping[str, str] | None = None,
    root: Path | None = None,
) -> dict[str, str]:
    """Build a child environment whose PATH starts with UCRT64 and MSYS tools."""
    source = os.environ if environment is None else environment
    child = dict(source)
    root = find_msys2_root(source) if root is None else root

    existing_path = ""
    for key in list(child):
        if key.casefold() == "path":
            existing_path = child.pop(key)

    prefixes = [root / "ucrt64" / "bin", root / "usr" / "bin"]
    prefix_keys = {_normalise_windows_path(str(path)) for path in prefixes}
    inherited_parts = [
        part
        for part in existing_path.split(";")
        if part and _normalise_windows_path(part) not in prefix_keys
    ]

    child["PATH"] = ";".join([*(str(path) for path in prefixes), *inherited_parts])
    child["MSYSTEM"] = "UCRT64"
    child["CHERE_INVOKING"] = "1"
    return child
