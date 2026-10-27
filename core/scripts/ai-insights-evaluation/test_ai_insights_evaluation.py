# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Evaluate AI Insights answers for known Performix analysis scenarios.

AI Insights are a Performix feature exposed via the Performix MCP Server which
provide LLM-generated explanations and recommendations of a Performix run.
They should identify the relevant performance issue, connect it to evidence in
the run, and suggest a useful next action.

This is a pytest evaluation suite for AI Insights. For each test case it:
1. Prepares the workload context, optionally importing a pre-recorded run for Pathways.
2. Invokes the selected AI Insights implementation (via an MCP client such as codex) and records the response.
3. Evaluates whether the answer matches the expected analysis for each testcase.

The expected answer is semantic rather than byte-for-byte stable, so the
suite uses a separate LLM judge with private rubrics. The judge checks
whether the response contains the required insight, evidence, and
recommendation without exposing those rubrics to the model under test.

The default Act 1 run uses production Performix MCP; see README.md for the
full local setup and reporting details.
"""

from __future__ import annotations

import json
import ipaddress
import logging
import os
import re
import shutil
import signal
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

from performix_attempt import PerformixAttempt, performix_env
from pathways_target_access import TargetAccessError

# The internal API proxy can use enterprise certificate authorities which are
# available through the operating-system trust store but not Python's CA bundle.
truststore.inject_into_ssl()

from codex_session_metrics import (
    codex_session_log,
    collect_mcp_tool_metrics,
    iter_jsonl,
    persist_codex_session_log,
)
from pathways_artifacts import load_pathways_judge_evidence
from performance_quality import PERFORMIX_MCP_MODE
from recipe_support import mode_supports_recipe, requires_source_archive, resolve_evaluation_inputs

sys.path.append(str(Path(__file__).resolve().parents[1]))
from run_export_helper import sha256_file

DEFAULT_API_BASE = "https://openai-api-proxy.geo.arm.com/api/providers/openai/v1"
PERFORMIX_SERVER = "arm-performix"
PERFORMIX_TOOL = "generate_ai_insights"
RETRYABLE_HTTP_STATUS = {429, 500, 502, 503, 504}
DEFAULT_PATHWAYS_JUDGE_MAX_REQUEST_BYTES = 4 * 1024 * 1024
FAILURE_RESPONSE_MAX_CHARS = 4000
FAILURE_RESPONSE_MAX_LINES = 80
FAILURE_TEXT_FALLBACK_WIDTH = 80
MCP_RUN_ID_PLACEHOLDER = "{run_id}"
PROMPTS_DIR = Path(__file__).resolve().parent / "prompts"


def read_prompt(name: str) -> str:
    """Load prompt text; license metadata lives in sibling .license files."""
    return (PROMPTS_DIR / name).read_text(encoding="utf-8").strip()


DEFAULT_MCP_PROMPT = read_prompt("agent_ai_insights.md")
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
    args: tuple[str, ...]
    extra_tools: tuple[str, ...] = ()
    required_tools: tuple[str, ...] = ()
    environment: tuple[tuple[str, str], ...] = ()


MCP_MODES = {
    # Pin the control to curated mode so a caller's environment cannot change
    # the baseline during a comparison.
    PERFORMIX_MCP_MODE: McpServer(
        id=PERFORMIX_MCP_MODE,
        server=PERFORMIX_SERVER,
        tool=PERFORMIX_TOOL,
        args=("mcp", "start", "--acknowledge-source-sharing"),
        extra_tools=("read_ai_insights_payload_details",),
        environment=(("APXD_CODE_HOTSPOTS_AI_INSIGHTS_MODE", "curated"),),
    ),
    # This candidate is limited to Code Hotspots by mode_supports_recipe().
    "performix_mcp_run_query": McpServer(
        id="performix_mcp_run_query",
        server=PERFORMIX_SERVER,
        tool=PERFORMIX_TOOL,
        args=("mcp", "start", "--acknowledge-source-sharing"),
        environment=(("APXD_CODE_HOTSPOTS_AI_INSIGHTS_MODE", "run_query"),),
    ),
}
PATHWAYS_EXECUTION_MODE = "host_ssh"
DEFAULT_PATHWAYS_AGENT_TIMEOUT_SECONDS = 600
CODEX_WORKSPACE_DIRNAME = "codex_workspace"
TARGET_PROMPT_KEYS = (
    "target_host",
    "target_port",
    "target_user",
    "target_private_key_path",
)
UNSUPPLIED_PROMPT_VALUE = "not supplied"


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def manifest_path(pytestconfig) -> Path:
    return Path(pytestconfig.getoption("--ai-manifest")).expanduser().resolve()


def selected_acts(pytestconfig) -> set[str]:
    raw = pytestconfig.getoption("--ai-act")
    return {act.strip() for act in str(raw).split(",") if act.strip()}


def selected_recipes(pytestconfig) -> set[str]:
    raw = pytestconfig.getoption("--ai-recipe")
    return {recipe.strip() for recipe in str(raw).split(",") if recipe.strip()}


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


def pathways_config_mapping(value: Any, path: str, *, allowed: set[str]) -> dict[str, Any]:
    """Return a credential-free pathways configuration mapping at *path*."""
    if not isinstance(value, dict):
        raise pytest.UsageError(f"pathways execution configuration {path} must be an object")
    unknown = set(value) - allowed
    if unknown:
        raise pytest.UsageError(
            f"pathways execution configuration {path} contains unsupported fields: "
            f"{', '.join(sorted(unknown))}"
        )
    return value


def pathways_config_string(value: dict[str, Any], key: str, path: str) -> str:
    """Return a required non-empty string from a pathways configuration mapping."""
    result = value.get(key)
    if not isinstance(result, str) or not result.strip():
        raise pytest.UsageError(f"pathways execution configuration {path}.{key} must be a non-empty string")
    return result


def pathways_agent_timeout_seconds(test_case: dict[str, Any]) -> int:
    """Return the positive time limit for the agent's Pathways investigation."""
    timeout = test_case["pathways_execution"].get(
        "agent_timeout_seconds", DEFAULT_PATHWAYS_AGENT_TIMEOUT_SECONDS,
    )
    if type(timeout) is not int or timeout <= 0:
        raise pytest.UsageError(
            f"testcase {test_case.get('id', '<unknown>')} "
            "pathways_execution.agent_timeout_seconds must be a positive integer"
        )
    return timeout


def validate_pathways_execution_config(test_case: dict[str, Any]) -> None:
    """Validate the declarative, credential-free host-over-SSH configuration."""
    config = test_case.get("pathways_execution")
    if config is None:
        return

    test_id = test_case.get("id", "<unknown>")
    prefix = f"testcase {test_id} pathways_execution"
    config = pathways_config_mapping(
        config,
        prefix,
        allowed={"mode", "workload_source", "agent_timeout_seconds"},
    )
    pathways_agent_timeout_seconds(test_case)
    if pathways_config_string(config, "mode", prefix) != PATHWAYS_EXECUTION_MODE:
        raise pytest.UsageError(
            f"{prefix}.mode must be {PATHWAYS_EXECUTION_MODE!r}"
        )

    workload_source = pathways_config_mapping(
        config.get("workload_source"),
        f"{prefix}.workload_source",
        allowed={"repository", "branch"},
    )
    for key in ("repository", "branch"):
        pathways_config_string(workload_source, key, f"{prefix}.workload_source")
    if workload_source["branch"] != "main":
        raise pytest.UsageError(f"{prefix}.workload_source.branch must be 'main'")
    if "run_artifact" in test_case:
        pathways_config_string(test_case, "run_artifact", f"testcase {test_id}")
    else:
        prompt = pathways_config_string(test_case, "mcp_prompt", f"testcase {test_id}")
        if MCP_RUN_ID_PLACEHOLDER in prompt:
            raise pytest.UsageError(f"testcase {test_id} mcp_prompt cannot use {{run_id}} without run_artifact")


def validate_selected_recipes(recipes: set[str], manifest: dict[str, Any]) -> None:
    if not recipes:
        return

    defaults = manifest.get("defaults", {})
    available = set()
    for test_case in manifest.get("tests", []):
        recipe = resolve_evaluation_inputs(test_case, defaults).get("recipe")
        if recipe:
            available.add(recipe)
    unknown = recipes - available
    if unknown:
        requested_text = ", ".join(sorted(recipes))
        unknown_text = ", ".join(sorted(unknown))
        available_text = ", ".join(sorted(available)) if available else "none"
        raise pytest.UsageError(
            f"Unknown AI Insights recipe selection: {unknown_text}. "
            f"Requested recipes: {requested_text}. Available recipes: {available_text}."
        )


def iter_manifest_parameters(pytestconfig, manifest: dict[str, Any]) -> list[dict[str, Any]]:
    defaults = manifest.get("defaults", {})
    cli_modes = selected_modes(pytestconfig)
    default_modes = defaults.get("modes") or [PERFORMIX_MCP_MODE]
    modes = cli_modes or default_modes
    acts = selected_acts(pytestconfig)
    recipes = selected_recipes(pytestconfig)
    validate_selected_acts(acts, manifest)
    validate_selected_recipes(recipes, manifest)
    params: list[dict[str, Any]] = []
    for test_case in manifest.get("tests", []):
        if acts and not acts.intersection(test_case.get("acts", [])):
            continue
        validate_pathways_execution_config(test_case)
        test_case = resolve_evaluation_inputs(test_case, defaults)
        if recipes and test_case.get("recipe") not in recipes:
            continue
        allowed_modes = test_case.get("evaluation_modes")
        if allowed_modes is not None:
            if (
                not isinstance(allowed_modes, list)
                or not allowed_modes
                or any(not isinstance(mode, str) or not mode for mode in allowed_modes)
            ):
                raise ValueError(f"invalid evaluation_modes for {test_case['id']}")
            for allowed_mode in allowed_modes:
                if not mode_supports_recipe(allowed_mode, test_case["recipe"]):
                    raise ValueError(
                        f"evaluation mode {allowed_mode!r} does not support recipe "
                        f"{test_case['recipe']!r} for testcase {test_case['id']!r}"
                    )
        for mode in modes:
            if allowed_modes is not None and mode not in allowed_modes:
                continue
            if "recipe" in test_case and not mode_supports_recipe(mode, test_case["recipe"]):
                continue
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
    """Return whether a one-shot AI Insights evidence call was truncated."""
    marker_count = tool_output_truncation_markers(invoke_meta)
    if marker_count <= 0:
        return False
    details = invoke_meta.get("truncation_details")
    if not isinstance(details, list) or len(details) != marker_count:
        return True
    mode_config = MCP_MODES.get(mode)
    if mode_config is None:
        return True
    expected_namespace = codex_mcp_namespace(mode_config.server)
    one_shot_evidence_tools = {mode_config.tool, *mode_config.extra_tools}
    return any(
        isinstance(detail, dict)
        and detail.get("namespace") == expected_namespace
        and detail.get("tool") in one_shot_evidence_tools
        for detail in details
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


def target_prompt_values(pytestconfig) -> dict[str, str]:
    """Return optional target values for pathways prompt rendering."""
    return {
        key: str(pytestconfig.getoption(f"--ai-{key.replace('_', '-')}") or "").strip()
        for key in TARGET_PROMPT_KEYS
    }


def target_network_domain(target_host: str) -> str | None:
    """Return a literal target host for the Codex network allowlist."""
    value = target_host.strip()
    if not value:
        return None
    ip_literal = value[1:-1] if value.startswith("[") and value.endswith("]") else value
    try:
        return str(ipaddress.ip_address(ip_literal))
    except ValueError:
        pass
    # Protect the network allowlist from patterns, URLs, ports and whitespace;
    # SSH and name resolution handle whether the destination is valid/reachable.
    if not re.fullmatch(r"[A-Za-z0-9._-]+", value):
        raise AiInsightsConfigError(
            "--ai-target-host must be a literal hostname or IP address without a port"
        )
    return value.lower()


def resolve_mcp_prompt(
    test_case: dict[str, Any],
    run_id: str | None,
    target_values: dict[str, str] | None = None,
    *,
    ssh_command: str = UNSUPPLIED_PROMPT_VALUE,
) -> str:
    """Resolve the investigation prompt, substituting a fixture ID when supplied."""
    prompt = test_case.get("mcp_prompt", DEFAULT_MCP_PROMPT if run_id else None)
    test_id = test_case.get("id", "<unknown>")
    if not isinstance(prompt, str) or not prompt.strip():
        raise ValueError(f"testcase {test_id} mcp_prompt must be a non-empty string")
    if not run_id and MCP_RUN_ID_PLACEHOLDER in prompt:
        raise ValueError(f"testcase {test_id} mcp_prompt cannot use {{run_id}} without a supplied run")
    resolved = prompt.replace(MCP_RUN_ID_PLACEHOLDER, run_id) if run_id else prompt
    target_values = {
        key: (target_values or {}).get(key, "").strip() or UNSUPPLIED_PROMPT_VALUE
        for key in TARGET_PROMPT_KEYS
    }
    for key, value in target_values.items():
        resolved = resolved.replace(f"{{{key}}}", value)
    if is_pathways_execution(test_case):
        resolved = resolved.rstrip() + "\n\n" + read_prompt("agent_pathways.md").format(
            ssh_command=ssh_command, run_id=run_id or UNSUPPLIED_PROMPT_VALUE, **target_values,
        )
    return resolved if resolved.endswith("\n") else f"{resolved}\n"


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
    LOGGER.info("Loaded AI Insights evaluation manifest: %s", path)
    return {
        "manifest_path": path,
        "manifest": manifest,
        "run_artifact_base": required_path("prerecorded_run_cache", run_base) if run_base else None,
        "cli_bin": Path(pytestconfig.getoption("--ai-cli-bin")).expanduser().resolve(),
        "results_dir": Path(pytestconfig.getoption("--ai-results-dir")).expanduser().resolve(),
        "model": model,
        "model_config_id": model_config.get("id", model),
        "reasoning_effort": reasoning_effort,
        "judge_model": pytestconfig.getoption("--ai-judge-model"),
        "judge_reasoning_effort": pytestconfig.getoption("--ai-judge-reasoning-effort"),
        "openai_api_key": openai_api_key,
        "attempts": attempts,
        "target_prompt_values": target_prompt_values(pytestconfig),
        "defaults": defaults,
    }


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


def import_attempt_run(test_case, cfg, performix):
    """Import this attempt's fixture, or return no run for a live-only case."""
    if is_pathways_execution(test_case) and "run_artifact" not in test_case:
        return {"run_id": None}
    archive = cfg["run_artifact_base"] / test_case["run_artifact"]
    if not archive.is_file():
        raise FileNotFoundError(
            f"missing pre-recorded run archive for {test_case['id']}: {archive}"
        )
    LOGGER.info("Using pre-recorded run archive for %s: %s", test_case["id"], archive)
    archive_sha = sha256_file(archive)
    source_archive_sha = ""
    source_archive_path = ""
    source_root = ""
    if requires_source_archive(test_case["recipe"], test_case.get("recipe_params")):
        source_archive = archive.parent / "test_src.zip"
        if not source_archive.is_file():
            raise FileNotFoundError(
                f"missing pre-recorded source archive for {test_case['id']}: {source_archive}"
            )
        source_archive_sha = sha256_file(source_archive)
        source_archive_path = str(source_archive)
        source_root = str(
            extract_source_archive(
                source_archive,
                source_archive_sha,
                cfg,
                test_case["id"],
            )
        )
    run_id = performix.import_fixture(cfg["cli_bin"], archive, source_root)
    run_meta = {
        "run_id": run_id,
        "archive_sha256": archive_sha,
        "archive_path": str(archive),
    }
    if source_root:
        run_meta.update(
            {
                "source_archive_sha256": source_archive_sha,
                "source_archive_path": source_archive_path,
                "source_root": source_root,
            }
        )
    return run_meta


def run_workload_git(command: list[str], failure_context: str) -> str:
    """Run one host-side Git command for a harness-prepared checkout."""
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=1800)
    except OSError as exc:
        raise RuntimeError(f"{failure_context}: {exc}") from exc
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise RuntimeError(f"{failure_context}: {detail or f'exit code {result.returncode}'}")
    return result.stdout.strip()


def prepare_agent_workspace(attempt_dir: Path, test_case: dict[str, Any]) -> tuple[Path, dict[str, Any] | None]:
    """Create the agent workspace, cloning the workloads repository for pathways."""
    workspace = attempt_dir / CODEX_WORKSPACE_DIRNAME
    if workspace.exists():
        raise RuntimeError(f"agent workspace already exists: {workspace}")

    if not is_pathways_execution(test_case):
        workspace.mkdir(parents=True)
        return workspace, None
    if shutil.which("git") is None:
        raise RuntimeError("missing required command: git")

    # Collection validates the configuration; normalize checkout inputs only here.
    workload_source = {
        key: test_case["pathways_execution"]["workload_source"][key].strip()
        for key in ("repository", "branch")
    }
    run_workload_git(
        ["git", "clone", "--branch", workload_source["branch"], "--single-branch", workload_source["repository"], str(workspace)],
        "workload checkout clone failed",
    )
    commit = run_workload_git(
        ["git", "-C", str(workspace), "rev-parse", "HEAD"],
        "workload checkout revision lookup failed",
    )
    if not commit:
        raise RuntimeError("workload checkout revision lookup returned no commit")
    initial_status = run_workload_git(
        ["git", "-C", str(workspace), "status", "--porcelain"],
        "workload checkout status lookup failed",
    )
    if initial_status:
        raise RuntimeError("workload checkout is not clean after clone")
    return workspace, {
        "repository": workload_source["repository"],
        "branch": workload_source["branch"],
        "commit": commit,
        "initial_status": "clean",
        "workspace": str(workspace),
    }


def is_pathways_execution(test_case: dict[str, Any]) -> bool:
    """Return whether a testcase uses the pathways execution configuration."""
    return test_case.get("pathways_execution") is not None


def toml_string(value: str) -> str:
    return json.dumps(value)


def write_codex_home(
    attempt_dir: Path,
    workspace: Path,
    cfg: dict[str, Any],
    mode: McpServer,
    pathways_execution: bool,
    performix_state: dict[str, Any],
) -> Path:
    """Create the isolated Codex configuration used for one MCP attempt.

    The model under test should obtain AI Insights through the configured
    MCP server, not through the developer's normal Codex configuration or
    arbitrary filesystem/network access. Each attempt therefore gets a
    generated CODEX_HOME with one MCP server and a profile appropriate to its
    manifest contract.
    """
    codex_home = attempt_dir / "codex_home"
    LOGGER.debug("Preparing isolated Codex home: %s", codex_home)
    if codex_home.exists():
        shutil.rmtree(codex_home)
    codex_home.mkdir(parents=True)

    server_env = {
        "APXD_ENABLE_EXPERIMENTAL_RECIPES": "true",
        **dict(mode.environment),
        **performix_env(performix_state),
    }
    env_lines = "\n".join(f"{key} = {toml_string(value)}" for key, value in server_env.items())
    LOGGER.debug("Using %s MCP server from: %s", mode.id, cfg["cli_bin"].parent)
    shell_tool = "true" if pathways_execution else "false"
    workspace_access = "write" if pathways_execution else "read"
    target_domain = None
    if pathways_execution:
        target_domain = target_network_domain(
            cfg["target_prompt_values"]["target_host"]
        )
    network_enabled = "true" if target_domain else "false"
    network_proxy = "true" if target_domain else "false"
    shell_comment = (
        "# Pathways may use the prepared checkout and SSH to the supplied target."
        if pathways_execution
        else "# The model under test obtains Performix evidence through MCP only."
    )
    network_comment = (
        "# Only the supplied SSH target is reachable from pathway commands."
        if target_domain
        else "# The model under test obtains Performix evidence through MCP only."
    )
    network_domain_section = ""
    runtime_read_section = ""
    if pathways_execution:
        # To run commands in its Linux sandbox, Codex starts another copy of itself.
        # Our filesystem restrictions hide the installed Codex binary, so we must
        # allow access to it here; otherwise sandbox commands cannot start.
        # Also allow Python and its libraries so the SSH proxy helper can run.
        runtime_paths = {Path(sys.prefix).resolve(), Path(sys.base_prefix).resolve()}
        codex_bin = shutil.which("codex")
        if codex_bin:
            runtime_paths.add(Path(codex_bin).resolve())
        runtime_read_section = "\n".join(
            f"{toml_string(str(path))} = \"read\"" for path in sorted(runtime_paths)
        )
        if performix_state and performix_state.get("home_dir"):
            runtime_read_section += f'\n{toml_string(performix_state["home_dir"])} = "write"'
    if target_domain:
        network_domain_section = f"""
[permissions.ai-insights.network.domains]
{toml_string(target_domain)} = "allow"
"""
    config = f"""# Use the same OpenAI-compatible proxy path as developer Codex runs.
model_provider = "proxy"

# Evaluation responses are already captured under the attempt artefacts.
disable_response_storage = true

# Keep Codex confined to the prompt workspace and the configured MCP server.
default_permissions = "ai-insights"

# Treat the generated workspace as the project root instead of loading project
# instructions from parent directories in the Performix checkout.
project_root_markers = []

[features]
{shell_comment}
shell_tool = {shell_tool}

# Route command networking through the profile policy when a target is supplied.
network_proxy = {network_proxy}

# Marketplace plugins are not part of the evaluation and would otherwise be
# downloaded into every attempt's isolated CODEX_HOME.
plugins = false

# Codex needs the API key; agent shell commands do not.
[shell_environment_policy.filters]
OPENAI_API_KEY = "exclude"
OPENAI_PROXY_TOKEN = "exclude"

[model_providers.proxy]
name = "OpenAI"
base_url = {toml_string(os.environ.get("OPENAI_API_BASE", DEFAULT_API_BASE).rstrip("/"))}
wire_api = "responses"
env_key = "OPENAI_API_KEY"

[permissions.ai-insights.filesystem]
# Minimal read access lets Codex read its prompt and generated workspace files
# without inheriting access to the developer's normal filesystem.
":minimal" = "read"
{runtime_read_section}
# The MCP server launches outside the shell sandbox; agent commands must not
# read, copy or execute its APX binary, even under an otherwise readable path.
{toml_string(str(cfg["cli_bin"].resolve()))} = "deny"

[permissions.ai-insights.filesystem.":workspace_roots"]
# The generated workspace contains only per-attempt inputs and outputs. A
# pathways attempt may modify only its harness-provided checkout.
"." = "{workspace_access}"

[permissions.ai-insights.network]
{network_comment}
enabled = {network_enabled}
allow_local_binding = {network_enabled}
{network_domain_section}

[mcp_servers.{mode.server}]
command = {toml_string(str(cfg["cli_bin"]))}
args = {json.dumps(list(mode.args))}
cwd = {toml_string(str(cfg["cli_bin"].parent))}
enabled = true
# Allow the configured MCP server to be used non-interactively by pytest.
default_tools_approval_mode = "approve"

[mcp_servers.{mode.server}.env]
# Pass only the environment needed by this MCP mode.
{env_lines}
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


def mcp_tool_failure_message(item: dict[str, Any]) -> str:
    """Return the most useful error message recorded for a failed MCP call."""
    error = item.get("error") or {}
    if message := error.get("message"):
        return str(message)

    result = item.get("result") or {}
    structured_error = (result.get("structured_content") or {}).get("error") or {}
    if message := structured_error.get("message"):
        return str(message)

    return "unknown MCP tool failure"


def validate_mcp_call(raw_jsonl: Path, mode: McpServer) -> dict[str, Any]:
    """Confirm the client used the expected AI Insights MCP tool.

    A non-empty Markdown answer is not enough for this suite: the answer
    must come from the intended MCP path so the test exercises the AI
    Insights integration rather than a generic model response. Individual
    MCP tool failures are retained for reporting, but do not invalidate a
    response which the model produced after obtaining the required AI
    Insights guidance.
    """
    LOGGER.info("Validating %s MCP tool call in Codex event log", mode.id)
    failed_messages: list[str] = []
    completed_by_tool: dict[str, int] = {}
    failed_by_tool: dict[str, int] = {}
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
            failed_by_tool[tool] = failed_by_tool.get(tool, 0) + 1
            failed_messages.append(f"{tool}: {mcp_tool_failure_message(item)}")
    observed_tools = completed_by_tool.keys() | failed_by_tool.keys()
    failure_rate_percent_by_tool = {
        tool: failed_by_tool.get(tool, 0)
        * 100.0
        / (failed_by_tool.get(tool, 0) + completed_by_tool.get(tool, 0))
        for tool in sorted(observed_tools)
    }
    if failed_messages:
        failure_rates = "; ".join(
            f"{tool}: {failed_by_tool[tool]}/"
            f"{failed_by_tool[tool] + completed_by_tool.get(tool, 0)} failed "
            f"({failure_rate_percent_by_tool[tool]:.1f}%)"
            for tool in sorted(failed_by_tool)
        )
        LOGGER.warning(
            "%s MCP tool call failure rate(s): %s. Failure details: %s",
            mode.server,
            failure_rates,
            "; ".join(failed_messages),
        )
    required_tools = (mode.tool, *mode.required_tools)
    required_completed = 0
    for required_tool in required_tools:
        completed = completed_by_tool.get(required_tool, 0)
        if completed <= 0:
            raise RuntimeError(f"{required_tool} did not complete successfully")
        required_completed += completed
    return {
        "server": mode.server,
        "tool": mode.tool,
        "extra_tools": list(mode.extra_tools),
        "required_tools": list(mode.required_tools),
        "completed_calls": sum(completed_by_tool.values()),
        "required_completed_calls": required_completed,
        "completed_by_tool": completed_by_tool,
        "failed_calls": len(failed_messages),
        "failed_by_tool": failed_by_tool,
        "failure_rate_percent_by_tool": failure_rate_percent_by_tool,
        "failed_messages": failed_messages,
    }


def command_output_is_denied_only(output: str) -> bool:
    lowered = output.lower()
    if "permission denied" not in lowered and "operation not permitted" not in lowered:
        return False
    return "Process exited with code 0" not in output


def codex_mcp_namespace(server: str) -> str:
    """Return the model-visible Codex namespace for an MCP server."""
    return f"mcp__{re.sub(r'[^A-Za-z0-9_]', '_', server)}"


def codex_truncation_markers(codex_home: Path, server: str) -> list[dict[str, Any]]:
    """Return tool identities and token counts for truncated Codex outputs.

    `codex_exec.jsonl` records the MCP server, tool, completion status and full
    result. The session transcript records the namespace, tool name, call ID
    and output sent to the model, so it contains any Codex truncation marker.
    Neither file contains all the data needed to validate the call and inspect
    the model-visible output. MCP call validation therefore uses the exec log;
    extract each marker and its tool identity here from the session transcript.
    The expected server is used to reject malformed Performix calls whose tool
    name cannot be read; other truncated tools are retained for warning output.
    """
    LOGGER.debug("Checking Codex session transcript for truncated tool output")
    session_log = codex_session_log(codex_home)
    expected_namespace = codex_mcp_namespace(server)
    calls: dict[str, dict[str, str]] = {}
    markers: list[dict[str, Any]] = []
    for event in iter_jsonl(session_log):
        payload = event.get("payload") or {}
        call_id = str(payload.get("call_id") or "")
        if payload.get("type") == "function_call":
            namespace = payload.get("namespace")
            tool = payload.get("name")
            if namespace == expected_namespace and (not isinstance(tool, str) or not tool):
                raise RuntimeError(
                    f"Codex session transcript recorded a {server} MCP call without a tool name"
                )
            if call_id:
                calls[call_id] = {
                    "namespace": namespace if isinstance(namespace, str) and namespace else "unknown",
                    "tool": tool if isinstance(tool, str) and tool else "unknown",
                }
            continue
        if payload.get("type") != "function_call_output":
            continue
        for match in CODEX_TRUNCATION_MARKER_RE.finditer(str(payload.get("output") or "")):
            call = calls.get(call_id, {"namespace": "unknown", "tool": "unknown"})
            markers.append(
                {
                    **call,
                    "token_count": int(match.group(1)),
                }
            )
    return markers


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


def run_agent(argv, *, timeout=None, **kwargs):
    """Run an agent with file-based stdio; allow graceful shutdown on cancellation."""
    with subprocess.Popen(argv, start_new_session=True, **kwargs) as process:
        try:
            process.wait(timeout=timeout)
        except BaseException:
            try:
                if process.poll() is None and os.name == "posix":
                    process.send_signal(signal.SIGINT)
                    try:
                        process.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        pass
            finally:
                if process.poll() is None:
                    process.kill()
                process.wait()
            raise
        return subprocess.CompletedProcess(argv, process.returncode)


def invoke_mcp_mode(
    attempt_dir: Path,
    run_meta: dict[str, Any],
    test_case: dict[str, Any],
    cfg: dict[str, Any],
    mode: McpServer,
    workspace: Path,
    *, performix: PerformixAttempt,
) -> dict[str, Any]:
    """Run Codex against one AI Insights MCP mode and capture artefacts.

    This owns the model-under-test invocation contract:
    prompt text, raw agent event log, final Markdown response, MCP tool
    validation, external-command validation, and invocation metadata.
    """
    if shutil.which("codex") is None:
        raise RuntimeError("missing required command: codex")

    if not workspace.is_dir():
        raise RuntimeError(f"agent workspace does not exist: {workspace}")
    pathways_execution = is_pathways_execution(test_case)
    codex_home = write_codex_home(
        attempt_dir,
        workspace,
        cfg,
        mode,
        pathways_execution,
        performix.state,
    )

    prompt = resolve_mcp_prompt(
        test_case, run_meta["run_id"], cfg["target_prompt_values"],
        ssh_command=cfg.get("pathways_ssh_command", UNSUPPLIED_PROMPT_VALUE),
    )
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
        run_meta["run_id"] or "none supplied",
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

    env = performix.environment()
    env["CODEX_HOME"] = str(codex_home)
    # Support the local proxy and CI's managed arm-openai provider. Only Codex
    # receives these credentials; its shell policy excludes both names.
    env["OPENAI_API_KEY"] = env["OPENAI_PROXY_TOKEN"] = cfg["openai_api_key"]
    version = subprocess.run(["codex", "--version"], capture_output=True, text=True)
    codex_version = version.stdout.strip() or version.stderr.strip()
    started = time.monotonic()
    with (prompt_path.open("r", encoding="utf-8") as stdin,
          raw_jsonl.open("w", encoding="utf-8") as stdout,
          (attempt_dir / "codex_stderr.txt").open("w", encoding="utf-8") as stderr):
        timeout = pathways_agent_timeout_seconds(test_case) if pathways_execution else None
        process = run_agent(argv, stdin=stdin, stdout=stdout, stderr=stderr, text=True, env=env, timeout=timeout)
    duration = time.monotonic() - started
    if process.returncode != 0:
        detail = (attempt_dir / "codex_stderr.txt").read_text(encoding="utf-8", errors="replace").strip()[-4000:]
        errors = [str(event.get("message") or event.get("error", {}).get("message", ""))
                  for event in iter_jsonl(raw_jsonl) if event.get("type") in ("error", "turn.failed")]
        detail = "\n".join([*errors[-2:], detail]).strip()
        raise RuntimeError(f"codex exec failed with exit code {process.returncode}: {detail}")
    LOGGER.info("Codex invocation completed in %.1fs", duration)

    if not last_message.is_file() or last_message.stat().st_size == 0:
        raise RuntimeError("codex exec produced no final message")
    response_md.write_text(last_message.read_text(encoding="utf-8"), encoding="utf-8")
    LOGGER.info("Captured Codex response: %s (%d bytes)", response_md, response_md.stat().st_size)

    session_jsonl = persist_codex_session_log(codex_home, attempt_dir)
    mcp = validate_mcp_call(raw_jsonl, mode)
    mcp.update(collect_mcp_tool_metrics(codex_home, mode.server))
    truncation_details = codex_truncation_markers(codex_home, mode.server)
    truncation_token_counts = [detail["token_count"] for detail in truncation_details]
    truncation_markers = len(truncation_details)
    successful_commands = successful_external_commands(codex_home)
    if successful_commands and not pathways_execution:
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
        "truncation_details": truncation_details,
        "successful_external_commands": successful_commands,
        "codex_home": str(codex_home),
        "session_jsonl": str(session_jsonl),
        "workspace": str(workspace),
        "prompt_path": str(prompt_path),
        "raw_jsonl": str(raw_jsonl),
        "last_message": str(last_message),
        "response_markdown": str(response_md),
        "response_bytes": response_md.stat().st_size,
    }


def write_score(attempt_dir: Path, test_case: dict[str, Any], score: dict[str, Any], *, failure_phase: str | None = None) -> None:
    """Render both score files from structured results, including any cleanup errors."""
    (attempt_dir / "score.json").write_text(json.dumps(score, indent=2), encoding="utf-8")
    lines = [f"# Verdict: {score['final']['final_label'].upper()}", "", f"- Test: `{score['test_id']}`"]
    judge = score["judge"]
    details = score["response_observations"]["details"]
    if judge:
        lines.extend([
            f"- Judge: `{judge['label']}` (confidence: `{judge['confidence']}`)", "",
            "## Material Gaps", judge["material_gaps"], "",
            "## Verdict Rationale", judge["verdict_rationale"],
        ])
        if is_pathways_execution(test_case) and details:
            lines.extend(["", "## Evaluation Rule Failures", *details])
    else:
        lines.extend([f"- Phase: `{failure_phase}`", "", *details])
    if score.get("cleanup_errors"):
        lines.extend(["", "## Evidence collection error", *score["cleanup_errors"]])
    (attempt_dir / "score.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def score_truncated_attempt(attempt_dir: Path, test_case: dict[str, Any], invoke_meta: dict[str, Any]) -> dict[str, Any]:
    """Record an evaluation failure caused by required tool-output truncation."""
    token_counts = invoke_meta.get("truncation_marker_token_counts", [])
    markers = invoke_meta.get("truncation_markers", len(token_counts))
    details = invoke_meta.get("truncation_details", [])
    detail = (
        "Codex truncated the required AI Insights MCP tool output in the session transcript: "
        f"found {markers} marker(s) with details {details}."
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
    write_score(attempt_dir, test_case, score)
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


def pathways_judge_transcript(raw: str) -> str:
    """Keep readable investigation evidence with references to the original JSONL."""
    evidence = []
    for number, line in enumerate(raw.splitlines(), 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except ValueError:
            event = {"invalid_jsonl": line}
        if not isinstance(event, dict):
            evidence.append({"session_line": number, "event": event})
            continue
        payload = event.get("payload", {})
        if event.get("type") == "event_msg" and payload.get("type") == "token_count":
            continue
        payload.pop("internal_chat_message_metadata_passthrough", None)
        if payload.get("type") == "reasoning":
            payload.pop("encrypted_content", None)  # Opaque ciphertext cannot be judged.
        evidence.append({"session_line": number, "event": event})
    return "\n".join(json.dumps(event, ensure_ascii=False, separators=(",", ":")) for event in evidence)


def judge_response(test_id: str, rubric: str, response_text: str, cfg: dict[str, Any],
                   *, pathways_evidence: dict[str, Any] | None = None) -> dict[str, Any]:
    """Score one AI Insights response with a separate rubric-aware judge.

    Private rubrics are only sent to this direct judge call. They must not
    be included in the prompt or artefacts seen by the model under test.
    """
    api_key = cfg["openai_api_key"]
    LOGGER.info("Judging AI Insights response for %s with %s", test_id, cfg["judge_model"])
    judge_file = "judge_pathways.md" if pathways_evidence is not None else "judge_ai_insights.md"
    judge_task, *judge_instructions = read_prompt(judge_file).split("\n\n")
    prompt = {
        "task": judge_task,
        "instructions": judge_instructions,
        "test_id": test_id,
        "private_rubric": rubric,
        "response_under_test": response_text,
    }
    if pathways_evidence is not None:
        prompt["pathways_evidence"] = {**pathways_evidence,
            "session_jsonl": pathways_judge_transcript(pathways_evidence.get("session_jsonl", ""))}
    else:
        prompt["output_schema"] = {
            "label": "pass|fail",
            "confidence": "low|medium|high",
            "what_was_correct": "string",
            "material_gaps": "string",
            "verdict_rationale": "string",
        }
    body = {
        "model": cfg["judge_model"],
        "reasoning": {"effort": cfg["judge_reasoning_effort"]},
        "input": [{"role": "user", "content": [{"type": "input_text", "text": json.dumps(prompt)}]}],
    }
    if pathways_evidence is not None:
        properties = {
            "label": {"type": "string", "enum": ["pass", "fail"]},
            "confidence": {"type": "string", "enum": ["low", "medium", "high"]},
            **{key: {"type": "string"} for key in ("what_was_correct", "material_gaps", "verdict_rationale")},
            "mcp_policy": {
                "type": "object", "additionalProperties": False,
                "properties": {
                    "label": {"type": "string", "enum": ["pass", "fail"]},
                    "violations": {"type": "array", "items": {
                        "type": "object", "additionalProperties": False,
                        "properties": {"session_line": {"type": "integer"}, "reason": {"type": "string"}},
                        "required": ["session_line", "reason"],
                    }},
                    "rationale": {"type": "string"},
                },
                "required": ["label", "violations", "rationale"],
            },
        }
        body["text"] = {"format": {"type": "json_schema", "name": "pathways_verdict", "strict": True,
                                  "schema": {"type": "object", "additionalProperties": False,
                                             "properties": properties, "required": list(properties)}}}
    request_data = json.dumps(body).encode("utf-8")
    if pathways_evidence is not None:
        setting = "AI_INSIGHTS_PATHWAYS_JUDGE_MAX_REQUEST_BYTES"
        try:
            limit = int(os.environ.get(setting, DEFAULT_PATHWAYS_JUDGE_MAX_REQUEST_BYTES))
            if limit <= 0:
                raise ValueError
        except ValueError:
            raise AiInsightsConfigError(f"{setting} must be a positive integer byte count") from None
        if len(request_data) > limit:
            raise RuntimeError(
                f"Pathways judge request is {len(request_data):,} bytes; limit is {limit:,} bytes "
                f"({setting}). No request was sent and no evidence was truncated."
            )
    request = urllib.request.Request(
        f"{os.environ.get('OPENAI_API_BASE', DEFAULT_API_BASE).rstrip('/')}/responses",
        data=request_data,
        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
        method="POST",
    )
    last_exc: Exception | None = None
    for attempt in range(1, 4):
        try:
            LOGGER.info("Judge attempt %d for %s", attempt, test_id)
            with urllib.request.urlopen(request, timeout=120) as response:
                payload = json.loads(response.read().decode("utf-8"))
            if payload.get("status") in {"incomplete", "failed", "cancelled"}:
                raise RuntimeError(f"Judge response {payload['status']}: {payload.get('incomplete_details') or payload.get('error')}")
            fragments: list[str] = []
            for item in payload.get("output", []):
                if item.get("type") != "message":
                    continue
                for content in item.get("content", []):
                    if content.get("type") == "output_text":
                        fragments.append(str(content.get("text") or ""))
            parsed = extract_json_object("\n".join(fragments).strip())
            verdict = {
                "label": str(parsed.get("label", "fail")).lower(),
                "confidence": str(parsed.get("confidence", "medium")).lower(),
                "what_was_correct": str(parsed.get("what_was_correct", "")).strip(),
                "material_gaps": str(parsed.get("material_gaps", "")).strip(),
                "verdict_rationale": str(parsed.get("verdict_rationale", "")).strip(),
            }
            if pathways_evidence is not None:
                verdict["mcp_policy"] = parsed.get("mcp_policy")
            return verdict
        except urllib.error.HTTPError as exc:
            # Keep the rejection reason without archiving request headers or bodies.
            detail = exc.read(8192).decode("utf-8", errors="replace")
            try:
                error = json.loads(detail).get("error", {})
                detail = json.dumps({key: error[key] for key in ("type", "code", "message", "param")
                                     if key in error}) if isinstance(error, dict) else str(error)
            except (ValueError, AttributeError):
                pass
            for secret in (api_key, os.environ.get("OPENAI_PROXY_TOKEN")):
                if secret:
                    detail = detail.replace(secret, "[REDACTED]")
            last_exc = RuntimeError(f"Judge HTTP {exc.code}: {detail[:2048]}")
            if exc.code not in RETRYABLE_HTTP_STATUS:
                raise last_exc from None
        except (urllib.error.URLError, TimeoutError, socket.timeout, json.JSONDecodeError, ValueError) as exc:
            last_exc = exc
        if attempt < 3:
            time.sleep(attempt)
    raise RuntimeError(f"judge unavailable after 3 attempts: {last_exc}")


def mcp_policy_passes(policy: Any) -> bool:
    """Require an explicit policy pass with no reported violations."""
    return (
        isinstance(policy, dict)
        and policy.get("label") == "pass"
        and policy.get("violations") == []
    )


def score_attempt(attempt_dir: Path, test_case: dict[str, Any], cfg: dict[str, Any]) -> dict[str, Any]:
    """Combine structural response checks and judge output into a label."""
    response_text = (attempt_dir / "llm_response.md").read_text(encoding="utf-8").strip()
    observations = {"issues": [], "details": []}
    if not response_text:
        observations["issues"].append("empty_response")
        observations["details"].append("llm_response.md was empty")
    rubric_path = Path(__file__).resolve().parent / test_case["rubric"]
    rubric = rubric_path.read_text(encoding="utf-8")
    if is_pathways_execution(test_case):
        session = attempt_dir / "codex_session.jsonl"
        run_meta_path = attempt_dir / "run_meta.json"
        fixture_id = json.loads(run_meta_path.read_text(encoding="utf-8")).get("run_id") if run_meta_path.is_file() else None
        evidence, transcript_error = load_pathways_judge_evidence(session, fixture_id)
        if transcript_error:
            observations["issues"].append("incomplete_pathways_transcript")
            observations["details"].append(transcript_error)
        judge = judge_response(test_case["id"], rubric, response_text, cfg, pathways_evidence=evidence)
        policy = judge.get("mcp_policy")
        if not mcp_policy_passes(policy):
            observations["issues"].append("pathways_mcp_policy_failed")
            observations["details"].append(json.dumps(policy) if policy else "Judge did not return an MCP policy verdict.")
    else:
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
    write_score(attempt_dir, test_case, score)
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


def non_reasoning_output_token_usage(attempt_results: list[dict[str, Any]]) -> int:
    """Return output tokens with the reasoning-token subset removed."""
    output_tokens = sum_token_usage(attempt_results, "output_tokens")
    reasoning_tokens = sum_token_usage(attempt_results, "reasoning_output_tokens")
    if reasoning_tokens > output_tokens:
        raise ValueError(
            "reasoning output token usage exceeds total output token usage: "
            f"{reasoning_tokens} > {output_tokens}"
        )
    return output_tokens - reasoning_tokens


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
    if "aggregate_path" in aggregate:
        record_property("ai_aggregate_path", aggregate["aggregate_path"])

    run_ids = unique_values([result["run_meta"].get("run_id") for result in attempt_results])
    archive_shas = unique_values([result["run_meta"].get("archive_sha256") for result in attempt_results])
    judge_labels = unique_values([result["score"].get("judge", {}).get("label") for result in attempt_results])
    judge_confidences = unique_values([result["score"].get("judge", {}).get("confidence") for result in attempt_results])
    artifact_paths = {
        "ai_llm_response_path": "llm_response.md",
        "ai_score_path": "score.md",
        "ai_score_json_path": "score.json",
        "ai_invoke_metadata_path": "invoke_metadata.json",
        "ai_run_meta_path": "run_meta.json",
    }
    if any(result.get("pathways") for result in attempt_results):
        artifact_paths.update({
            "ai_session_path": "codex_session.jsonl",
            "ai_performix_runs_path": "performix_runs.json",
            "ai_codex_exec_path": "codex_exec.jsonl",
            "ai_codex_stderr_path": "codex_stderr.txt",
        })
    for result in attempt_results:
        if "timeout_seconds" in result["invoke"]:
            record_property("ai_agent_timeout_seconds", result["invoke"]["timeout_seconds"])
    if run_ids:
        record_property("ai_run_ids", run_ids)
    if archive_shas:
        record_property("ai_archive_sha256s", archive_shas)
    if judge_labels:
        record_property("ai_judge_labels", judge_labels)
    if judge_confidences:
        record_property("ai_judge_confidences", judge_confidences)
    for property_name, filename in artifact_paths.items():
        paths = unique_values([result["attempt_dir"] / filename for result in attempt_results
                               if not result.get("pathways") or (result["attempt_dir"] / filename).is_file()])
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
    mcp_tool_rounds = sum(
        result["invoke"].get("mcp", {}).get("tool_rounds", 0)
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
    record_property("ai_mcp_tool_rounds", mcp_tool_rounds)
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
    for key in ("input_tokens", "cached_input_tokens"):
        record_property(f"ai_{key}", sum_token_usage(attempt_results, key))
    record_property("ai_output_tokens", non_reasoning_output_token_usage(attempt_results))
    record_property(
        "ai_reasoning_output_tokens",
        sum_token_usage(attempt_results, "reasoning_output_tokens"),
    )


def record_evaluation_suite_properties(pytestconfig, record_testsuite_property, cfg: dict[str, Any]) -> None:
    """Record run-wide model configuration once instead of per attempt."""
    if getattr(pytestconfig, "_ai_insights_suite_properties_recorded", False):
        return

    record_testsuite_property("ai_model", cfg["model"])
    if cfg["reasoning_effort"]:
        record_testsuite_property("ai_reasoning_effort", cfg["reasoning_effort"])
    record_testsuite_property("ai_judge_model", cfg["judge_model"])
    record_testsuite_property("ai_judge_reasoning_effort", cfg["judge_reasoning_effort"])
    record_testsuite_property("ai_results_dir", str(cfg["results_dir"]))
    record_testsuite_property("ai_manifest_path", str(cfg["manifest_path"]))
    pytestconfig._ai_insights_suite_summary = {
        "model": cfg["model"],
        "reasoning_effort": cfg["reasoning_effort"],
        "judge_model": cfg["judge_model"],
        "judge_reasoning_effort": cfg["judge_reasoning_effort"],
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
            *(["  agent record: codex_session.jsonl",
               "  Performix runs: performix_runs.json", "  score: score.md",
               "  generated response: llm_response.md"]
              if (attempt_dir / "performix_runs.json").is_file()
              else ["  score: score.md", "  generated response: llm_response.md"]),
        ]
    )
    for name in ("codex_stderr.txt", "codex_exec.jsonl"):
        if (attempt_dir / name).is_file():
            lines.append(f"  diagnostics: {name}")
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
    timed_out = any("timeout_seconds" in result["invoke"] for result in attempt_results)
    execution_error = "error" in aggregate["scores"]
    if timed_out:
        heading = "AI Insights execution: TIMEOUT"
        explanation = "The agent exceeded its time limit. The judge was not run."
    elif execution_error:
        heading = "AI Insights execution: ERROR"
        explanation = "Evaluation could not complete; this is not a judge verdict."
    else:
        heading = "AI Insights judge verdict: FAIL"
        explanation = "The generated AI Insight did not satisfy the testcase rubric."
    lines = [
        heading,
        "",
        f"Test: {test_id} / {mode} / attempt {aggregate['attempt']}/{aggregate['attempts_total']}",
        explanation,
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
                f"  result: {'timeout' if 'timeout_seconds' in result['invoke'] else final_label}",
                ("  judge: not run" if "timeout_seconds" in result["invoke"]
                 else f"  judge: {judge_label} (confidence: {judge_confidence})"),
            ]
        )
        lines.extend(format_failure_field("Material gaps", judge.get("material_gaps")))
        lines.extend(format_failure_field("Verdict rationale", judge.get("verdict_rationale")))
        lines.extend(["", *format_failure_artifacts(attempt_dir, cfg)])
        if result.get("pathways"):
            lines.extend(format_failure_field("Attempt error", result["invoke"].get("error")))
        lines.extend(["", *format_response_excerpt(response_path)])

    return "\n".join(lines)


def run_attempt(test_case: dict[str, Any], mode: str, cfg: dict[str, Any], attempt_num: int) -> dict[str, Any]:
    if mode not in MCP_MODES:
        raise ValueError(f"unsupported AI Insights mode: {mode}")

    LOGGER.info("Starting %s/%s attempt %03d", test_case["id"], mode, attempt_num)
    pathways = is_pathways_execution(test_case)
    attempt_dir = cfg["results_dir"] / test_case["id"] / mode / "attempts" / f"{attempt_num:03d}"
    if attempt_dir.exists() and not pathways:
        shutil.rmtree(attempt_dir)
    attempt_dir.mkdir(parents=True)  # Pathways collisions must preserve old evidence.
    pathways_attempt = None
    if pathways:
        from pathways_attempt import create_pathways_attempt
        try:
            pathways_attempt = create_pathways_attempt(cfg, attempt_dir)
        except Exception as exc:
            pytest.exit(f"Pathways attempt setup failed: {exc}", returncode=2)
    performix = pathways_attempt.performix if pathways_attempt else PerformixAttempt(attempt_dir)
    workspace = None
    workload_checkout = None
    full_run_meta = {"test_id": test_case["id"], "mode": mode}
    invoke_meta = {"status": "not_started"}
    phase = "setup"
    try:
        if not pathways_attempt:
            performix.prepare()
        workspace, workload_checkout = prepare_agent_workspace(attempt_dir, test_case)
        full_run_meta["agent_workspace"] = str(workspace)
        if workload_checkout is not None:
            full_run_meta["workload_checkout"] = workload_checkout
        if pathways_attempt:
            connection, ssh_command = pathways_attempt.prepare_workspace(workspace, workload_checkout["commit"])
            cfg = {**cfg,
                   "target_prompt_values": {"target_host": connection["host"], "target_port": str(connection["port"]),
                                            "target_user": connection["user"], "target_private_key_path": connection["private_key_path"]},
                   "pathways_ssh_command": ssh_command}
        full_run_meta["performix_state"] = performix.state.copy()
        run_meta = import_attempt_run(test_case, cfg, performix)
        full_run_meta.update({
            **run_meta,
            "cli_bin": str(cfg["cli_bin"]),
            "recipe": test_case.get("recipe"),
            "target": test_case.get("target", cfg["defaults"].get("target")),
            "model_config_id": cfg["model_config_id"],
        })
        (attempt_dir / "run_meta.json").write_text(json.dumps(full_run_meta, indent=2), encoding="utf-8")
        phase = "invocation"
        invocation_started = time.monotonic()
        invoke_meta = invoke_mcp_mode(
            attempt_dir, full_run_meta, test_case, cfg, MCP_MODES[mode], workspace,
            performix=performix,
        )
        phase = "judge"
        if should_fail_on_tool_output_truncation(mode, invoke_meta):
            score = score_truncated_attempt(attempt_dir, test_case, invoke_meta)
        else:
            score = score_attempt(attempt_dir, test_case, cfg)
    except BaseException as exc:
        error = str(exc)
        if phase == "invocation":
            invoke_meta["duration_seconds"] = time.monotonic() - invocation_started
        if pathways and phase == "invocation" and isinstance(exc, subprocess.TimeoutExpired):
            limit = pathways_agent_timeout_seconds(test_case)
            invoke_meta["timeout_seconds"] = limit
            error = (f"Codex timed out: {limit}s agent time limit reached "
                     f"(elapsed {invoke_meta['duration_seconds']:.1f}s). Judge not run. "
                     "See codex_stderr.txt and codex_exec.jsonl for diagnostics.")
            LOGGER.error("%s/%s attempt %03d: %s", test_case["id"], mode, attempt_num, error)
        elif isinstance(exc, TargetAccessError):
            LOGGER.error("%s/%s attempt %03d failed during %s: %s",
                         test_case["id"], mode, attempt_num, phase, error)
        else:
            LOGGER.exception("%s/%s attempt %03d failed", test_case["id"], mode, attempt_num)
        invoke_meta = {**invoke_meta, "status": "error", "error": error, "failure_phase": phase}
        score = {
            "test_id": test_case["id"],
            "response_observations": {"issues": [f"{phase}_error"], "details": [error]},
            "judge": {},
            "final": {"final_label": "error"},
        }
        write_score(attempt_dir, test_case, score, failure_phase=phase)
        if not isinstance(exc, Exception):
            raise  # Preserve cancellation/SystemExit after the finally capture.
    finally:
        (attempt_dir / "run_meta.json").write_text(json.dumps(full_run_meta, indent=2), encoding="utf-8")
        (attempt_dir / "invoke_metadata.json").write_text(json.dumps(invoke_meta, indent=2), encoding="utf-8")
        try:
            if pathways_attempt:
                pathways_attempt.collect_evidence_and_clean_host()
            else:
                performix.clean()
        except Exception as exc:
            error = f"Attempt cleanup or evidence collection failed: {exc}"
            LOGGER.error("%s/%s attempt %03d: %s", test_case["id"], mode, attempt_num, error)
            invoke_meta["status"] = "error"
            invoke_meta.setdefault("failure_phase", "collection")
            invoke_meta["error"] = "\n".join(filter(None, [invoke_meta.get("error"), error]))
            score["cleanup_errors"] = [str(exc)]
            score["final"]["final_label"] = "error"
            write_score(attempt_dir, test_case, score, failure_phase=invoke_meta["failure_phase"])
            (attempt_dir / "invoke_metadata.json").write_text(json.dumps(invoke_meta, indent=2), encoding="utf-8")
    return {"attempt_dir": attempt_dir, "run_meta": full_run_meta, "invoke": invoke_meta, "score": score, "pathways": pathways}


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
    attempt_results = [run_attempt(test_case, mode, cfg, attempt)]
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
    artifact_dir = attempt_results[0]["attempt_dir"]
    aggregate["artifact_dir"] = str(artifact_dir)
    aggregate["artifact_rel_dir"] = str(artifact_dir.relative_to(cfg["results_dir"]))
    if not is_pathways_execution(test_case):
        aggregate_path = artifact_dir / "aggregate.json"
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
