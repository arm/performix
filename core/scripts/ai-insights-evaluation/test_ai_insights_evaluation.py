# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Evaluate AI Insights answers for known Performix analysis scenarios.

AI Insights are a Performix feature exposed via the Performix MCP Server which
provide LLM-generated explanations and recommendations of a Performix run.
They should identify the relevant performance issue, connect it to evidence in
the run, and suggest a useful next action.

This is a pytest evaluation suite for AI Insights. For each test case it:
1. Imports a pre-recorded Performix run of a workload with a known issue.
2. Invokes the selected AI Insights implementation (via an MCP client such as codex) and records the response.
3. Evaluates whether the answer matches the expected analysis for each testcase.

The expected answer is semantic rather than byte-for-byte stable, so the
suite uses a separate LLM judge with private rubrics. The judge checks
whether the response contains the required insight, evidence, and
recommendation without exposing those rubrics to the model under test.

The default Act 1 run uses REST, Hackathon MCP, and production
Performix MCP modes; see README.md for the full local setup and
reporting details.
"""

from __future__ import annotations

import json
import logging
import os
import re
import shutil
import socket
import subprocess
import sys
import textwrap
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import pytest
import truststore

# The internal API proxy can use enterprise certificate authorities which are
# available through the operating-system trust store but not Python's CA bundle.
truststore.inject_into_ssl()

from codex_session_metrics import (
    codex_session_log,
    collect_mcp_tool_metrics,
    iter_jsonl,
)
from performance_quality import PERFORMIX_MCP_MODE

sys.path.append(str(Path(__file__).resolve().parents[1]))
from run_export_helper import sha256_file
from rest_mode import REST_MODE, invoke_rest_mode

DEFAULT_API_BASE = "https://openai-api-proxy.geo.arm.com/api/providers/openai/v1"
HACKATHON_SERVER = "performix-hackathon"
HACKATHON_TOOL = "atp_show_ai_insights"
PERFORMIX_SERVER = "arm-performix"
PERFORMIX_TOOL = "generate_ai_insights"
RETRYABLE_HTTP_STATUS = {429, 500, 502, 503, 504}
FAILURE_RESPONSE_MAX_CHARS = 4000
FAILURE_RESPONSE_MAX_LINES = 80
FAILURE_TEXT_FALLBACK_WIDTH = 80
LOGGER = logging.getLogger(__name__)
CODEX_TRUNCATION_MARKER_RE = re.compile(
    rf"\N{{HORIZONTAL ELLIPSIS}}(\d+) tokens truncated\N{{HORIZONTAL ELLIPSIS}}"
)


class AiInsightsConfigError(RuntimeError):
    """Raised when required local evaluation configuration is missing."""


@dataclass(frozen=True)
class McpServer:
    """MCP server configuration for one AI Insights implementation."""

    id: str
    server: str
    tool: str
    server_dir_key: str
    command: str | None
    args: tuple[str, ...]
    command_key: str | None = None
    extra_tools: tuple[str, ...] = ()


MCP_MODES = {
    "hackathon_mcp": McpServer(
        id="hackathon_mcp",
        server=HACKATHON_SERVER,
        tool=HACKATHON_TOOL,
        server_dir_key="hackathon_mcp_server_dir",
        command="node",
        args=("dist/index.js",),
    ),
    PERFORMIX_MCP_MODE: McpServer(
        id=PERFORMIX_MCP_MODE,
        server=PERFORMIX_SERVER,
        tool=PERFORMIX_TOOL,
        server_dir_key="cli_dir",
        command=None,
        args=("mcp", "start"),
        command_key="cli_bin",
        extra_tools=("read_ai_insights_payload_details",),
    ),
}
NON_FATAL_TOOL_OUTPUT_TRUNCATION_MODES = {"hackathon_mcp"}


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def manifest_path(pytestconfig) -> Path:
    return Path(pytestconfig.getoption("--ai-manifest")).expanduser().resolve()


def selected_acts(pytestconfig) -> set[str]:
    raw = pytestconfig.getoption("--ai-act")
    return {act.strip() for act in str(raw).split(",") if act.strip()}


def selected_modes(pytestconfig) -> list[str]:
    raw = pytestconfig.getoption("--ai-modes")
    return [mode.strip() for mode in str(raw).split(",") if mode.strip()]


def manifest_acts(manifest: dict[str, Any]) -> set[str]:
    acts: set[str] = set()
    for test_case in manifest.get("tests", []):
        for act in test_case.get("acts", []):
            if isinstance(act, str) and act.strip():
                acts.add(act.strip())
    return acts


def validate_selected_acts(acts: set[str], manifest: dict[str, Any]) -> None:
    if not acts:
        return

    available = manifest_acts(manifest)
    unknown = acts - available
    if unknown:
        requested_text = ", ".join(sorted(acts))
        unknown_text = ", ".join(sorted(unknown))
        available_text = ", ".join(sorted(available)) if available else "none"
        raise pytest.UsageError(
            f"Unknown AI Insights act selection: {unknown_text}. "
            f"Requested acts: {requested_text}. Available acts: {available_text}."
        )


def iter_manifest_parameters(pytestconfig, manifest: dict[str, Any]) -> list[dict[str, Any]]:
    defaults = manifest.get("defaults", {})
    cli_modes = selected_modes(pytestconfig)
    default_modes = defaults.get("modes") or [REST_MODE, "hackathon_mcp", PERFORMIX_MCP_MODE]
    modes = cli_modes or default_modes
    acts = selected_acts(pytestconfig)
    validate_selected_acts(acts, manifest)
    params: list[dict[str, Any]] = []
    for test_case in manifest.get("tests", []):
        if acts and not acts.intersection(test_case.get("acts", [])):
            continue
        for mode in modes:
            params.append({"test_case": test_case, "mode": mode})
    return params


def normalize_expected_failure(value: Any) -> dict[str, Any]:
    """Normalize manifest expected-failure metadata."""
    if isinstance(value, bool):
        return {"expected_failure": value, "reason": ""}
    if isinstance(value, str):
        return {"expected_failure": True, "reason": value.strip()}
    if isinstance(value, dict):
        has_reason = bool(str(value.get("reason") or value.get("expected_failure_reason") or "").strip())
        return {
            "expected_failure": bool(value.get("expected_failure", has_reason)),
            "reason": str(value.get("reason") or value.get("expected_failure_reason") or "").strip(),
        }
    return {"expected_failure": False, "reason": ""}


def expected_failure_for_mode(test_case: dict[str, Any], mode: str) -> dict[str, Any]:
    """Return expected-failure metadata for this testcase/mode pair."""
    expectation = normalize_expected_failure(
        {
            "expected_failure": test_case.get("expected_failure", False),
            "reason": test_case.get("expected_failure_reason") or test_case.get("reason", ""),
        }
    )
    expected_failures = test_case.get("expected_failures")
    if isinstance(expected_failures, dict):
        modes = expected_failures.get("modes")
        if isinstance(modes, dict) and mode in modes:
            return normalize_expected_failure(modes[mode])
        if mode in expected_failures:
            return normalize_expected_failure(expected_failures[mode])
        nested_expectation = normalize_expected_failure(expected_failures)
        if nested_expectation["expected_failure"]:
            return nested_expectation
    return expectation


def display_label(actual_label: str, *, expected_failure: bool) -> str:
    actual = str(actual_label or "unknown").lower()
    if actual == "pass" and expected_failure:
        return "xpass"
    if should_xfail_final_label(actual, expected_failure=expected_failure):
        return "xfail"
    return actual


def should_xfail_final_label(final_label: str, *, expected_failure: bool) -> bool:
    """Return whether the final label is an expected model failure."""
    return expected_failure and str(final_label or "unknown").lower() == "fail"


def should_fail_on_tool_output_truncation(mode: str, invoke_meta: dict[str, Any]) -> bool:
    """Return whether Codex tool-output truncation should fail this attempt."""
    return (
        tool_output_truncation_markers(invoke_meta) > 0
        and mode not in NON_FATAL_TOOL_OUTPUT_TRUNCATION_MODES
    )


def tool_output_truncation_severity(mode: str, invoke_meta: dict[str, Any]) -> str:
    """Return the report severity for Codex tool-output truncation."""
    if tool_output_truncation_markers(invoke_meta) <= 0:
        return ""
    if should_fail_on_tool_output_truncation(mode, invoke_meta):
        return "error"
    return "warning"


def tool_output_truncation_markers(invoke_meta: dict[str, Any]) -> int:
    markers = invoke_meta.get("truncation_markers", 0)
    if isinstance(markers, int):
        return markers
    return 0


def slugify_test_name(value: str) -> str:
    """Return a pytest-id-safe description fragment."""
    slug = re.sub(r"[^A-Za-z0-9]+", "_", value).strip("_")
    return slug or "unspecified"


def pytest_parameter_id(test_case: dict[str, Any], mode: str, attempt: int) -> str:
    """Build the pytest item id without changing run-facing identifiers."""
    summary = test_case.get("summary")
    if not isinstance(summary, str) or not summary.strip():
        raise ValueError(f"testcase {test_case['id']} must define a non-empty summary")
    parts = [test_case["id"], slugify_test_name(summary)]
    parts.extend([mode, f"attempt_{attempt:03d}"])
    return "-".join(parts)


def attempt_count(pytestconfig, manifest: dict[str, Any], test_case: dict[str, Any]) -> int:
    defaults = manifest.get("defaults", {})
    configured_attempts = pytestconfig.getoption("--ai-attempts") or defaults.get("attempts", 1)
    return int(test_case.get("attempts", configured_attempts))


def pytest_generate_tests(metafunc) -> None:
    """Parse manifest and generate pytest testcase/mode/attempt items.

    We generate a separate test for each attempt instead of performing
    multiple attempts inside a single test. This makes repeated attempts
    explicit in pytest reporting, with per-attempt properties such as token
    usage and runtime instead of one aggregated metric set.
    """
    if {"test_case", "mode", "attempt"}.issubset(metafunc.fixturenames):
        manifest = load_json(manifest_path(metafunc.config))
        params: list[tuple[dict[str, Any], str, int, int]] = []
        ids: list[str] = []
        for param in iter_manifest_parameters(metafunc.config, manifest):
            test_case = param["test_case"]
            mode = param["mode"]
            attempts = attempt_count(metafunc.config, manifest, test_case)
            for attempt in range(1, attempts + 1):
                params.append((test_case, mode, attempt, attempts))
                ids.append(pytest_parameter_id(test_case, mode, attempt))
        metafunc.parametrize(
            ("test_case", "mode", "attempt", "attempts_total"),
            params,
            ids=ids,
        )


def required_path(name: str, value: str | None) -> Path:
    if not value:
        raise AiInsightsConfigError(f"{name} is required")
    return Path(value).expanduser().resolve()


def required_option_path(option: str, env_var: str, value: str | None) -> Path:
    if not value:
        raise AiInsightsConfigError(f"{option} is required. Set {option} or {env_var}.")
    path = Path(value).expanduser().resolve()
    if not path.is_dir():
        raise AiInsightsConfigError(f"{option} path does not exist or is not a directory: {path}")
    return path


def required_openai_api_key(value: str | None) -> str:
    api_key = (value or "").strip()
    if not api_key:
        raise AiInsightsConfigError(
            "--ai-openai-api-key is required for AI Insights evaluation. "
            "Set --ai-openai-api-key or OPENAI_API_KEY to the API key used by "
            "the evaluation agent and the judge."
        )
    return api_key


def resolve_config(pytestconfig) -> dict[str, Any]:
    """Resolve manifest defaults, pytest options, and required environment."""
    path = manifest_path(pytestconfig)
    manifest = load_json(path)
    defaults = manifest.get("defaults", {})
    modes = selected_modes(pytestconfig)
    run_base = pytestconfig.getoption("--ai-prerecorded-run-cache")
    model_configs = manifest.get("model_configs") or [{"id": "default", "model": "gpt-5.5"}]
    model_config = model_configs[0]
    model = pytestconfig.getoption("--ai-model") or model_config["model"]
    reasoning_effort = pytestconfig.getoption("--ai-reasoning-effort")
    if reasoning_effort is None:
        reasoning_effort = model_config["reasoning_effort"]
    reasoning_effort = str(reasoning_effort).strip()
    if not reasoning_effort:
        raise AiInsightsConfigError("reasoning_effort is required")
    attempts = pytestconfig.getoption("--ai-attempts") or int(defaults.get("attempts", 1))
    openai_api_key = required_openai_api_key(pytestconfig.getoption("--ai-openai-api-key"))
    hackathon_mcp_server_dir = None
    if "hackathon_mcp" in modes:
        hackathon_mcp_server_dir = required_option_path(
            "--ai-hackathon-mcp-server-dir",
            "AI_INSIGHTS_HACKATHON_MCP_SERVER_DIR",
            pytestconfig.getoption("--ai-hackathon-mcp-server-dir"),
        )
    LOGGER.info("Loaded AI Insights evaluation manifest: %s", path)
    return {
        "manifest_path": path,
        "manifest": manifest,
        "modes": modes,
        "run_artifact_base": required_path("prerecorded_run_cache", run_base),
        "cli_bin": Path(pytestconfig.getoption("--ai-cli-bin")).expanduser().resolve(),
        "cli_dir": Path(pytestconfig.getoption("--ai-cli-bin")).expanduser().resolve().parent,
        "results_dir": Path(pytestconfig.getoption("--ai-results-dir")).expanduser().resolve(),
        "hackathon_mcp_server_dir": hackathon_mcp_server_dir,
        "model": model,
        "model_config_id": model_config.get("id", model),
        "reasoning_effort": reasoning_effort,
        "judge_model": pytestconfig.getoption("--ai-judge-model"),
        "openai_api_key": openai_api_key,
        "attempts": attempts,
        "defaults": defaults,
    }


def run_info_succeeds(cli_bin: Path, run_id: str) -> bool:
    process = subprocess.run(
        [str(cli_bin), "run", "info", run_id, "--json"],
        cwd=cli_bin.parent,
        capture_output=True,
        text=True,
    )
    return process.returncode == 0


def extract_source_archive(
    source_archive: Path,
    source_sha: str,
    cfg: dict[str, Any],
    test_id: str,
) -> Path:
    """Return the source root extracted by the serial pytest setup phase."""

    source_root = cfg["results_dir"] / "imported_sources" / test_id / source_sha / "test_src"
    if not source_root.is_dir():
        raise FileNotFoundError(
            f"pre-recorded source archive was not extracted for {test_id}: {source_root}"
        )
    return source_root


def import_run_cached(pytestconfig, test_case: dict[str, Any], cfg: dict[str, Any]) -> dict[str, Any]:
    """Return the run imported by the serial pytest setup phase.

    The test body may run in parallel across modes for the same testcase. It
    therefore treats run import and source extraction as completed setup work,
    and fails clearly if the controller did not populate the cache first.
    """
    archive = cfg["run_artifact_base"] / test_case["run_artifact"]
    if not archive.is_file():
        raise FileNotFoundError(
            f"missing pre-recorded run archive for {test_case['id']}: {archive}"
        )
    source_archive = archive.parent / "test_src.zip"
    if not source_archive.is_file():
        raise FileNotFoundError(
            f"missing pre-recorded source archive for {test_case['id']}: {source_archive}"
        )
    LOGGER.info("Using pre-recorded run archive for %s: %s", test_case["id"], archive)
    archive_sha = sha256_file(archive)
    source_archive_sha = sha256_file(source_archive)
    source_root = extract_source_archive(
        source_archive,
        source_archive_sha,
        cfg,
        test_case["id"],
    )
    cache_key = f"ai_insights/imports/{test_case['id']}/{archive_sha}"
    cached = pytestconfig.cache.get(cache_key, None)
    if not isinstance(cached, dict) or not isinstance(cached.get("run_id"), str):
        raise FileNotFoundError(
            f"pre-recorded run archive was not imported during setup for {test_case['id']}: {archive}"
        )
    run_id = cached["run_id"]
    if not run_info_succeeds(cfg["cli_bin"], run_id):
        raise RuntimeError(
            f"pre-recorded run import prepared during setup is not available for {test_case['id']}: {run_id}"
        )
    LOGGER.info("Using setup-imported run for %s: %s", test_case["id"], run_id)
    return {
        "run_id": run_id,
        "archive_sha256": archive_sha,
        "archive_path": str(archive),
        "source_archive_sha256": source_archive_sha,
        "source_archive_path": str(source_archive),
        "source_root": str(source_root),
    }


def toml_string(value: str) -> str:
    return json.dumps(value)


def mcp_mode_env(mode: McpServer, cfg: dict[str, Any]) -> dict[str, str]:
    """Return environment passed to the selected MCP server."""
    if mode.id == "hackathon_mcp":
        # These ATP_* names are the current Hackathon MCP server contract.
        return {
            "ATP_CLI_PATH": str(cfg["cli_bin"]),
            "ATP_ENGINE_PORT": os.environ.get("ATP_ENGINE_PORT", "9000"),
        }
    if mode.id == PERFORMIX_MCP_MODE:
        return {}
    raise ValueError(f"unsupported AI Insights mode: {mode.id}")


def mcp_mode_command(mode: McpServer, cfg: dict[str, Any]) -> str:
    if mode.command_key:
        return str(cfg[mode.command_key])
    if mode.command is None:
        raise ValueError(f"unsupported AI Insights mode command: {mode.id}")
    return mode.command


def write_codex_home(attempt_dir: Path, workspace: Path, cfg: dict[str, Any], mode: McpServer) -> Path:
    """Create the isolated Codex configuration used for one MCP attempt.

    The model under test should obtain AI Insights through the configured
    MCP server, not through the developer's normal Codex configuration or
    arbitrary filesystem/network access. Each attempt therefore gets a
    generated CODEX_HOME with one MCP server and narrow permissions.
    """
    codex_home = attempt_dir / "codex_home"
    LOGGER.debug("Preparing isolated Codex home: %s", codex_home)
    if codex_home.exists():
        shutil.rmtree(codex_home)
    codex_home.mkdir(parents=True)

    (codex_home / "auth.json").write_text(
        json.dumps(
            {"auth_mode": "apikey", "OPENAI_API_KEY": cfg["openai_api_key"]},
            indent=2,
        ),
        encoding="utf-8",
    )
    (codex_home / "auth.json").chmod(0o600)

    server_dir = cfg[mode.server_dir_key]
    server_env = mcp_mode_env(mode, cfg)
    env_section = ""
    if server_env:
        env_lines = "\n".join(f"{key} = {toml_string(value)}" for key, value in server_env.items())
        env_section = f"""
[mcp_servers.{mode.server}.env]
# Pass only the environment needed by this MCP mode.
{env_lines}
"""
    LOGGER.debug("Using %s MCP server from: %s", mode.id, server_dir)
    config = f"""# Use the same OpenAI-compatible proxy path as developer Codex runs.
model_provider = "proxy"

# Evaluation responses are already captured under the attempt artefacts.
disable_response_storage = true

# Keep Codex confined to the prompt workspace and the configured MCP server.
default_permissions = "ai-insights-mcp"

# Treat the generated workspace as the project root instead of loading project
# instructions from parent directories in the Performix checkout.
project_root_markers = []

[features]
# The model under test should obtain Performix evidence through MCP, not by
# running local shell commands during evaluation.
shell_tool = false

# Marketplace plugins are not part of the evaluation and would otherwise be
# downloaded into every attempt's isolated CODEX_HOME.
plugins = false

[model_providers.proxy]
name = "OpenAI"
base_url = {toml_string(os.environ.get("OPENAI_API_BASE", DEFAULT_API_BASE).rstrip("/"))}
wire_api = "responses"

[permissions.ai-insights-mcp.filesystem]
# Minimal read access lets Codex read its prompt and generated workspace files
# without inheriting access to the developer's normal filesystem.
":minimal" = "read"

[permissions.ai-insights-mcp.filesystem.":workspace_roots"]
# The generated workspace contains only per-attempt inputs and outputs.
"." = "read"

[permissions.ai-insights-mcp.network]
# The model under test should obtain Performix evidence through MCP, not by
# making arbitrary network calls during evaluation.
enabled = false

[mcp_servers.{mode.server}]
command = {toml_string(mcp_mode_command(mode, cfg))}
args = {json.dumps(list(mode.args))}
cwd = {toml_string(str(server_dir))}
enabled = true
# Allow the configured MCP server to be used non-interactively by pytest.
default_tools_approval_mode = "approve"
{env_section}
"""
    (codex_home / "config.toml").write_text(config, encoding="utf-8")
    return codex_home


def collect_json_value(value: Any, keys: set[str]) -> list[Any]:
    matches: list[Any] = []
    if isinstance(value, dict):
        for key, child in value.items():
            if key in keys:
                matches.append(child)
            matches.extend(collect_json_value(child, keys))
    elif isinstance(value, list):
        for child in value:
            matches.extend(collect_json_value(child, keys))
    return matches


def collect_token_usage(raw_jsonl: Path) -> list[Any]:
    usage: list[Any] = []
    for event in iter_jsonl(raw_jsonl):
        usage.extend(collect_json_value(event, {"usage", "token_usage"}))
    return usage


def validate_mcp_call(raw_jsonl: Path, mode: McpServer) -> dict[str, Any]:
    """Confirm MCP client used the expected AI Insights MCP tool successfully.

    A non-empty Markdown answer is not enough for this suite: the answer
    must come from the intended MCP path so the test exercises the AI
    Insights integration rather than a generic model response.
    """
    LOGGER.info("Validating %s MCP tool call in Codex event log", mode.id)
    failed_messages: list[str] = []
    completed_by_tool: dict[str, int] = {}
    for event in iter_jsonl(raw_jsonl):
        if event.get("type") != "item.completed":
            continue
        item = event.get("item") or {}
        if item.get("type") != "mcp_tool_call":
            continue
        if item.get("server") not in (None, mode.server):
            continue
        tool = str(item.get("tool") or "")
        if item.get("status") == "completed":
            completed_by_tool[tool] = completed_by_tool.get(tool, 0) + 1
        elif item.get("status") == "failed":
            error = item.get("error") or {}
            failed_messages.append(f"{tool}: {error.get('message') or 'unknown MCP tool failure'}")
    if failed_messages:
        raise RuntimeError(f"{mode.server} MCP tool call failed: {'; '.join(failed_messages)}")
    required_completed = completed_by_tool.get(mode.tool, 0)
    if required_completed == 0:
        raw_text = raw_jsonl.read_text(encoding="utf-8") if raw_jsonl.is_file() else ""
        if (
            f'"tool":"{mode.tool}"' in raw_text
            and '"type":"mcp_tool_call"' in raw_text
            and '"status":"completed"' in raw_text
        ):
            required_completed = 1
            completed_by_tool[mode.tool] = max(completed_by_tool.get(mode.tool, 0), 1)
        else:
            raise RuntimeError(f"{mode.tool} did not complete successfully")
    return {
        "server": mode.server,
        "tool": mode.tool,
        "extra_tools": list(mode.extra_tools),
        "completed_calls": sum(completed_by_tool.values()),
        "required_completed_calls": required_completed,
        "completed_by_tool": completed_by_tool,
    }


def command_output_is_denied_only(output: str) -> bool:
    lowered = output.lower()
    if "permission denied" not in lowered and "operation not permitted" not in lowered:
        return False
    return "Process exited with code 0" not in output


def codex_truncation_marker_token_counts(codex_home: Path) -> list[int]:
    """Return per-marker token counts from Codex tool-output truncation markers.

    Codex writes the full MCP result to the exec JSONL event log, but the
    per-attempt session transcript records the tool output as it was sent to
    the model. The transcript is therefore where the Codex truncation marker(s)
    appear when `tool_output_token_limit` is exceeded.
    """
    LOGGER.debug("Checking Codex session transcript for truncated tool output")
    session_log = codex_session_log(codex_home)
    return [
        int(match.group(1))
        for match in CODEX_TRUNCATION_MARKER_RE.finditer(session_log.read_text(encoding="utf-8"))
    ]


def successful_external_commands(codex_home: Path) -> list[str]:
    """Return successful shell commands that would invalidate the attempt.

    The test allows denied shell attempts because they show the sandbox is
    working, but a successful external command means the model may have
    inspected files outside the intended MCP evidence path. Codex records
    shell tool calls in the per-attempt session transcript under CODEX_HOME.
    """
    LOGGER.debug("Checking Codex logs for successful external shell commands")
    calls: dict[str, str] = {}
    commands: list[str] = []
    session_log = codex_session_log(codex_home)
    for event in iter_jsonl(session_log):
        payload = event.get("payload") or {}
        if payload.get("type") == "function_call" and payload.get("name") == "exec_command":
            call_id = str(payload.get("call_id") or "")
            try:
                args = json.loads(str(payload.get("arguments") or "{}"))
            except json.JSONDecodeError:
                args = {}
            if call_id:
                calls[call_id] = str(args.get("cmd") or "unknown command")
        elif payload.get("type") == "function_call_output":
            output = str(payload.get("output") or "")
            if command_output_is_denied_only(output):
                continue
            if "Process exited with code 0" in output:
                command = calls.get(str(payload.get("call_id") or ""))
                if command:
                    commands.append(command)
    return [cmd for cmd in dict.fromkeys(commands) if cmd not in ("pwd", "/bin/zsh -lc pwd")]


def invoke_mcp_mode(
    attempt_dir: Path,
    run_meta: dict[str, Any],
    cfg: dict[str, Any],
    mode: McpServer,
) -> dict[str, Any]:
    """Run Codex against one AI Insights MCP mode and capture artefacts.

    This owns the model-under-test invocation contract:
    prompt text, raw agent event log, final Markdown response, MCP tool
    validation, external-command validation, and invocation metadata.
    """
    if shutil.which("codex") is None:
        raise RuntimeError("missing required command: codex")

    workspace = attempt_dir / "codex_workspace"
    if workspace.exists():
        shutil.rmtree(workspace)
    workspace.mkdir(parents=True)
    codex_home = write_codex_home(attempt_dir, workspace, cfg, mode)

    prompt = f"Show me AI Insights for Performix run {run_meta['run_id']}. Use Markdown.\n"
    prompt_path = attempt_dir / "codex_prompt.txt"
    raw_jsonl = attempt_dir / "codex_exec.jsonl"
    last_message = attempt_dir / "codex_last_message.txt"
    response_md = attempt_dir / "llm_response.md"
    response_md.unlink(missing_ok=True)
    prompt_path.write_text(prompt, encoding="utf-8")
    LOGGER.info(
        "Invoking Codex for %s via %s, run %s",
        run_meta["test_id"],
        mode.tool,
        run_meta["run_id"],
    )

    argv = ["codex", "exec", "--color", "never", "--json"]
    if cfg["reasoning_effort"]:
        argv.extend(["-c", f"model_reasoning_effort={cfg['reasoning_effort']}"])
    argv.extend([
        "--skip-git-repo-check",
        "--output-last-message",
        str(last_message),
        "-C",
        str(workspace),
        "--model",
        cfg["model"],
        "-",
    ])

    env = os.environ.copy()
    env["CODEX_HOME"] = str(codex_home)
    version = subprocess.run(["codex", "--version"], capture_output=True, text=True)
    codex_version = version.stdout.strip() or version.stderr.strip()
    started = time.monotonic()
    with prompt_path.open("r", encoding="utf-8") as stdin, raw_jsonl.open("w", encoding="utf-8") as stdout:
        process = subprocess.run(argv, stdin=stdin, stdout=stdout, stderr=subprocess.PIPE, text=True, env=env)
    duration = time.monotonic() - started
    if process.returncode != 0:
        raise RuntimeError(f"codex exec failed with exit code {process.returncode}: {process.stderr.strip()}")
    LOGGER.info("Codex invocation completed in %.1fs", duration)

    if not last_message.is_file() or last_message.stat().st_size == 0:
        raise RuntimeError("codex exec produced no final message")
    response_md.write_text(last_message.read_text(encoding="utf-8"), encoding="utf-8")
    LOGGER.info("Captured Codex response: %s (%d bytes)", response_md, response_md.stat().st_size)

    mcp = validate_mcp_call(raw_jsonl, mode)
    mcp.update(collect_mcp_tool_metrics(codex_home, mode.server))
    truncation_token_counts = codex_truncation_marker_token_counts(codex_home)
    truncation_markers = len(truncation_token_counts)
    successful_commands = successful_external_commands(codex_home)
    if successful_commands:
        raise RuntimeError(f"Codex successfully used external shell commands: {successful_commands}")
    return {
        "status": "ok",
        "model": cfg["model"],
        "reasoning_effort": cfg["reasoning_effort"],
        "codex_version": codex_version,
        "duration_seconds": duration,
        "mcp": mcp,
        "token_usage": collect_token_usage(raw_jsonl),
        "truncation_markers": truncation_markers,
        "truncation_marker_token_counts": truncation_token_counts,
        "successful_external_commands": successful_commands,
        "codex_home": str(codex_home),
        "workspace": str(workspace),
        "prompt_path": str(prompt_path),
        "raw_jsonl": str(raw_jsonl),
        "last_message": str(last_message),
        "response_markdown": str(response_md),
        "response_bytes": response_md.stat().st_size,
    }


def score_truncated_attempt(attempt_dir: Path, test_case: dict[str, Any], invoke_meta: dict[str, Any]) -> dict[str, Any]:
    """Record an evaluation failure caused by Codex tool-output truncation."""
    token_counts = invoke_meta.get("truncation_marker_token_counts", [])
    markers = invoke_meta.get("truncation_markers", len(token_counts))
    detail = (
        "Codex truncated MCP tool output in the session transcript: "
        f"found {markers} marker(s) with truncated token counts {token_counts}."
    )
    judge = {
        "label": "fail",
        "confidence": "high",
        "what_was_correct": "",
        "material_gaps": detail,
        "verdict_rationale": "The model did not receive the complete MCP evidence bundle.",
    }
    score = {
        "test_id": test_case["id"],
        "response_observations": {
            "issues": ["codex_tool_output_truncated"],
            "details": [detail],
        },
        "judge": judge,
        "final": {"final_label": "fail"},
    }
    (attempt_dir / "score.json").write_text(json.dumps(score, indent=2), encoding="utf-8")
    (attempt_dir / "score.md").write_text(
        "# Verdict: FAIL\n\n"
        f"- Test: `{test_case['id']}`\n"
        "- Judge: `fail` (confidence: `high`)\n\n"
        f"## Material Gaps\n{judge['material_gaps']}\n\n"
        f"## Verdict Rationale\n{judge['verdict_rationale']}\n",
        encoding="utf-8",
    )
    return score


def extract_json_object(text: str) -> dict[str, Any]:
    """Extract the judge JSON object even when the model wraps it in text."""
    try:
        value = json.loads(text)
        if isinstance(value, dict):
            return value
    except json.JSONDecodeError:
        pass
    start = text.find("{")
    end = text.rfind("}")
    if start == -1 or end <= start:
        raise ValueError("no JSON object found in judge response")
    value = json.loads(text[start : end + 1])
    if not isinstance(value, dict):
        raise ValueError("judge response JSON is not an object")
    return value


def judge_response(test_id: str, rubric: str, response_text: str, cfg: dict[str, Any]) -> dict[str, Any]:
    """Score one AI Insights response with a separate rubric-aware judge.

    Private rubrics are only sent to this direct judge call. They must not
    be included in the prompt or artefacts seen by the model under test.
    """
    api_key = cfg["openai_api_key"]
    LOGGER.info("Judging AI Insights response for %s with %s", test_id, cfg["judge_model"])
    prompt = {
        "task": "Score an AI Insights response for one performance testcase.",
        "instructions": [
            "Return strict JSON only.",
            "Use labels: pass or fail only.",
            "Judge semantic correctness and usefulness, not keyword overlap.",
            "Fail when the response misses the main testcase issue or gives only generic tuning advice.",
        ],
        "test_id": test_id,
        "private_rubric": rubric,
        "response_under_test": response_text,
        "output_schema": {
            "label": "pass|fail",
            "confidence": "low|medium|high",
            "what_was_correct": "string",
            "material_gaps": "string",
            "verdict_rationale": "string",
        },
    }
    body = {
        "model": cfg["judge_model"],
        "input": [{"role": "user", "content": [{"type": "input_text", "text": json.dumps(prompt)}]}],
    }
    request = urllib.request.Request(
        f"{os.environ.get('OPENAI_API_BASE', DEFAULT_API_BASE).rstrip('/')}/responses",
        data=json.dumps(body).encode("utf-8"),
        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
        method="POST",
    )
    last_exc: Exception | None = None
    for attempt in range(1, 4):
        try:
            LOGGER.info("Judge attempt %d for %s", attempt, test_id)
            with urllib.request.urlopen(request, timeout=120) as response:
                payload = json.loads(response.read().decode("utf-8"))
            fragments: list[str] = []
            for item in payload.get("output", []):
                if item.get("type") != "message":
                    continue
                for content in item.get("content", []):
                    if content.get("type") == "output_text":
                        fragments.append(str(content.get("text") or ""))
            parsed = extract_json_object("\n".join(fragments).strip())
            return {
                "label": str(parsed.get("label", "fail")).lower(),
                "confidence": str(parsed.get("confidence", "medium")).lower(),
                "what_was_correct": str(parsed.get("what_was_correct", "")).strip(),
                "material_gaps": str(parsed.get("material_gaps", "")).strip(),
                "verdict_rationale": str(parsed.get("verdict_rationale", "")).strip(),
            }
        except urllib.error.HTTPError as exc:
            last_exc = exc
            if exc.code not in RETRYABLE_HTTP_STATUS:
                raise
        except (urllib.error.URLError, TimeoutError, socket.timeout, json.JSONDecodeError, ValueError) as exc:
            last_exc = exc
        if attempt < 3:
            time.sleep(attempt)
    raise RuntimeError(f"judge unavailable after 3 attempts: {last_exc}")


def score_attempt(attempt_dir: Path, test_case: dict[str, Any], cfg: dict[str, Any]) -> dict[str, Any]:
    """Combine structural response checks and judge output into a label."""
    response_text = (attempt_dir / "llm_response.md").read_text(encoding="utf-8").strip()
    observations = {"issues": [], "details": []}
    if not response_text:
        observations["issues"].append("empty_response")
        observations["details"].append("llm_response.md was empty")
    rubric_path = Path(__file__).resolve().parent / test_case["rubric"]
    rubric = rubric_path.read_text(encoding="utf-8")
    judge = judge_response(test_case["id"], rubric, response_text, cfg)
    final_label = "pass" if judge["label"] == "pass" and not observations["issues"] else "fail"
    LOGGER.info(
        "Judge result for %s: %s (confidence: %s)",
        test_case["id"],
        final_label,
        judge["confidence"],
    )
    log_judge_output = LOGGER.warning if final_label == "fail" else LOGGER.info
    log_judge_output("Judge output for %s:", test_case["id"])
    log_judge_output("  what_was_correct: %s", judge["what_was_correct"] or "<empty>")
    log_judge_output("  material_gaps: %s", judge["material_gaps"] or "<empty>")
    log_judge_output("  verdict_rationale: %s", judge["verdict_rationale"] or "<empty>")
    score = {
        "test_id": test_case["id"],
        "response_observations": observations,
        "judge": judge,
        "final": {"final_label": final_label},
    }
    (attempt_dir / "score.json").write_text(json.dumps(score, indent=2), encoding="utf-8")
    (attempt_dir / "score.md").write_text(
        f"# Verdict: {final_label.upper()}\n\n"
        f"- Test: `{test_case['id']}`\n"
        f"- Judge: `{judge['label']}` (confidence: `{judge['confidence']}`)\n\n"
        f"## Material Gaps\n{judge['material_gaps']}\n\n"
        f"## Verdict Rationale\n{judge['verdict_rationale']}\n",
        encoding="utf-8",
    )
    return score


def unique_values(values: list[Any]) -> str:
    return ",".join(dict.fromkeys(str(value) for value in values if value not in (None, "")).keys())


def sum_token_usage(attempt_results: list[dict[str, Any]], key: str) -> int:
    total = 0
    for result in attempt_results:
        for usage in result["invoke"].get("token_usage", []):
            if isinstance(usage, dict) and isinstance(usage.get(key), int):
                total += usage[key]
    return total


def sum_tool_output_truncated_tokens(attempt_results: list[dict[str, Any]]) -> int:
    total = 0
    for result in attempt_results:
        for count in result["invoke"].get("truncation_marker_token_counts", []):
            if isinstance(count, int):
                total += count
    return total


def record_evaluation_properties(
    record_property,
    aggregate: dict[str, Any],
    attempt_results: list[dict[str, Any]],
) -> None:
    """Record pytest/JUnit metadata that indexes the detailed artefacts."""
    record_property("ai_test_id", aggregate["test_id"])
    record_property("ai_mode", aggregate["mode"])
    record_property("ai_attempt", aggregate["attempt"])
    record_property("ai_attempts_total", aggregate["attempts_total"])
    record_property("ai_attempts", aggregate["attempts"])
    record_property("ai_passes", aggregate["passes"])
    record_property("ai_pass_rate", aggregate["pass_rate"])
    record_property("ai_scores", ",".join(aggregate["scores"]))
    record_property("ai_display_scores", ",".join(aggregate["display_scores"]))
    record_property("ai_expected_failure", aggregate["expected_failure"])
    if aggregate["expected_failure_reason"]:
        record_property("ai_expected_failure_reason", aggregate["expected_failure_reason"])
    record_property("ai_artifact_dir", aggregate["artifact_dir"])
    record_property("ai_artifact_rel_dir", aggregate["artifact_rel_dir"])
    record_property("ai_aggregate_path", aggregate["aggregate_path"])

    run_ids = unique_values([result["run_meta"].get("run_id") for result in attempt_results])
    archive_shas = unique_values([result["run_meta"].get("archive_sha256") for result in attempt_results])
    judge_labels = unique_values([result["score"].get("judge", {}).get("label") for result in attempt_results])
    judge_confidences = unique_values([result["score"].get("judge", {}).get("confidence") for result in attempt_results])
    rest_reasoning_efforts = unique_values(
        [
            result["invoke"].get("request_reasoning_effort") or result["invoke"].get("reasoning_effort")
            for result in attempt_results
            if result["invoke"].get("mode") == REST_MODE
        ]
    )
    artifact_paths = {
        "ai_llm_response_path": "llm_response.md",
        "ai_score_path": "score.md",
        "ai_score_json_path": "score.json",
        "ai_invoke_metadata_path": "invoke_metadata.json",
        "ai_run_meta_path": "run_meta.json",
    }
    attempt_dirs = [Path(result["attempt_dir"]) for result in attempt_results]
    if run_ids:
        record_property("ai_run_ids", run_ids)
    if archive_shas:
        record_property("ai_archive_sha256s", archive_shas)
    if judge_labels:
        record_property("ai_judge_labels", judge_labels)
    if judge_confidences:
        record_property("ai_judge_confidences", judge_confidences)
    if rest_reasoning_efforts:
        record_property("ai_rest_reasoning_effort", rest_reasoning_efforts)
    for property_name, filename in artifact_paths.items():
        paths = unique_values([attempt_dir / filename for attempt_dir in attempt_dirs])
        if paths:
            record_property(property_name, paths)

    agent_duration = sum(
        result["invoke"].get("duration_seconds", 0)
        for result in attempt_results
        if isinstance(result["invoke"].get("duration_seconds"), (int, float))
    )
    mcp_tool_calls_succeeded = sum(
        result["invoke"].get("mcp", {}).get("tool_calls_succeeded", 0)
        for result in attempt_results
        if isinstance(result["invoke"].get("mcp"), dict)
    )
    mcp_tool_calls_failed = sum(
        result["invoke"].get("mcp", {}).get("tool_calls_failed", 0)
        for result in attempt_results
        if isinstance(result["invoke"].get("mcp"), dict)
    )
    mcp_tool_duration_seconds_succeeded = sum(
        result["invoke"].get("mcp", {}).get("tool_duration_seconds_succeeded", 0)
        for result in attempt_results
        if isinstance(
            result["invoke"].get("mcp", {}).get("tool_duration_seconds_succeeded"),
            (int, float),
        )
    )
    mcp_tool_duration_seconds_failed = sum(
        result["invoke"].get("mcp", {}).get("tool_duration_seconds_failed", 0)
        for result in attempt_results
        if isinstance(
            result["invoke"].get("mcp", {}).get("tool_duration_seconds_failed"),
            (int, float),
        )
    )
    truncation_markers = sum(
        result["invoke"].get("truncation_markers", 0)
        for result in attempt_results
        if isinstance(result["invoke"].get("truncation_markers", 0), int)
    )
    truncation_token_counts = [
        count
        for result in attempt_results
        for count in result["invoke"].get("truncation_marker_token_counts", [])
        if isinstance(count, int)
    ]
    truncation_severities = unique_values(
        [
            tool_output_truncation_severity(aggregate["mode"], result["invoke"])
            for result in attempt_results
        ]
    )
    record_property("ai_agent_duration_seconds", round(agent_duration, 3))
    record_property("ai_mcp_tool_calls_succeeded", mcp_tool_calls_succeeded)
    record_property("ai_mcp_tool_calls_failed", mcp_tool_calls_failed)
    record_property(
        "ai_mcp_tool_duration_seconds_succeeded",
        round(mcp_tool_duration_seconds_succeeded, 3),
    )
    record_property(
        "ai_mcp_tool_duration_seconds_failed",
        round(mcp_tool_duration_seconds_failed, 3),
    )
    record_property("ai_tool_output_truncated", truncation_markers > 0)
    record_property("ai_tool_output_truncation_markers", truncation_markers)
    record_property("ai_tool_output_truncated_tokens", sum_tool_output_truncated_tokens(attempt_results))
    if truncation_token_counts:
        record_property(
            "ai_tool_output_truncation_token_counts",
            ",".join(str(count) for count in truncation_token_counts),
        )
    if truncation_severities:
        record_property("ai_tool_output_truncation_severity", truncation_severities)
    for key in ("input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens"):
        record_property(f"ai_{key}", sum_token_usage(attempt_results, key))


def record_evaluation_suite_properties(pytestconfig, record_testsuite_property, cfg: dict[str, Any]) -> None:
    """Record run-wide model configuration once instead of per attempt."""
    if getattr(pytestconfig, "_ai_insights_suite_properties_recorded", False):
        return

    record_testsuite_property("ai_model", cfg["model"])
    if cfg["reasoning_effort"]:
        record_testsuite_property("ai_reasoning_effort", cfg["reasoning_effort"])
    record_testsuite_property("ai_judge_model", cfg["judge_model"])
    record_testsuite_property("ai_results_dir", str(cfg["results_dir"]))
    record_testsuite_property("ai_manifest_path", str(cfg["manifest_path"]))
    pytestconfig._ai_insights_suite_summary = {
        "model": cfg["model"],
        "reasoning_effort": cfg["reasoning_effort"],
        "judge_model": cfg["judge_model"],
        "results_dir": str(cfg["results_dir"]),
        "manifest_path": str(cfg["manifest_path"]),
    }
    pytestconfig._ai_insights_suite_properties_recorded = True


def wrap_failure_text(
    text: str,
    prefix: str = "  ",
    break_long_words: bool = False,
) -> list[str]:
    """Wrap text for pytest reporters that render failures as preformatted text."""
    try:
        width = shutil.get_terminal_size().columns
    except OSError:
        width = FAILURE_TEXT_FALLBACK_WIDTH
    lines = []
    for raw_line in str(text).splitlines() or [""]:
        if not raw_line.strip():
            lines.append(prefix.rstrip())
            continue
        lines.extend(
            textwrap.wrap(
                raw_line.strip(),
                width=width,
                initial_indent=prefix,
                subsequent_indent=prefix,
                break_long_words=break_long_words,
                break_on_hyphens=False,
            )
        )
    return lines


def format_failure_field(label: str, value: Any) -> list[str]:
    if not value:
        return []
    return [f"{label}:", *wrap_failure_text(str(value))]


def format_failure_artifacts(attempt_dir: Path, cfg: dict[str, Any]) -> list[str]:
    lines = ["Artifacts:"]
    try:
        rel_dir = attempt_dir.relative_to(cfg["results_dir"])
        lines.append(f"  result directory: {rel_dir}")
    except ValueError:
        pass
    lines.extend(
        [
            "  full result directory:",
            *wrap_failure_text(str(attempt_dir), prefix="    ", break_long_words=True),
            "  score: score.md",
            "  generated response: llm_response.md",
        ]
    )
    return lines


def format_response_excerpt(response_path: Path) -> list[str]:
    """Return a bounded response excerpt for pytest failure output."""
    if not response_path.is_file():
        return ["Generated AI Insight excerpt: <not available; response file is missing>"]

    response = response_path.read_text(encoding="utf-8", errors="replace").strip()
    if not response:
        return ["Generated AI Insight excerpt: <empty>"]

    response_lines = response.splitlines()
    excerpt = "\n".join(response_lines[:FAILURE_RESPONSE_MAX_LINES])
    truncated = len(response_lines) > FAILURE_RESPONSE_MAX_LINES
    if len(excerpt) > FAILURE_RESPONSE_MAX_CHARS:
        excerpt = excerpt[:FAILURE_RESPONSE_MAX_CHARS].rstrip()
        truncated = True

    lines = ["Generated AI Insight excerpt:", *wrap_failure_text(excerpt)]
    if truncated:
        lines.append(
            (
                f"[truncated to {FAILURE_RESPONSE_MAX_LINES} lines/"
                f"{FAILURE_RESPONSE_MAX_CHARS} chars; see full response artefact]"
            )
        )
    return lines


def format_evaluation_failure(
    aggregate: dict[str, Any],
    attempt_results: list[dict[str, Any]],
    cfg: dict[str, Any],
    failed_requirement: str,
) -> str:
    test_id = aggregate["test_id"]
    mode = aggregate["mode"]
    scores = ", ".join(aggregate["scores"])
    lines = [
        "AI Insights judge verdict: FAIL",
        "",
        f"Test: {test_id} / {mode} / attempt {aggregate['attempt']}/{aggregate['attempts_total']}",
        "The generated AI Insight did not satisfy the testcase rubric.",
        f"Failed requirement: {failed_requirement}",
        f"Scores: {scores}",
    ]

    for result in attempt_results:
        score = result["score"]
        judge = score.get("judge", {})
        attempt_dir = Path(result["attempt_dir"])
        attempt_label = attempt_dir.name
        final_label = score.get("final", {}).get("final_label", "unknown")
        judge_label = judge.get("label", "unknown")
        judge_confidence = judge.get("confidence", "unknown")
        response_path = attempt_dir / "llm_response.md"
        lines.extend(
            [
                "",
                f"Attempt {attempt_label}",
                f"  result: {final_label}",
                f"  judge: {judge_label} (confidence: {judge_confidence})",
            ]
        )
        lines.extend(format_failure_field("Material gaps", judge.get("material_gaps")))
        lines.extend(format_failure_field("Verdict rationale", judge.get("verdict_rationale")))
        lines.extend(["", *format_failure_artifacts(attempt_dir, cfg), "", *format_response_excerpt(response_path)])

    return "\n".join(lines)


def run_attempt(pytestconfig, test_case: dict[str, Any], mode: str, cfg: dict[str, Any], attempt_num: int) -> dict[str, Any]:
    if mode != REST_MODE and mode not in MCP_MODES:
        raise ValueError(f"unsupported AI Insights mode: {mode}")

    LOGGER.info("Starting %s/%s attempt %03d", test_case["id"], mode, attempt_num)
    run_meta = import_run_cached(pytestconfig, test_case, cfg)
    attempt_dir = cfg["results_dir"] / test_case["id"] / mode / "attempts" / f"{attempt_num:03d}"
    if attempt_dir.exists():
        shutil.rmtree(attempt_dir)
    attempt_dir.mkdir(parents=True)
    full_run_meta = {
        **run_meta,
        "test_id": test_case["id"],
        "mode": mode,
        "cli_bin": str(cfg["cli_bin"]),
        "recipe": test_case.get("recipe", cfg["defaults"].get("recipe")),
        "target": test_case.get("target", cfg["defaults"].get("target")),
        "model_config_id": cfg["model_config_id"],
    }
    (attempt_dir / "run_meta.json").write_text(json.dumps(full_run_meta, indent=2), encoding="utf-8")
    try:
        if mode == REST_MODE:
            invoke_meta = invoke_rest_mode(attempt_dir, full_run_meta, cfg)
        else:
            invoke_meta = invoke_mcp_mode(attempt_dir, full_run_meta, cfg, MCP_MODES[mode])
        if should_fail_on_tool_output_truncation(mode, invoke_meta):
            score = score_truncated_attempt(attempt_dir, test_case, invoke_meta)
        else:
            score = score_attempt(attempt_dir, test_case, cfg)
    except Exception as exc:
        LOGGER.exception("%s/%s attempt %03d failed", test_case["id"], mode, attempt_num)
        invoke_meta = {"status": "error", "error": str(exc)}
        score = {
            "test_id": test_case["id"],
            "response_observations": {"issues": ["invocation_or_judge_error"], "details": [str(exc)]},
            "judge": {},
            "final": {"final_label": "error"},
        }
        (attempt_dir / "score.json").write_text(json.dumps(score, indent=2), encoding="utf-8")
    (attempt_dir / "invoke_metadata.json").write_text(json.dumps(invoke_meta, indent=2), encoding="utf-8")
    return {"attempt_dir": attempt_dir, "run_meta": full_run_meta, "invoke": invoke_meta, "score": score}


def test_ai_insights(
    pytestconfig,
    record_property,
    record_testsuite_property,
    test_case: dict[str, Any],
    mode: str,
    attempt: int,
    attempts_total: int,
):
    config_error = None
    try:
        cfg = resolve_config(pytestconfig)
    except AiInsightsConfigError as exc:
        config_error = str(exc)
    if config_error:
        pytest.fail(config_error, pytrace=False)
    record_evaluation_suite_properties(pytestconfig, record_testsuite_property, cfg)
    LOGGER.info(
        "Running AI Insights evaluation: test=%s mode=%s attempt=%d/%d model=%s",
        test_case["id"],
        mode,
        attempt,
        attempts_total,
        cfg["model"],
    )
    attempt_results = [run_attempt(pytestconfig, test_case, mode, cfg, attempt)]
    scores = [result["score"] for result in attempt_results]
    expectation = expected_failure_for_mode(test_case, mode)
    expected_failure = bool(expectation["expected_failure"])
    expected_failure_reason = str(expectation["reason"])
    display_scores = [
        display_label(score["final"]["final_label"], expected_failure=expected_failure)
        for score in scores
    ]
    passes = sum(1 for score in scores if score["final"]["final_label"] == "pass")
    aggregate = {
        "test_id": test_case["id"],
        "mode": mode,
        "attempt": attempt,
        "attempts_total": attempts_total,
        "attempts": len(scores),
        "passes": passes,
        "pass_rate": passes / len(scores) if scores else 0,
        "scores": [score["final"]["final_label"] for score in scores],
        "display_scores": display_scores,
        "expected_failure": expected_failure,
        "expected_failure_reason": expected_failure_reason,
    }
    aggregate_path = cfg["results_dir"] / test_case["id"] / mode / "attempts" / f"{attempt:03d}" / "aggregate.json"
    aggregate_path.parent.mkdir(parents=True, exist_ok=True)
    artifact_dir = aggregate_path.parent
    aggregate["artifact_dir"] = str(artifact_dir)
    aggregate["artifact_rel_dir"] = str(artifact_dir.relative_to(cfg["results_dir"]))
    aggregate["aggregate_path"] = str(aggregate_path)
    aggregate_path.write_text(json.dumps(aggregate, indent=2), encoding="utf-8")
    record_evaluation_properties(record_property, aggregate, attempt_results)
    LOGGER.info(
        "Aggregate result for %s/%s: %d/%d pass, pass_rate=%.2f",
        test_case["id"],
        mode,
        passes,
        len(scores),
        aggregate["pass_rate"],
    )
    final_label = scores[0]["final"]["final_label"]
    if should_xfail_final_label(final_label, expected_failure=expected_failure):
        pytest.xfail(expected_failure_reason or "expected AI Insights failure")
    if final_label != "pass":
        pytest.fail(
            format_evaluation_failure(
                aggregate,
                attempt_results,
                cfg,
                "final_label == pass",
            ),
            pytrace=False,
        )
