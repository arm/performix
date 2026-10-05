# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Shared support for coding-agent MCP test clients."""

import asyncio
import os
from pathlib import Path
import platform
import shutil
import time
from typing import Any, Protocol

PERFORMIX_MCP_SERVER = "arm-performix"


def isolated_environment(test_root: Path) -> tuple[dict[str, str], Path]:
    """Create isolated APX directories and return its process environment."""
    state_home = test_root / "state"
    config_home = test_root / "config"
    data_home = test_root / "data"
    for directory in (state_home, config_home, data_home):
        directory.mkdir(parents=True)

    environment = os.environ.copy()
    environment.update(
        {
            "HOME": str(test_root),
            "USERPROFILE": str(test_root),
            "XDG_STATE_HOME": str(state_home),
            "XDG_CONFIG_HOME": str(config_home),
            "XDG_DATA_HOME": str(data_home),
            "APXD_CONFIG_DIR": str(config_home),
            "APXD_DATA_DIR": str(data_home),
            "APXD_LOG_FILE": str(state_home / "apxd.log"),
            "NO_COLOR": "1",
            "APXD_LOG_LEVEL": "debug",
        }
    )

    if platform.system() == "Windows":
        state_directory = test_root / "AppData" / "Local" / "apxd"
    else:
        state_directory = state_home / "apxd"
    return environment, state_directory


def copy_known_hosts(test_root: Path) -> None:
    """Copy the user's SSH host keys into an isolated APX home directory."""
    known_hosts = Path.home() / ".ssh" / "known_hosts"
    if not known_hosts.is_file():
        return
    destination = test_root / ".ssh"
    destination.mkdir()
    shutil.copy2(known_hosts, destination / "known_hosts")


async def wait_for_pid_file_count(
    state_directory: Path, expected: int
) -> list[Path]:
    """Wait for the expected number of APX engine PID files."""
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        pid_files = sorted(state_directory.glob("*.pid"))
        if len(pid_files) == expected:
            return pid_files
        await asyncio.sleep(0.05)
    pid_files = sorted(state_directory.glob("*.pid"))
    raise AssertionError(
        f"Expected {expected} MCP engine PID files, found {pid_files}"
    )


class CodingAgentMCPTestClient(Protocol):
    """Run a Performix MCP prompt through a coding agent."""

    def prepare(
        self,
        apx_binary: str,
        environment: dict[str, str],
        state_directory: Path,
        test_root: Path,
    ) -> None:
        """Configure and authenticate the coding agent."""
        ...

    def run(self, prompt: str, output_dir: Path) -> list[dict[str, Any]]:
        """Run the prompt and return its MCP tool calls."""
        ...
