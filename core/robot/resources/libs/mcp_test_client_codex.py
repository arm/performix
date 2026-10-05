# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Codex client for Performix MCP smoke tests."""

import asyncio
import json
import os
from pathlib import Path
import shutil
import subprocess
from typing import Any

from robot.api import logger

from mcp_test_clients import (
    PERFORMIX_MCP_SERVER,
    wait_for_pid_file_count,
)

CODEX_ENVIRONMENT_KEYS = (
    "HOME",
    "USERPROFILE",
    "XDG_STATE_HOME",
    "APXD_CONFIG_DIR",
    "APXD_DATA_DIR",
    "APXD_LOG_FILE",
    "NO_COLOR",
)


class CodexMCPTestClient:
    """Call the Performix MCP server through Codex."""

    def __init__(self):
        self._codex_binary = ""
        self._codex_environment: dict[str, str] = {}
        self._workspace = Path()
        self._state_directory = Path()

    def prepare(
        self,
        apx_binary: str,
        environment: dict[str, str],
        state_directory: Path,
        test_root: Path,
    ) -> None:
        """Configure Codex for this test suite."""
        codex_home = test_root / "codex-home"
        workspace = test_root / "workspace"
        codex_home.mkdir(exist_ok=True)
        workspace.mkdir(exist_ok=True)
        (codex_home / "config.toml").write_text(
            self._config(apx_binary),
            encoding="utf-8",
        )

        codex_binary = shutil.which("codex")
        if not codex_binary:
            raise AssertionError("Codex binary was not found on PATH")
        codex_binary = str(Path(codex_binary).resolve())
        logger.info(f"Using Codex binary: {codex_binary}", also_console=True)

        codex_environment = os.environ.copy()
        codex_environment.update(
            {key: environment[key] for key in CODEX_ENVIRONMENT_KEYS}
        )
        codex_environment["CODEX_HOME"] = str(codex_home)

        self._codex_binary = codex_binary
        self._codex_environment = codex_environment
        self._workspace = workspace
        self._state_directory = state_directory

    def run(self, prompt: str, output_dir: Path) -> list[dict[str, Any]]:
        """Run one prompt and return its Performix MCP tool calls."""
        if not self._codex_binary:
            raise AssertionError("Codex MCP test client is not prepared")

        command = [
            self._codex_binary,
            "exec",
            "--color",
            "never",
            "--json",
            "--skip-git-repo-check",
            "-C",
            str(self._workspace),
            "-",
        ]
        output_dir.mkdir(parents=True, exist_ok=True)
        output_path = output_dir / "mcp-codex-exec.jsonl"
        stderr_path = output_dir / "mcp-codex-exec.stderr"
        try:
            with output_path.open("w", encoding="utf-8") as output_file:
                with stderr_path.open("w", encoding="utf-8") as stderr_file:
                    process = subprocess.run(
                        command,
                        input=prompt,
                        stdout=output_file,
                        stderr=stderr_file,
                        check=False,
                        env=self._codex_environment,
                        text=True,
                        timeout=15 * 60,
                    )
        except subprocess.TimeoutExpired as error:
            raise AssertionError(
                "codex exec timed out after 15 minutes. Transcript: "
                f"{output_path}\nLast stderr lines:\n"
                f"{self._last_stderr_lines(stderr_path)}"
            ) from error

        if process.returncode != 0:
            raise AssertionError(
                f"codex exec failed with exit code {process.returncode}. "
                f"Transcript: {output_path}\nLast stderr lines:\n"
                f"{self._last_stderr_lines(stderr_path)}"
            )

        asyncio.run(wait_for_pid_file_count(self._state_directory, 0))
        return self._tool_calls(output_path.read_text(encoding="utf-8"))

    @staticmethod
    def _last_stderr_lines(stderr_path: Path) -> str:
        output = stderr_path.read_text(encoding="utf-8", errors="replace")
        return CodexMCPTestClient._last_lines(output)

    @staticmethod
    def _last_lines(output: str) -> str:
        lines = output.splitlines()
        return "\n".join(lines[-10:]) or "(no stderr output)"

    @staticmethod
    def _tool_calls(output: str) -> list[dict[str, Any]]:
        calls = []
        for line_number, line in enumerate(output.splitlines(), start=1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError as error:
                raise AssertionError(
                    "codex exec emitted malformed JSONL on line "
                    f"{line_number}: {error.msg}"
                ) from error
            item = event.get("item") or {}
            if (
                event.get("type") != "item.completed"
                or item.get("type") != "mcp_tool_call"
                or item.get("server") != PERFORMIX_MCP_SERVER
            ):
                continue
            result = item.get("result") or {}
            calls.append(
                {
                    "tool": item.get("tool"),
                    "arguments": item.get("arguments") or {},
                    "result": result.get("structured_content") or {},
                    "status": item.get("status"),
                }
            )
        return calls

    @staticmethod
    def _config(apx_binary: str) -> str:
        provider = ""
        proxy = os.environ.get("OPENAI_API_PROXY", "").rstrip("/")
        if proxy:
            provider = f'''model_provider = "proxy"

[model_providers.proxy]
name = "OpenAI"
base_url = {json.dumps(proxy)}
wire_api = "responses"
env_key = "OPENAI_API_KEY"

'''
        return f'''disable_response_storage = true
project_root_markers = []

{provider}
[features]
memories = false
plugins = false
shell_tool = false

[memories]
use_memories = false
generate_memories = false

[tools]
web_search = false

[mcp_servers.{PERFORMIX_MCP_SERVER}]
command = {json.dumps(apx_binary)}
args = ["mcp", "start"]
enabled = true
default_tools_approval_mode = "approve"
env_vars = {json.dumps(CODEX_ENVIRONMENT_KEYS)}
'''
