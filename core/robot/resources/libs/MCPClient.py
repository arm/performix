# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Robot keywords for exercising an MCP server over stdio."""

import asyncio
import json
import os
from pathlib import Path
import platform
import signal
import shutil
import socket
import tempfile
import time

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client
import psutil
from robot.api import SkipExecution
from robot.api.deco import keyword, library

from mcp_test_clients import (
    isolated_environment,
    wait_for_pid_file_count,
)

SUPPORTED_MCP_CLIENTS = (
    "antigravity",
    "claude-code",
    "claude-desktop",
    "codex",
    "cursor",
    "vscode",
)


class _MCPStderr:
    """Own the client side of the MCP process's stderr destination."""

    def __init__(self, stream, read_fd: int | None = None):
        self.stream = stream
        self.read_fd = read_fd
        self.is_pipe = read_fd is not None

    def close_reader(self):
        """Close the pipe reader once, leaving subsequent calls harmless."""
        if self.read_fd is not None:
            os.close(self.read_fd)
            self.read_fd = None


@library(scope="TEST", auto_keywords=False)
class MCPClient:
    """Call MCP tools through a real ``apx`` process."""

    def __init__(self):
        self._registration_directory: Path | None = None

    @keyword("Run MCP And Verify Engine Lifecycle")
    def run_mcp_and_verify_engine_lifecycle(
        self,
        apx_binary: str,
        tool_name: str | None = None,
        interrupt_cleanup: bool = False,
        broken_stderr_cleanup: bool = False,
    ):
        """Run MCP and verify that its engine stops with the session.

        ``broken_stderr_cleanup`` closes the client's stderr read end before
        its stdin writer. This reproduces clients such as Codex.app closing
        all MCP pipes together and verifies that a resulting broken-pipe write
        cannot terminate MCP before it shuts down the engine.
        """
        with tempfile.TemporaryDirectory(prefix="apx-mcp-robot-") as directory:
            test_root = Path(directory)
            environment, state_directory = isolated_environment(test_root)
            stderr_path = test_root / "mcp-stderr.log"
            stderr_target = None

            try:
                if broken_stderr_cleanup:
                    stderr_read_fd, stderr_write_fd = os.pipe()
                    stderr_target = _MCPStderr(
                        os.fdopen(stderr_write_fd, "w", encoding="utf-8"),
                        stderr_read_fd,
                    )
                else:
                    stderr_target = _MCPStderr(
                        stderr_path.open("w", encoding="utf-8")
                    )

                with stderr_target.stream:
                    return asyncio.run(
                        self._run_mcp(
                            apx_binary,
                            tool_name,
                            environment,
                            state_directory,
                            stderr_target=stderr_target,
                            interrupt_cleanup=interrupt_cleanup,
                        )
                    )
            except Exception as error:
                if stderr_path.exists():
                    stderr = stderr_path.read_text(encoding="utf-8")
                    raise AssertionError(f"{error}\nMCP stderr:\n{stderr}") from error
                raise
            finally:
                if stderr_target is not None:
                    stderr_target.close_reader()

    async def _run_mcp(
        self,
        apx_binary: str,
        tool_name: str | None,
        environment: dict,
        state_directory: Path,
        *,
        stderr_target: _MCPStderr,
        interrupt_cleanup: bool,
    ):
        parameters = StdioServerParameters(
            command=apx_binary,
            args=["mcp", "start"],
            env=environment,
        )
        engine_process = None
        try:
            async with stdio_client(parameters, errlog=stderr_target.stream) as (
                read,
                write,
            ):
                async with ClientSession(read, write) as session:
                    await session.initialize()
                    pid_files = await wait_for_pid_file_count(
                        state_directory, 1
                    )
                    if pid_files[0].name.endswith("_9000.pid"):
                        raise AssertionError(
                            f"MCP engine used the default daemon PID file: {pid_files[0]}"
                        )
                    engine_process = self._running_engine_process(pid_files[0])
                    if interrupt_cleanup:
                        mcp_process = self._running_mcp_process(apx_binary)
                        # Stop MCP before signalling its group so it cannot run
                        # its deferred engine shutdown. The engine must stop
                        # without help from MCP.
                        os.kill(mcp_process.pid, signal.SIGSTOP)
                        os.killpg(os.getpgid(mcp_process.pid), signal.SIGTERM)
                        os.kill(mcp_process.pid, signal.SIGKILL)
                        result = True
                    else:
                        if tool_name is None:
                            raise AssertionError("MCP tool name is required")
                        result = await session.call_tool(tool_name, arguments={})
                        if result.isError:
                            raise AssertionError(
                                f"MCP tool {tool_name} returned an error"
                            )

                    if stderr_target.read_fd is not None:
                        # Close the stderr reader before stdio_client closes
                        # stdin. This reproduces clients which close all MCP
                        # pipes while the SDK is processing stdin EOF.
                        stderr_target.close_reader()

            await self._wait_for_process_exit(engine_process)
            await wait_for_pid_file_count(state_directory, 0)
            if stderr_target.is_pipe:
                self._verify_broken_pipe_shutdown_log(environment["APXD_LOG_FILE"])
            if interrupt_cleanup:
                return result
            return result.structuredContent
        finally:
            if engine_process is not None and engine_process.is_running():
                engine_process.terminate()
                try:
                    engine_process.wait(timeout=5)
                except psutil.TimeoutExpired:
                    engine_process.kill()

    @staticmethod
    def _running_mcp_process(apx_binary: str):
        expected_binary = Path(apx_binary).resolve()
        for process in psutil.Process().children():
            try:
                command = process.cmdline()
            except psutil.Error:
                continue
            if (
                len(command) >= 3
                and Path(command[0]).resolve() == expected_binary
                and command[1:3] == ["mcp", "start"]
            ):
                return process
        raise AssertionError("No running MCP process was found")

    @keyword("Create MCP Registration Fixture")
    def create_mcp_registration_fixture(
        self,
        client_id: str,
        mode: str = "mock",
        scenario: str = "normal",
    ):
        """Create isolated directories and ports for CLI registration tests."""
        if self._registration_directory is not None:
            raise AssertionError("an MCP registration fixture already exists")

        self._registration_directory = Path(
            tempfile.mkdtemp(prefix="apx-mcp-registration-")
        )
        test_root = self._registration_directory
        try:
            environment, _ = isolated_environment(test_root)
            environment["XDG_CONFIG_HOME"] = str(test_root / ".config")
            if mode == "mock":
                environment.update(
                    {
                        "APPDATA": str(test_root / "AppData" / "Roaming"),
                        "LOCALAPPDATA": str(test_root / "AppData" / "Local"),
                        "ProgramFiles": str(test_root / "Program Files"),
                    }
                )
                config_path = self._configure_mock_client(
                    client_id, test_root, environment, scenario
                )
            elif mode == "real":
                if scenario != "normal":
                    raise SkipExecution(
                        "MCP diagnostic problem fixtures require mock mode"
                    )
                config_path = self._configure_real_client(
                    client_id, test_root, environment
                )
            else:
                raise ValueError(f"unsupported MCP client test mode: {mode}")
        except Exception:
            self.remove_mcp_registration_fixture()
            raise

        server_port = self._available_tcp_port()
        auth_port = self._available_tcp_port()
        while auth_port == server_port:
            auth_port = self._available_tcp_port()

        return {
            "environment": environment,
            "server_port": server_port,
            "auth_port": auth_port,
            "configuration_path": str(config_path) if config_path else "",
            "configuration_root": (
                "servers" if client_id == "vscode" else "mcpServers"
            )
            if config_path
            else "",
        }

    @keyword("Remove MCP Registration Fixture")
    def remove_mcp_registration_fixture(self):
        """Remove directories created for a CLI registration test."""
        if self._registration_directory is not None:
            shutil.rmtree(self._registration_directory)
            self._registration_directory = None

    @staticmethod
    def _available_tcp_port():
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            listener.bind(("127.0.0.1", 0))
            return listener.getsockname()[1]

    @staticmethod
    def _configure_mock_client(
        client_id: str,
        test_root: Path,
        environment: dict,
        scenario: str,
    ):
        if scenario not in {"normal", "conflict", "unreadable", "undetected"}:
            raise ValueError(f"unsupported MCP client fixture scenario: {scenario}")
        if scenario == "undetected":
            if client_id != "codex":
                raise ValueError("the undetected fixture currently uses Codex")
            binary_directory = test_root / "bin"
            binary_directory.mkdir()
            environment["PATH"] = str(binary_directory)
            return None

        client_ids = SUPPORTED_MCP_CLIENTS if client_id == "all" else (client_id,)
        config_path = None
        for configured_client in client_ids:
            candidate = MCPClient._configure_normal_mock_client(
                configured_client, test_root, environment
            )
            if configured_client == client_id:
                config_path = candidate

        # Do not let real clients installed on the host affect mock-mode
        # discovery or --all selection.
        environment["PATH"] = str(test_root / "bin")
        if scenario == "conflict":
            if client_id != "codex":
                raise ValueError("the conflict fixture currently uses Codex")
            state_path = MCPClient._native_stub_state_path(environment, "codex")
            state_path.write_text(
                json.dumps(
                    {
                        "arm-performix": {
                            "type": "stdio",
                            "command": "/unexpected/apx",
                            "args": ["mcp", "start"],
                        }
                    }
                ),
                encoding="utf-8",
            )
        elif scenario == "unreadable":
            if client_id != "antigravity" or config_path is None:
                raise ValueError(
                    "the unreadable fixture currently uses Antigravity"
                )
            config_path.write_text('{"mcpServers":', encoding="utf-8")
        return config_path

    @staticmethod
    def _configure_normal_mock_client(
        client_id: str,
        test_root: Path,
        environment: dict,
    ):
        if client_id in {"claude-code", "codex", "vscode"}:
            MCPClient._copy_native_client_stub(
                client_id, test_root, environment
            )
            if client_id == "vscode":
                config_path = MCPClient._vscode_config_path(
                    test_root, environment
                )
                environment["APAP_MCP_STUB_VSCODE_CONFIG"] = str(config_path)
                return config_path
            return None

        config_path = MCPClient._file_client_config_path(
            client_id, test_root, environment
        )
        config_path.parent.mkdir(parents=True)
        host_os = platform.system()
        if host_os == "Linux" and client_id in {"cursor", "claude-desktop"}:
            MCPClient._copy_native_client_stub(
                client_id, test_root, environment
            )
            return config_path
        evidence = {
            "cursor": {
                "Darwin": test_root / "Applications" / "Cursor.app",
                "Windows": Path(environment["LOCALAPPDATA"]).joinpath(
                    "Programs", "cursor", "Cursor.exe"
                ),
            },
            "claude-desktop": {
                "Darwin": test_root / "Applications" / "Claude.app",
                "Windows": Path(environment["LOCALAPPDATA"]).joinpath(
                    "Microsoft", "WindowsApps", "claude.exe"
                ),
            },
            "antigravity": {
                "Darwin": test_root / "Applications" / "Antigravity.app",
                "Windows": Path(environment["LOCALAPPDATA"]).joinpath(
                    "agy", "bin", "agy.exe"
                ),
                "Linux": test_root / ".gemini" / "antigravity",
            },
        }
        try:
            evidence_path = evidence[client_id][host_os]
        except KeyError as error:
            detail = (
                f"unsupported MCP registration fixture: {client_id} "
                f"on {host_os}"
            )
            raise ValueError(detail) from error
        if host_os == "Windows" and client_id in {
            "cursor",
            "antigravity",
            "claude-desktop",
        }:
            evidence_path.parent.mkdir(parents=True, exist_ok=True)
            evidence_path.touch()
        else:
            evidence_path.mkdir(parents=True, exist_ok=True)
        return config_path

    @staticmethod
    def _configure_real_client(
        client_id: str,
        test_root: Path,
        environment: dict,
    ):
        command = {"claude-code": "claude", "codex": "codex"}.get(client_id)
        if command:
            if shutil.which(command, path=environment.get("PATH")) is None:
                raise SkipExecution(
                    f"{command} is not installed or is not on PATH"
                )
            config_directory = test_root / client_id
            config_directory.mkdir()
            variable = (
                "CLAUDE_CONFIG_DIR"
                if client_id == "claude-code"
                else "CODEX_HOME"
            )
            environment[variable] = str(config_directory)
            return None

        if client_id == "vscode":
            raise SkipExecution(
                "VS Code has no isolated real-client profile supported by "
                "this suite"
            )

        host_os = platform.system()
        if host_os == "Linux":
            raise SkipExecution(
                f"{client_id} has no separate Linux application evidence "
                "and isolated configuration"
            )

        if host_os == "Darwin":
            application = {
                "cursor": Path("/Applications/Cursor.app"),
                "claude-desktop": Path("/Applications/Claude.app"),
                "antigravity": Path("/Applications/Antigravity.app"),
            }.get(client_id)
            if application is None or not application.exists():
                raise SkipExecution(
                    f"{client_id} is not installed in /Applications"
                )
        elif host_os == "Windows":
            MCPClient._configure_real_windows_application(
                client_id, test_root, environment
            )
        else:
            raise SkipExecution(
                f"real {client_id} testing is unsupported on {host_os}"
            )

        config_path = MCPClient._file_client_config_path(
            client_id, test_root, environment
        )
        config_path.parent.mkdir(parents=True)
        return config_path

    @staticmethod
    def _configure_real_windows_application(
        client_id: str,
        test_root: Path,
        environment: dict,
    ):
        appdata = Path(os.environ.get("APPDATA", ""))
        local_appdata = Path(os.environ.get("LOCALAPPDATA", ""))
        program_files = Path(os.environ.get("ProgramFiles", ""))
        antigravity_candidates = [
            local_appdata / "agy" / "bin" / "agy.exe",
            local_appdata.joinpath(
                "Programs", "antigravity-ide", "Antigravity IDE.exe"
            ),
            local_appdata.joinpath(
                "Programs", "Antigravity", "Antigravity.exe"
            ),
        ]
        agy_executable = shutil.which("agy", path=os.environ.get("PATH"))
        if agy_executable:
            antigravity_candidates.append(Path(agy_executable))
        candidates = {
            "cursor": [
                local_appdata / "Programs" / "cursor" / "Cursor.exe",
                program_files / "Cursor" / "Cursor.exe",
            ],
            "antigravity": antigravity_candidates,
            "claude-desktop": [
                local_appdata.joinpath(
                    "Microsoft", "WindowsApps", "claude.exe"
                ),
                local_appdata.joinpath(
                    "Programs", "Claude", "Claude.exe"
                ),
            ],
        }.get(client_id, [])
        if not any(path.exists() for path in candidates):
            raise SkipExecution(
                f"{client_id} is not installed in a standard location"
            )

        if client_id == "claude-desktop":
            environment["APPDATA"] = str(test_root / "AppData" / "Roaming")
            environment["LOCALAPPDATA"] = str(local_appdata)
        else:
            environment["APPDATA"] = str(appdata)
            environment["LOCALAPPDATA"] = str(local_appdata)
        environment["ProgramFiles"] = str(program_files)

    @staticmethod
    def _file_client_config_path(
        client_id: str,
        test_root: Path,
        environment: dict,
    ):
        if client_id == "cursor":
            return test_root / ".cursor" / "mcp.json"
        if client_id == "antigravity":
            return test_root / ".gemini" / "config" / "mcp_config.json"
        if client_id == "claude-desktop":
            host_os = platform.system()
            if host_os == "Darwin":
                return test_root.joinpath(
                    "Library",
                    "Application Support",
                    "Claude",
                    "claude_desktop_config.json",
                )
            if host_os == "Windows":
                return Path(environment["APPDATA"]).joinpath(
                    "Claude", "claude_desktop_config.json"
                )
            return Path(environment["XDG_CONFIG_HOME"]).joinpath(
                "Claude", "claude_desktop_config.json"
            )
        raise ValueError(f"unsupported file-managed MCP client: {client_id}")

    @staticmethod
    def _vscode_config_path(test_root: Path, environment: dict):
        host_os = platform.system()
        if host_os == "Darwin":
            return test_root.joinpath(
                "Library", "Application Support", "Code", "User", "mcp.json"
            )
        if host_os == "Windows":
            return Path(environment["APPDATA"]) / "Code" / "User" / "mcp.json"
        return Path(environment["XDG_CONFIG_HOME"]).joinpath(
            "Code", "User", "mcp.json"
        )

    @staticmethod
    def _copy_native_client_stub(
        client_id: str,
        test_root: Path,
        environment: dict,
    ):
        names = {
            "claude-code": "claude",
            "claude-desktop": "claude-desktop",
            "codex": "codex",
            "cursor": "cursor",
            "vscode": "code",
        }
        suffix = ".exe" if platform.system() == "Windows" else ""
        name = names[client_id] + suffix
        binary_directory = test_root / "bin"
        binary_directory.mkdir(exist_ok=True)
        binary = binary_directory / name
        source = Path(__file__).resolve().parents[1].joinpath(
            "files", "mcp-client-stub", f"mcp-client-stub{suffix}"
        )
        if not source.is_file():
            raise AssertionError(
                "MCP client stub is not built. Run `task core:test:robot` "
                "from the repository root."
            )
        shutil.copy2(source, binary)
        environment["APAP_MCP_STUB_STATE"] = str(
            test_root / "native-client-state"
        )

    @staticmethod
    def _native_stub_state_path(environment: dict, client_id: str):
        command = {"claude-code": "claude", "codex": "codex"}[client_id]
        prefix = environment["APAP_MCP_STUB_STATE"]
        return Path(f"{prefix}-{command}.json")

    @staticmethod
    def _verify_broken_pipe_shutdown_log(log_path: str):
        log_contents = Path(log_path).read_text(encoding="utf-8")
        for expected in (
            "MCP protocol complete",
            "Engine daemon shutdown complete",
        ):
            if expected not in log_contents:
                raise AssertionError(
                    f"MCP log does not contain {expected!r}:\n{log_contents}"
                )

    @staticmethod
    def _running_engine_process(pid_file: Path):
        try:
            pid = int(pid_file.read_text(encoding="utf-8"))
            process = psutil.Process(pid)
            command = process.cmdline()
        except (OSError, ValueError, psutil.Error) as error:
            raise AssertionError(
                f"No running process was found for MCP engine PID file {pid_file}"
            ) from error

        if not process.is_running():
            raise AssertionError(f"MCP engine process {pid} is not running")
        if command[1:4] != ["daemon", "start", "--block"]:
            raise AssertionError(
                f"MCP engine PID {pid} has unexpected command line: {command}"
            )
        return process

    @staticmethod
    async def _wait_for_process_exit(process: psutil.Process):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if not process.is_running():
                return
            await asyncio.sleep(0.05)
        raise AssertionError(f"MCP engine process {process.pid} is still running")
