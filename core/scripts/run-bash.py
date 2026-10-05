#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Run a Bash command, establishing an MSYS2 UCRT64 environment on Windows."""

from __future__ import annotations

import platform
import shutil
import subprocess
import sys
from collections.abc import Sequence

from lib.msys2 import (
    find_msys2_bash,
    is_windows_host,
    to_msys2_path,
    ucrt64_environment,
)


def venv_directory() -> str:
    """Return the venv directory for the interpreter used to create it."""
    # All non-native launchers use the UCRT64 Python first on PATH. MSYS2's
    # UCRT64 Python reports ``sys.platform == "win32"``, so use the system
    # name to keep its environment separate from native Windows Python.
    return "env-windows" if platform.system().casefold() == "windows" else "env-ucrt64"


def main(arguments: Sequence[str] | None = None) -> int:
    """Run the requested Bash command and return its exit code."""
    arguments = list(sys.argv[1:] if arguments is None else arguments)
    if not arguments:
        raise RuntimeError("No Bash script or command was provided")

    if is_windows_host():
        bash = find_msys2_bash()
        environment = ucrt64_environment()
        environment["PERFORMIX_PYTHON"] = to_msys2_path(sys.executable)
        environment["PERFORMIX_VENV_DIRECTORY"] = venv_directory()
        environment["PERFORMIX_VENV_LAYOUT"] = (
            "windows" if sys.platform == "win32" else "posix"
        )
    else:
        bash_path = shutil.which("bash")
        if bash_path is None:
            raise RuntimeError("bash was not found on PATH")
        bash = bash_path
        environment = None

    result = subprocess.run([str(bash), *arguments], env=environment)
    return result.returncode


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as error:
        print(f"run-bash: {error}", file=sys.stderr)
        raise SystemExit(1) from error
