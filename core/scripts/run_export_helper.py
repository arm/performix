#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Shared helpers for running apx commands and exporting recorded runs."""

from __future__ import annotations

import hashlib
import re
import subprocess
from pathlib import Path
from subprocess import CompletedProcess


class CommandFailure(RuntimeError):
    """Raised when an apx subprocess exits with a non-zero status."""

    def __init__(self, process: CompletedProcess[str]) -> None:
        self.process = process
        super().__init__(self._format_message(process))

    @staticmethod
    def _tail(text: str, limit: int = 4000) -> str:
        if len(text) <= limit:
            return text
        return text[-limit:]

    @classmethod
    def _format_message(cls, process: CompletedProcess[str]) -> str:
        command = " ".join(str(part) for part in process.args)
        stdout = cls._tail(process.stdout or "").strip() or "<no stdout>"
        stderr = cls._tail(process.stderr or "").strip() or "<no stderr>"
        return (
            f"Command failed with exit code {process.returncode}: {command}\n"
            f"STDOUT:\n{stdout}\n"
            f"STDERR:\n{stderr}"
        )


def run_cli(cmd: list[str], cwd: Path | None = None) -> CompletedProcess[str]:
    """Run an apx command and raise CommandFailure if it fails."""

    process = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    if process.returncode != 0:
        raise CommandFailure(process)
    return process


def parse_recipe_run_id(text: str) -> str:
    """Parse a recipe run id from apx text output."""

    match = re.search(r"Run ID:\s*([0-9a-fA-F\-]+)", text)
    if match:
        return match.group(1)
    raise ValueError("could not parse Run ID from command output")


def get_cli_version(cli_bin: Path) -> str:
    """Return the semantic CLI version reported by ``apx version``."""

    process = run_cli([str(cli_bin), "version"], cli_bin.parent)
    match = re.search(r"CLI version:\s*([0-9]+\.[0-9]+\.[0-9]+)", process.stdout)
    if not match:
        raise ValueError(f"could not parse CLI version from output: {process.stdout}")
    return match.group(1)


def export_run(cli_bin: Path, run_id: str, output_dir: Path) -> Path:
    """Export a run and return the expected ``<run_id>.zip`` path."""

    output_dir.mkdir(parents=True, exist_ok=True)
    run_cli([str(cli_bin), "run", "export", run_id, str(output_dir)], cli_bin.parent)
    archive = output_dir / f"{run_id}.zip"
    if not archive.is_file():
        raise FileNotFoundError(f"exported run archive not found: {archive}")
    return archive


def sha256_file(path: Path) -> str:
    """Return the lowercase hexadecimal SHA256 digest for a file."""

    digest = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()
