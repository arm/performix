# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Robot keywords for smoke-testing Performix MCP tools through coding agents."""

import asyncio
import json
from pathlib import Path
import socket
import subprocess
import tempfile
from typing import Any
import uuid

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client
from robot.api.deco import keyword, library

from mcp_test_client_codex import CodexMCPTestClient
from mcp_test_clients import (
    CodingAgentMCPTestClient,
    copy_known_hosts,
    isolated_environment,
    wait_for_pid_file_count,
)

COVERED_MCP_TOOLS = frozenset(
    {
        "add_target",
        "close_render_session",
        "generate_ai_insights",
        "list_recipes",
        "list_render_sessions",
        "list_runs",
        "list_targets",
        "open_render_session",
        "read_ai_insights_payload_details",
        "recipe_info",
        "run_query",
        "run_recipe",
        "search_target_processes",
    }
)


@library(scope="SUITE", auto_keywords=False)
class MCPClientSmoke:
    """Call individual Performix MCP tools through a coding agent."""

    def __init__(self):
        self._temporary_directory = None
        self._apx_binary = ""
        self._environment: dict[str, str] = {}
        self._state_directory = Path()
        self._test_root = Path()
        self._output_dir = Path()
        self._target: dict[str, Any] = {}
        self._curated_run_id = ""
        self._query_run_id = ""
        self._server_port = 0
        self._auth_port = 0
        self._client: CodingAgentMCPTestClient = CodexMCPTestClient()

    @keyword("Prepare MCP Client Smoke Tests")
    def prepare_smoke_tests(
        self,
        apx_binary: str,
        target_name: str,
        target_host: str,
        target_port: int,
        target_user: str,
        target_key: str,
        output_dir: str,
    ) -> None:
        """Create an isolated target and runs used by the smoke tests."""
        if self._temporary_directory is not None:
            raise AssertionError("MCP client smoke tests are already prepared")

        self._temporary_directory = tempfile.TemporaryDirectory(
            prefix="apx-mcp-client-"
        )
        self._test_root = Path(self._temporary_directory.name)
        self._environment, self._state_directory = isolated_environment(
            self._test_root
        )
        copy_known_hosts(self._test_root)
        self._apx_binary = str(Path(apx_binary).resolve())
        self._output_dir = Path(output_dir)
        self._target = {
            "name": target_name,
            "host": target_host,
            "port": int(target_port),
            "user": target_user,
            "private_key_path": str(Path(target_key).expanduser().resolve()),
        }
        self._server_port = self._available_tcp_port()
        self._auth_port = self._available_tcp_port()
        while self._auth_port == self._server_port:
            self._auth_port = self._available_tcp_port()

        try:
            login = (
                f"{target_user}@{target_host}:{int(target_port)}:"
                f"{self._target['private_key_path']}"
            )
            self._run_apx(
                [
                    "target",
                    "add",
                    login,
                    "--name",
                    target_name,
                    "--host-key-policy",
                    "strict",
                ]
            )
            self._curated_run_id = self._create_fixture_run(
                "code_hotspots",
                ["--system-wide", "--timeout", "1"],
            )
            self._query_run_id = self._create_fixture_run(
                "system_utilization",
                [
                    "--system-wide",
                    "--param",
                    "interval=0.1",
                    "--timeout",
                    "1",
                ],
            )
        finally:
            self._stop_fixture_daemon()

        self._client.prepare(
            self._apx_binary,
            self._environment,
            self._state_directory,
            self._test_root,
        )

    @keyword("Close MCP Client Smoke Tests")
    def close_smoke_tests(self) -> None:
        """Remove the isolated state used by the smoke tests."""
        if self._temporary_directory is None:
            return
        self._stop_fixture_daemon()
        self._temporary_directory.cleanup()
        self._temporary_directory = None

    @keyword("MCP Client Smoke Tests Are Prepared")
    def smoke_tests_are_prepared(self) -> None:
        """Check that the suite fixture completed successfully."""
        self._require_prepared()

    @keyword("Registered MCP Tools Have Smoke Coverage")
    def registered_tools_have_smoke_coverage(self) -> bool:
        """Check that every registered tool has coding-agent coverage."""
        self._require_prepared()
        stderr_path = self._output_dir / "tool-inventory.stderr"
        try:
            registered = asyncio.run(
                self._registered_mcp_tools(stderr_path)
            )
        except Exception as error:
            stderr = (
                stderr_path.read_text(encoding="utf-8", errors="replace")
                if stderr_path.exists()
                else "(no MCP stderr output)"
            )
            raise AssertionError(
                f"Could not list registered MCP tools. MCP stderr:\n{stderr}"
            ) from error

        untested = registered - COVERED_MCP_TOOLS
        obsolete = COVERED_MCP_TOOLS - registered
        if untested or obsolete:
            details = []
            if untested:
                details.append(
                    "Add smoke tests to mcp_clients.robot and "
                    "MCPClientSmoke.py for: "
                    f"{', '.join(sorted(untested))}; then update "
                    "COVERED_MCP_TOOLS"
                )
            if obsolete:
                details.append(
                    "Remove obsolete COVERED_MCP_TOOLS entries for: "
                    f"{', '.join(sorted(obsolete))}"
                )
            raise AssertionError(
                "MCP client smoke coverage does not match the registered "
                f"tools. {'; '.join(details)}"
            )
        return True

    @keyword("Codex Can Call Add Target")
    def call_add_target(self) -> bool:
        """Check that Codex can call add_target with explicit arguments."""
        target = dict(self._target)
        target["name"] = f"{target['name']}-mcp-{uuid.uuid4().hex[:8]}"
        call = self._call_tool("add-target", "add_target", target)
        if call["result"].get("target_name") != target["name"]:
            raise AssertionError(f"Target was not added: {call['result']}")
        if call["result"].get("connectivity_ok") is not True:
            raise AssertionError(
                f"Target connectivity check failed: {call['result']}"
            )
        return True

    @keyword("Codex Can Call List Targets")
    def call_list_targets(self) -> bool:
        """Check that Codex can call list_targets."""
        call = self._call_tool("list-targets", "list_targets", {})
        target_names = {
            target.get("name") for target in call["result"].get("targets", [])
        }
        if self._target["name"] not in target_names:
            raise AssertionError(f"Fixture target was not listed: {call['result']}")
        return True

    @keyword("Codex Can Search Target Processes")
    def search_target_processes(self) -> bool:
        """Check that Codex can call search_target_processes."""
        arguments = {
            "target": self._target["name"],
            "contains": "sshd",
        }
        call = self._call_tool(
            "search-target-processes", "search_target_processes", arguments
        )
        processes = call["result"].get("processes") or []
        if not processes:
            raise AssertionError(
                f"No SSH server processes were returned: {call['result']}"
            )
        if call["result"].get("total_matches", 0) < len(processes):
            raise AssertionError(
                f"Process result count is inconsistent: {call['result']}"
            )
        if not isinstance(call["result"].get("results_truncated"), bool):
            raise AssertionError(
                f"Process truncation status was not returned: {call['result']}"
            )
        return True

    @keyword("Codex Can Call List Recipes")
    def call_list_recipes(self) -> bool:
        """Check that Codex can call list_recipes."""
        call = self._call_tool("list-recipes", "list_recipes", {})
        recipe_names = {
            recipe.get("name") for recipe in call["result"].get("recipes", [])
        }
        expected = {"code_hotspots", "system_utilization"}
        if not expected.issubset(recipe_names):
            raise AssertionError(
                f"Expected recipes were not listed: {call['result']}"
            )
        return True

    @keyword("Codex Can Call Recipe Info")
    def call_recipe_info(self) -> bool:
        """Check that Codex can call recipe_info with explicit arguments."""
        arguments = {
            "recipe": "code_hotspots",
            "target": self._target["name"],
        }
        call = self._call_tool("recipe-info", "recipe_info", arguments)
        if call["result"].get("name") != "code_hotspots":
            raise AssertionError(
                f"Code Hotspots details were not returned: {call['result']}"
            )
        support = (call["result"].get("target_support") or {}).get("result")
        if support not in {"supported", "conditionally_supported"}:
            raise AssertionError(
                f"Target support was not returned: {call['result']}"
            )
        return True

    @keyword("Codex Can Call Run Recipe")
    def call_run_recipe(self) -> bool:
        """Check that Codex can call run_recipe with explicit arguments."""
        arguments = {
            "recipe": "code_hotspots",
            "target": self._target["name"],
            "system": {},
            "timeout": 1,
        }
        call = self._call_tool("run-recipe", "run_recipe", arguments)
        if call["result"].get("run_status") != "completed":
            raise AssertionError(f"Recipe did not complete: {call['result']}")
        if not call["result"].get("run_id"):
            raise AssertionError(f"Recipe returned no run ID: {call['result']}")
        return True

    @keyword("Codex Can Call List Runs")
    def call_list_runs(self) -> bool:
        """Check that Codex can call list_runs."""
        call = self._call_tool("list-runs", "list_runs", {})
        runs = call["result"].get("runs", [])
        fixtures = {
            run.get("id"): run
            for run in runs
            if run.get("id") in {self._curated_run_id, self._query_run_id}
        }
        if fixtures.keys() != {self._curated_run_id, self._query_run_id}:
            raise AssertionError(f"Fixture runs were not listed: {call['result']}")
        failed = [
            run for run in fixtures.values() if run.get("run_result") != "success"
        ]
        if failed:
            raise AssertionError(f"Fixture runs were not successful: {failed}")
        return True

    @keyword("Codex Can Manage A Render Session")
    def manage_render_session(self) -> bool:
        """Check the open, list, and close render-session tools."""
        open_arguments = {"run_id": self._query_run_id}
        prompt = self._prompt(
            "Call `open_render_session` exactly once with these JSON arguments: "
            f"{json.dumps(open_arguments)}. Then call `list_render_sessions` "
            "exactly once with {}. Finally, call `close_render_session` exactly "
            "once with the session ID returned by `open_render_session`."
        )
        calls = self._run_client("render-session", prompt)
        self._require_call_sequence(
            calls,
            [
                "open_render_session",
                "list_render_sessions",
                "close_render_session",
            ],
        )

        opened, listed, closed = calls
        self._require_arguments(opened, open_arguments)
        session_id = opened["result"].get("session_id")
        if not session_id:
            raise AssertionError(
                f"Render session returned no session ID: {opened['result']}"
            )

        self._require_arguments(listed, {})
        listed_session_ids = {
            session.get("session_id")
            for session in listed["result"].get("sessions", [])
        }
        if session_id not in listed_session_ids:
            raise AssertionError(
                f"Opened render session was not listed: {listed['result']}"
            )

        self._require_arguments(closed, {"session_id": session_id})
        if closed["result"].get("error"):
            raise AssertionError(
                f"Render session was not closed: {closed['result']}"
            )
        return True

    @keyword("Codex Can Call Run Query")
    def call_run_query(self) -> bool:
        """Check that Codex can call run_query with explicit arguments."""
        arguments = {
            "run_id": self._query_run_id,
            "sql": "SELECT 1 AS value",
        }
        call = self._call_tool("run-query", "run_query", arguments)
        if call["result"].get("returned_row_count") != 1:
            raise AssertionError(f"Query returned no row: {call['result']}")
        return True

    @keyword("Codex Can Generate Curated AI Insights")
    def generate_curated_ai_insights(self) -> bool:
        """Check curated insight generation and its payload-details tool."""
        prompt = self._prompt(
            "Call `generate_ai_insights` exactly once with these JSON arguments: "
            f"{json.dumps({'run_id': self._curated_run_id})}. Then call "
            "`read_ai_insights_payload_details` exactly once with `bundle_id` "
            "set to the returned `bundle_id`, `name` set to the first returned "
            "payload's `name`, and `offset` set to 0, even if that payload is "
            "already complete."
        )
        calls = self._run_client("curated-ai-insights", prompt)
        self._require_call_sequence(
            calls,
            [
                "generate_ai_insights",
                "read_ai_insights_payload_details",
            ],
        )
        generate, read_payload = calls
        self._require_arguments(generate, {"run_id": self._curated_run_id})
        bundle_id = generate["result"].get("bundle_id")
        payloads = generate["result"].get("payloads") or []
        if (
            generate["result"].get("run_id") != self._curated_run_id
            or not bundle_id
            or not payloads
        ):
            raise AssertionError(
                "Curated AI Insights returned no readable bundle: "
                f"{generate['result']}"
            )

        expected_arguments = {
            "bundle_id": bundle_id,
            "name": payloads[0].get("name"),
            "offset": 0,
        }
        self._require_arguments(read_payload, expected_arguments)
        if read_payload["result"].get("name") != expected_arguments["name"]:
            raise AssertionError(
                f"Payload details returned the wrong payload: {read_payload['result']}"
            )
        return True

    @keyword("Codex Can Generate Query-Based AI Insights")
    def generate_query_ai_insights(self) -> bool:
        """Check query-based insight generation."""
        arguments = {"run_id": self._query_run_id}
        generate = self._call_tool(
            "query-ai-insights", "generate_ai_insights", arguments
        )
        result = generate["result"]
        payloads = result.get("payloads") or []
        if result.get("run_id") != self._query_run_id:
            raise AssertionError(
                f"Query-based AI Insights used the wrong run: {result}"
            )
        if result.get("bundle_id"):
            raise AssertionError(
                f"Query-based AI Insights returned a curated bundle: {result}"
            )
        if "run_query" not in result.get("guidance", ""):
            raise AssertionError(
                f"Query-based AI Insights returned no query guidance: {result}"
            )
        if (
            len(payloads) != 1
            or payloads[0].get("name") != "run_details"
            or payloads[0].get("complete") is not True
        ):
            raise AssertionError(
                f"Query-based AI Insights returned unexpected payloads: {result}"
            )
        return True

    def _call_tool(
        self, test_name: str, tool: str, arguments: dict[str, Any]
    ) -> dict[str, Any]:
        prompt = self._prompt(
            f"Call `{tool}` exactly once with these JSON arguments: "
            f"{json.dumps(arguments)}."
        )
        calls = self._run_client(test_name, prompt)
        self._require_call_sequence(calls, [tool])
        call = calls[0]
        self._require_arguments(call, arguments)
        return call

    def _run_client(
        self, test_name: str, prompt: str
    ) -> list[dict[str, Any]]:
        self._require_prepared()
        calls = self._client.run(
            prompt, self._output_dir / "codex" / test_name
        )
        failed = [call for call in calls if call.get("status") != "completed"]
        if failed:
            raise AssertionError(f"MCP calls failed: {failed}")
        return calls

    @staticmethod
    def _prompt(instruction: str) -> str:
        return (
            "Use only the Arm Performix MCP server. Do not use shell commands, "
            "files, web search, or other MCP servers. "
            f"{instruction} Do not call any other MCP tool."
        )

    @staticmethod
    def _require_call_sequence(
        calls: list[dict[str, Any]], expected: list[str]
    ) -> None:
        called = [call.get("tool") for call in calls]
        if called != expected:
            raise AssertionError(
                f"Expected MCP calls {expected}, found {called}"
            )

    @staticmethod
    def _require_arguments(
        call: dict[str, Any], expected: dict[str, Any]
    ) -> None:
        if call["arguments"] != expected:
            raise AssertionError(
                f"{call['tool']} used {call['arguments']}, expected {expected}"
            )

    async def _registered_mcp_tools(self, stderr_path: Path) -> set[str]:
        parameters = StdioServerParameters(
            command=self._apx_binary,
            args=["mcp", "start"],
            env=self._environment,
        )
        stderr_path.parent.mkdir(parents=True, exist_ok=True)
        tool_names = set()
        with stderr_path.open("w", encoding="utf-8") as stderr:
            async with stdio_client(parameters, errlog=stderr) as (read, write):
                async with ClientSession(read, write) as session:
                    await session.initialize()
                    await wait_for_pid_file_count(self._state_directory, 1)
                    cursor = None
                    while True:
                        page = await session.list_tools(cursor)
                        tool_names.update(tool.name for tool in page.tools)
                        cursor = page.nextCursor
                        if cursor is None:
                            break
        await wait_for_pid_file_count(self._state_directory, 0)
        return tool_names

    def _run_apx(
        self,
        arguments: list[str],
        *,
        json_output: bool = False,
        timeout: int = 60,
    ) -> subprocess.CompletedProcess[str]:
        command = [
            self._apx_binary,
            "--server-port",
            str(self._server_port),
            "--auth-port",
            str(self._auth_port),
        ]
        if json_output:
            command.append("--json")
        command.extend(arguments)
        process = subprocess.run(
            command,
            capture_output=True,
            check=False,
            env=self._environment,
            text=True,
            timeout=timeout,
        )
        if process.returncode != 0:
            raise AssertionError(
                f"Fixture command failed with exit code {process.returncode}: "
                f"{' '.join(arguments)}\n"
                f"stdout:\n{process.stdout or '(no output)'}\n"
                f"stderr:\n{process.stderr or '(no output)'}"
            )
        return process

    def _create_fixture_run(
        self, recipe: str, arguments: list[str]
    ) -> str:
        process = self._run_apx(
            [
                "recipe",
                "run",
                recipe,
                "--target",
                self._target["name"],
                "--deploy-tools",
                *arguments,
            ],
            json_output=True,
            timeout=10 * 60,
        )
        return self._run_id(process.stdout)

    def _stop_fixture_daemon(self) -> None:
        if not self._apx_binary or not self._server_port:
            return
        subprocess.run(
            [
                self._apx_binary,
                "--server-port",
                str(self._server_port),
                "--auth-port",
                str(self._auth_port),
                "daemon",
                "stop",
            ],
            capture_output=True,
            check=False,
            env=self._environment,
            text=True,
            timeout=30,
        )
        asyncio.run(wait_for_pid_file_count(self._state_directory, 0))

    def _require_prepared(self) -> None:
        if self._temporary_directory is None:
            raise AssertionError("MCP client smoke tests are not prepared")

    @staticmethod
    def _run_id(output: str) -> str:
        for line in reversed(output.splitlines()):
            try:
                result = json.loads(line)
            except json.JSONDecodeError:
                continue
            run_id = ((result.get("data") or {}).get("run_id") or {}).get(
                "value"
            )
            if run_id:
                return run_id
        raise AssertionError(f"Fixture recipe returned no run ID: {output}")

    @staticmethod
    def _available_tcp_port() -> int:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            listener.bind(("127.0.0.1", 0))
            return listener.getsockname()[1]
