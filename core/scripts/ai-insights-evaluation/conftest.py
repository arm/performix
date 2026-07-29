# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Pytest integration for the AI Insights evaluation harness.

Pytest loads a `conftest.py` file automatically for tests in the same
directory tree. This file is therefore the harness-local place to add command
line options, validate suite-wide configuration before any testcase starts,
and customise the terminal summary printed after pytest has run.

The AI Insights tests themselves live in `test_ai_insights_evaluation.py`.
That module owns the Performix run import, MCP invocation, LLM judging, and
recorded pytest properties. This file deliberately stays at the pytest
integration boundary: it defines how a user configures the suite and how those
recorded properties are presented in normal pytest output.

The most important pytest concepts used here are:

- `pytest_addoption`: called during pytest startup to register custom
  `--ai-*` options.
- `pytest_collection_modifyitems`: called after pytest has discovered and
  parametrised tests, but before it runs them. This lets us fail early for
  missing suite-level inputs such as API keys.
- `pytest_terminal_summary`: called after test execution to add a concise
  human-readable summary derived from pytest's standard recorded properties.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import stat
import subprocess
import sys
import zipfile
from datetime import datetime, timezone
from pathlib import Path

import pytest
import requests

from evaluation_summary import render_console_summary
from performance_quality import (
    QUALITY_GOOD,
    performance_metrics_for_attempt,
    performance_thresholds_from_manifest,
    recorded_performance_properties,
)


ROOT = Path(__file__).resolve().parents[3]
CORE_DIR = ROOT / "core"
SCRIPTS_DIR = CORE_DIR / "scripts"
HARNESS_DIR = Path(__file__).resolve().parent
AI_PROPERTY_PREFIX = "ai_"
HACKATHON_MODE = "hackathon_mcp"
DEFAULT_MODES = "rest,hackathon_mcp,performix_mcp"
DEFAULT_PRERECORDED_RUN_CACHE = (
    Path.home() / ".cache" / "performix" / "ai-insights-evaluation" / "pre-recorded-runs"
)
DEFAULT_ARTIFACTORY_RUN_BASE = "its.apx-prerecorded-runs/ai-insights-evaluation"
AI_XDIST_PREPARED_ATTR = "_ai_insights_xdist_prepared"
AI_INSIGHTS_TEST_NODEID_MARKER = "::test_ai_insights["

sys.path.append(str(SCRIPTS_DIR))
from run_export_helper import run_cli, sha256_file


def get_artifactory_url() -> str:
    constants_path = SCRIPTS_DIR / "constants.sh"
    with constants_path.open(encoding="utf-8") as constants:
        for line in constants:
            match = re.match(r'^\s*export\s+readonly\s+ARTIFACTORY_BASE_URL="([^"]+)"', line)
            if match:
                return match.group(1)
    sys.exit(f"Error: ARTIFACTORY_BASE_URL not found in {constants_path}")


# Options that are required for at least one supported mode are declared as
# small data records. Keeping the environment variable, CLI flag, and help text
# together makes the pytest help output and collection-time validation use the
# same source of truth.
OPENAI_API_KEY_OPT = {
    "option": "--ai-openai-api-key",
    "env_var": "OPENAI_API_KEY",
    "help": "OpenAI API key used by REST mode, the Codex coding agent, and the judge.",
}
PRERECORDED_RUN_CACHE_OPT = {
    "option": "--ai-prerecorded-run-cache",
    "aliases": ("--ai-run-artifact-base",),
    "env_var": "AI_INSIGHTS_PRERECORDED_RUN_CACHE",
    "fallback_env_var": "AI_INSIGHTS_RUN_ARTIFACT_BASE",
    "default": str(DEFAULT_PRERECORDED_RUN_CACHE),
    "help": "Directory used as the local cache for pre-recorded AI Insights run inputs.",
}
ARTIFACTORY_RUN_BASE_OPT = {
    "option": "--ai-artifactory-run-base",
    "env_var": "AI_INSIGHTS_ARTIFACTORY_RUN_BASE",
    "default": DEFAULT_ARTIFACTORY_RUN_BASE,
    "help": "Artifactory path containing pre-recorded AI Insights run archives.",
}
HACKATHON_MCP_SERVER_DIR_OPT = {
    "option": "--ai-hackathon-mcp-server-dir",
    "env_var": "AI_INSIGHTS_HACKATHON_MCP_SERVER_DIR",
    "help": "Hackathon MCP server checkout directory (https://github.com/Arm-Debug/atp-mcp-server-hackathon).",
    "mode": HACKATHON_MODE,
}


def pytest_addoption(parser) -> None:
    """Register AI Insights command line options with pytest.

    Pytest calls this hook before collection. Each option can be supplied on
    the command line, or via the matching environment variable. The test suite
    resolves the final configuration from these options plus manifest defaults.
    """
    group = parser.getgroup("ai-insights")
    group.addoption(
        "--ai-manifest",
        default=os.environ.get("AI_INSIGHTS_MANIFEST", str(HARNESS_DIR / "ai_insights_evaluation.json")),
    )
    group.addoption("--ai-act", default=os.environ.get("AI_INSIGHTS_ACT", "act1"))
    group.addoption(
        "--ai-modes",
        default=os.environ.get("AI_INSIGHTS_MODES", DEFAULT_MODES),
        help=(
            "Comma-separated AI Insights modes to run. Overrides manifest modes. "
            "Supported modes are rest, hackathon_mcp, and performix_mcp. "
            f"Defaults to {DEFAULT_MODES}."
        ),
    )
    _add_env_option(group, PRERECORDED_RUN_CACHE_OPT)
    _add_env_option(group, ARTIFACTORY_RUN_BASE_OPT)
    group.addoption(
        "--ai-cli-bin",
        default=os.environ.get("AI_INSIGHTS_CLI_BIN", str(ROOT / "core" / "apap-cli" / "apx")),
    )
    group.addoption(
        "--ai-results-dir",
        default=os.environ.get("AI_INSIGHTS_RESULTS_DIR", str(HARNESS_DIR / "results")),
    )
    _add_env_option(group, HACKATHON_MCP_SERVER_DIR_OPT)
    group.addoption("--ai-model", default=os.environ.get("AI_INSIGHTS_MODEL"))
    group.addoption("--ai-reasoning-effort", default=os.environ.get("AI_INSIGHTS_REASONING_EFFORT"))
    group.addoption("--ai-judge-model", default=os.environ.get("AI_INSIGHTS_JUDGE_MODEL", "gpt-5-mini"))
    _add_env_option(group, OPENAI_API_KEY_OPT)
    group.addoption("--ai-attempts", type=int, default=int(os.environ.get("AI_INSIGHTS_ATTEMPTS", "0")))


@pytest.hookimpl(optionalhook=True)
def pytest_xdist_node_collection_finished(node, ids) -> None:
    """Prepare selected inputs after an xdist worker has collected tests.

    xdist workers collect the same filtered node IDs before any test is
    scheduled. The first collection is enough to know which run inputs are
    needed, and the prepared flag prevents later worker callbacks doing the
    same shared filesystem work again.
    """

    config = node.config
    if config.option.collectonly:
        return

    if getattr(config, AI_XDIST_PREPARED_ATTR, False):
        return

    setattr(config, AI_XDIST_PREPARED_ATTR, True)
    _prepare_selected_testcases(config, _selected_testcases_from_nodeids(config, ids))


def _add_env_option(group, option_spec: dict[str, str]) -> None:
    env_value = os.environ.get(option_spec["env_var"])
    fallback_env_var = option_spec.get("fallback_env_var")
    if env_value is None and fallback_env_var:
        env_value = os.environ.get(fallback_env_var)
    default = env_value if env_value is not None else option_spec.get("default")
    help_parts = [option_spec["help"]]
    if fallback_env_var:
        help_parts.append(
            f"Can be set with {option_spec['env_var']} or {fallback_env_var}."
        )
    else:
        help_parts.append(f"Can be set with {option_spec['env_var']}.")
    if option_spec.get("default"):
        help_parts.append(f"Defaults to {option_spec['default']}.")
    group.addoption(
        option_spec["option"],
        *option_spec.get("aliases", ()),
        default=default,
        help=" ".join(help_parts),
    )


@pytest.hookimpl(wrapper=True, trylast=True)
def pytest_collection_modifyitems(config, items):
    """Validate suite-level runtime inputs after test parametrisation.

    The evaluation tests are parametrised by testcase, mode, and attempt. At
    this point pytest has already expanded the manifest into concrete test
    items, so we can validate only the inputs required by the selected items.
    For example, the Hackathon MCP checkout is required only if a collected
    test item uses `hackathon_mcp`.

    This hook runs after pytest has applied normal filtering, such as `-k`. In
    serial pytest, downloads can run here. In xdist workers, this hook only
    validates options; the controller downloads inputs when the first worker
    reports its filtered test list.
    """
    yield

    if config.option.collectonly:
        return

    missing = []
    for option_spec in (
        OPENAI_API_KEY_OPT,
        PRERECORDED_RUN_CACHE_OPT,
        HACKATHON_MCP_SERVER_DIR_OPT,
    ):
        if option_spec.get("mode") and not _has_mode(items, option_spec["mode"]):
            continue
        if not (config.getoption(option_spec["option"]) or "").strip():
            missing.append(option_spec)

    prerecorded_run_cache = config.getoption("--ai-prerecorded-run-cache")
    if prerecorded_run_cache:
        testcases = _selected_testcases(items)
        if not missing and not os.environ.get("PYTEST_XDIST_WORKER"):
            # Serial pytest downloads here. xdist workers skip this because the
            # controller downloads after seeing the filtered worker collection.
            _prepare_selected_testcases(config, testcases)

    if missing:
        raise pytest.UsageError(_format_missing_config(missing, []))


def _has_mode(items, mode: str) -> bool:
    for item in items:
        callspec = getattr(item, "callspec", None)
        if callspec and callspec.params.get("mode") == mode:
            return True
    return False


def _selected_testcases(items) -> list[dict[str, str]]:
    testcases = []
    seen = set()
    for item in items:
        callspec = getattr(item, "callspec", None)
        test_case = callspec.params.get("test_case") if callspec else None
        if not isinstance(test_case, dict):
            continue
        test_id = test_case.get("id")
        run_artifact = test_case.get("run_artifact")
        if not test_id or not run_artifact or test_id in seen:
            continue
        testcases.append(test_case)
        seen.add(test_id)
    return testcases


def _selected_testcases_from_nodeids(config, nodeids) -> list[dict[str, str]]:
    """Map filtered pytest node IDs back to manifest testcases."""

    manifest_path = Path(config.getoption("--ai-manifest")).expanduser().resolve()
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    manifest_by_id = {}
    for test_case in manifest.get("tests", []):
        if not isinstance(test_case, dict):
            continue
        test_id = test_case.get("id")
        if test_id and test_case.get("run_artifact"):
            manifest_by_id[test_id] = test_case

    # xdist gives the controller node IDs, not pytest Items. Match the
    # parameter id back to the longest manifest id so ids such as
    # "test_case_2" do not steal "test_case_26-...".
    testcases = []
    seen = set()
    manifest_ids = sorted(manifest_by_id, key=len, reverse=True)
    for nodeid in nodeids:
        marker_index = nodeid.find(AI_INSIGHTS_TEST_NODEID_MARKER)
        if marker_index < 0:
            continue
        param_start = marker_index + len(AI_INSIGHTS_TEST_NODEID_MARKER)
        param_end = nodeid.rfind("]")
        if param_end < param_start:
            raise pytest.UsageError(f"AI Insights pytest node ID is missing its parameter id: {nodeid}")
        param_id = nodeid[param_start:param_end]
        test_id = next(
            (
                test_id
                for test_id in manifest_ids
                if param_id == test_id or param_id.startswith(f"{test_id}-")
            ),
            None,
        )
        if test_id is None:
            raise pytest.UsageError(
                f"Could not map AI Insights pytest node ID to a manifest testcase: {nodeid}"
            )
        if test_id in seen:
            continue
        testcases.append(manifest_by_id[test_id])
        seen.add(test_id)
    return testcases


def _terminal_write_line(config):
    terminalreporter = config.pluginmanager.get_plugin("terminalreporter")
    if terminalreporter is None:
        return None
    return terminalreporter.write_line


def _prepare_selected_testcases(
    config,
    testcases: list[dict[str, str]],
    write_line=None,
) -> None:
    """Download, extract, and import selected run inputs once.

    These steps write to shared cache paths and APX run storage. Running them in
    one process avoids races when xdist runs modes for the same testcase on
    different workers.
    """

    if write_line is None:
        write_line = _terminal_write_line(config)
    run_cache = Path(config.getoption("--ai-prerecorded-run-cache")).expanduser().resolve()
    results_dir = Path(config.getoption("--ai-results-dir")).expanduser().resolve()
    artifactory_run_base = (config.getoption("--ai-artifactory-run-base") or "").strip()
    _prepare_run_inputs(
        testcases,
        run_cache,
        artifactory_run_base,
        results_dir,
        write_line,
    )

    cli_bin = Path(config.getoption("--ai-cli-bin")).expanduser().resolve()
    _prepare_imported_runs(
        testcases,
        config.cache,
        cli_bin,
        run_cache,
        results_dir,
        write_line,
    )


def _prepare_run_inputs(
    testcases: list[dict[str, str]],
    prerecorded_run_cache: Path,
    artifactory_run_base: str,
    results_dir: Path,
    write_line=None,
) -> None:
    """Download, check, and extract selected run inputs before tests start."""
    if artifactory_run_base:
        _download_missing_run_artifacts(
            testcases,
            prerecorded_run_cache,
            artifactory_run_base,
            write_line,
        )

    missing_artifacts = _missing_run_artifacts_for_testcases(testcases, prerecorded_run_cache)
    if missing_artifacts:
        raise pytest.UsageError(_format_missing_config([], missing_artifacts))

    _extract_source_archives(testcases, prerecorded_run_cache, results_dir, write_line)


def _download_missing_run_artifacts(
    testcases: list[dict[str, str]],
    prerecorded_run_cache: Path,
    artifactory_run_base: str,
    write_line=None,
) -> None:
    """Fetch missing pre-recorded inputs for the selected testcases.

    Pytest has already selected the tests by this point. If a download fails
    here, the selected test cannot run, so report it as a setup error.
    """

    wrote_header = False
    for test_case in testcases:
        test_id = test_case["id"]
        archive = prerecorded_run_cache / test_case["run_artifact"]
        source_archive = archive.parent / "test_src.zip"
        missing_files = []
        if not archive.is_file():
            missing_files.append("latest.zip")
        if not source_archive.is_file():
            missing_files.append("test_src.zip")
        metadata = archive.parent / "metadata.json"
        if not metadata.is_file():
            missing_files.append("metadata.json")

        if missing_files and write_line:
            if not wrote_header:
                write_line(f"AI Insights evaluation: downloading missing inputs to {prerecorded_run_cache}")
                wrote_header = True
            write_line(f"AI Insights evaluation: {test_id}: {', '.join(missing_files)}")

        if not archive.is_file():
            _download_artifactory_input(
                artifactory_run_base,
                test_id,
                "latest.zip",
                archive.parent,
                "latest.zip",
            )
        if not source_archive.is_file():
            _download_artifactory_input(
                artifactory_run_base,
                test_id,
                "test_src.zip",
                archive.parent,
                "test_src.zip",
            )
        if not metadata.is_file():
            if not _download_artifactory_input(
                artifactory_run_base,
                test_id,
                "latest.metadata.json",
                archive.parent,
                "metadata.json",
                required=False,
            ):
                _download_artifactory_input(
                    artifactory_run_base,
                    test_id,
                    "metadata.json",
                    archive.parent,
                    "metadata.json",
                )


def _extract_source_archives(
    testcases: list[dict[str, str]],
    prerecorded_run_cache: Path,
    results_dir: Path,
    write_line=None,
) -> None:
    wrote_header = False
    for test_case in testcases:
        test_id = test_case["id"]
        archive = prerecorded_run_cache / test_case["run_artifact"]
        source_archive = archive.parent / "test_src.zip"
        source_sha = sha256_file(source_archive)
        extract_dir = results_dir / "imported_sources" / test_id / source_sha
        source_root = extract_dir / "test_src"
        if source_root.is_dir():
            continue

        if write_line:
            if not wrote_header:
                write_line(f"AI Insights evaluation: extracting source inputs to {results_dir}")
                wrote_header = True
            write_line(f"AI Insights evaluation: {test_id}: test_src.zip")

        if extract_dir.exists():
            shutil.rmtree(extract_dir)
        try:
            _safe_extract_zip(source_archive, extract_dir)
        except Exception as exc:
            shutil.rmtree(extract_dir, ignore_errors=True)
            raise pytest.UsageError(
                f"Failed to extract AI Insights source input for {test_id}: {source_archive}"
            ) from exc
        if not source_root.is_dir():
            raise pytest.UsageError(
                f"AI Insights source input did not contain test_src for {test_id}: {source_archive}"
            )


def _safe_extract_zip(archive: Path, destination: Path) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    destination_root = destination.resolve()
    with zipfile.ZipFile(archive) as archive_zip:
        for member in archive_zip.infolist():
            if stat.S_ISLNK(member.external_attr >> 16):
                raise ValueError(f"refusing to extract archive link: {member.filename}")
            target = (destination / member.filename).resolve()
            if not target.is_relative_to(destination_root):
                raise ValueError(f"refusing to extract unsafe archive member: {member.filename}")
        archive_zip.extractall(destination)


def _prepare_imported_runs(
    testcases: list[dict[str, str]],
    cache,
    cli_bin: Path,
    prerecorded_run_cache: Path,
    results_dir: Path,
    write_line=None,
) -> None:
    """Import each selected run before tests start.

    `apx run import` writes to shared APX run storage. If several workers
    import the same archive at once, they can write the same run directory at
    the same time. Import here once and cache the run id for the tests.
    """
    wrote_header = False
    for test_case in testcases:
        test_id = test_case["id"]
        archive = prerecorded_run_cache / test_case["run_artifact"]
        source_archive = archive.parent / "test_src.zip"
        archive_sha = sha256_file(archive)
        source_sha = sha256_file(source_archive)
        source_root = results_dir / "imported_sources" / test_id / source_sha / "test_src"
        cache_key = f"ai_insights/imports/{test_id}/{archive_sha}"
        cached = cache.get(cache_key, None)

        run_id = ""
        if isinstance(cached, dict):
            cached_run_id = cached.get("run_id")
            if isinstance(cached_run_id, str) and _run_info_succeeds(cli_bin, cached_run_id):
                run_id = cached_run_id

        if not run_id:
            if write_line:
                if not wrote_header:
                    write_line("AI Insights evaluation: importing runs before test workers")
                    wrote_header = True
                write_line(f"AI Insights evaluation: {test_id}: latest.zip")
            process = run_cli(
                [str(cli_bin), "run", "import", str(archive), "--json"],
                cli_bin.parent,
            )
            try:
                payload = json.loads(process.stdout)
            except json.JSONDecodeError as exc:
                raise ValueError("apx run import --json output was not valid JSON") from exc
            if isinstance(payload, dict):
                value = payload.get("data", {}).get("new_id", {}).get("value")
                if isinstance(value, str) and value:
                    run_id = value
            if not run_id:
                raise ValueError("apx run import --json output did not contain data.new_id.value")

        run_cli([str(cli_bin), "run", "update", run_id, "--source", str(source_root)], cli_bin.parent)
        cache.set(
            cache_key,
            {
                "run_id": run_id,
                "archive_sha256": archive_sha,
                "archive_path": str(archive),
                "source_archive_sha256": source_sha,
                "source_archive_path": str(source_archive),
                "source_root": str(source_root),
                "timestamp_utc": datetime.now(timezone.utc).isoformat(),
            },
        )


def _run_info_succeeds(cli_bin: Path, run_id: str) -> bool:
    if not run_id:
        return False
    process = subprocess.run(
        [str(cli_bin), "run", "info", run_id, "--json"],
        cwd=cli_bin.parent,
        capture_output=True,
        text=True,
    )
    return process.returncode == 0


def _artifactory_base_url() -> str:
    return get_artifactory_url()


def _artifactory_api_token() -> str:
    token = os.environ.get("ARTIFACTORY_API_TOKEN", "").strip()
    if not token:
        raise pytest.UsageError(
            "ARTIFACTORY_API_TOKEN is required to download AI Insights inputs "
            "from Artifactory. Set ARTIFACTORY_API_TOKEN to an Artifactory "
            "identity token, or provide local inputs with --ai-prerecorded-run-cache."
        )
    return token


def _download_artifactory_input(
    artifactory_run_base: str,
    test_id: str,
    filename: str,
    destination_dir: Path,
    destination_name: str,
    required: bool = True,
) -> bool:
    destination_dir.mkdir(parents=True, exist_ok=True)
    source = f"{artifactory_run_base.rstrip('/')}/{test_id}/{filename}"
    url = f"{_artifactory_base_url().rstrip('/')}/{source}"
    destination = destination_dir / destination_name
    partial = destination_dir / f".{destination_name}.tmp"
    headers = {"X-JFrog-Art-Api": _artifactory_api_token()}

    try:
        response = requests.get(url, headers=headers, timeout=120)
        if response.status_code == 404 and not required:
            partial.unlink(missing_ok=True)
            return False
        response.raise_for_status()
        # Pre-recorded run inputs are expected to stay in the single-digit MB
        # range, so writing the response body directly keeps this path simple.
        partial.write_bytes(response.content)
        partial.replace(destination)
    except requests.HTTPError as exc:
        partial.unlink(missing_ok=True)
        response = exc.response
        body = response.text.strip() if response is not None else ""
        status = f"{response.status_code} {response.reason}" if response is not None else "unknown"
        raise pytest.UsageError(
            "\n".join(
                [
                    "",
                    f"Failed to download AI Insights input from Artifactory: {source}",
                    f"URL: {url}",
                    f"HTTP status: {status}",
                    f"Response: {body[:1000]}",
                ]
            )
        ) from exc
    except requests.RequestException as exc:
        partial.unlink(missing_ok=True)
        raise pytest.UsageError(
            "\n".join(
                [
                    "",
                    f"Failed to download AI Insights input from Artifactory: {source}",
                    f"URL: {url}",
                    f"Error: {exc}",
                ]
            )
        ) from exc
    return True


def _missing_run_artifacts_for_testcases(
    testcases: list[dict[str, str]],
    prerecorded_run_cache: Path,
) -> list[tuple[str, str, Path]]:
    """Find missing pre-recorded run inputs for manifest testcases."""
    missing = []
    for test_case in testcases:
        test_id = test_case.get("id")
        run_artifact = test_case.get("run_artifact")
        if not test_id or not run_artifact:
            continue
        archive = prerecorded_run_cache / run_artifact
        if not archive.is_file():
            missing.append((test_id, "run archive", archive))
        source_archive = archive.parent / "test_src.zip"
        if not source_archive.is_file():
            missing.append((test_id, "source archive", source_archive))
    return missing


def _format_missing_config(
    missing: list[dict[str, str]],
    missing_artifacts: list[tuple[str, str, Path]],
) -> str:
    plural = "s" if len(missing) > 1 else ""
    lines = [
        "",
        "AI Insights evaluation is not configured.",
        "",
    ]
    if missing:
        lines.append(f"Missing required setting{plural}:")
        for option_spec in missing:
            lines.extend(
                [
                    f"  * {_option_label(option_spec)}",
                    f"    {option_spec['help']}",
                ]
            )
    if missing_artifacts:
        if missing:
            lines.append("")
        artifact_plural = "s" if len(missing_artifacts) > 1 else ""
        lines.append(f"Missing pre-recorded run input{artifact_plural}:")
        for test_id, label, path in missing_artifacts:
            lines.append(f"  * {test_id} {label}: {path}")
        lines.extend(
            [
                "    Set --ai-prerecorded-run-cache or AI_INSIGHTS_PRERECORDED_RUN_CACHE",
                "    to use a different pre-recorded run input directory. To fetch",
                "    missing inputs from Artifactory, set ARTIFACTORY_API_TOKEN.",
            ]
        )
    return "\n".join(lines)


def _option_label(option_spec: dict[str, str]) -> str:
    env_vars = [option_spec["env_var"]]
    if option_spec.get("fallback_env_var"):
        env_vars.append(option_spec["fallback_env_var"])
    return f"{option_spec['option']} or {'/'.join(env_vars)}"


def pytest_terminal_summary(terminalreporter, exitstatus, config) -> None:
    """Print the AI Insights summary after normal pytest reporting.

    The testcase records metadata through `record_property`, which pytest
    stores on each test report. This hook gathers the properties
    from completed call reports and renders a compact human-readible summary.
    The same properties remain available to pytest reporters such as JUnit XML.
    """
    reports = _ai_call_reports(terminalreporter)
    if not reports:
        return

    terminalreporter.section("AI Insights evaluation")
    summary = getattr(config, "_ai_insights_suite_summary", None)
    if summary:
        terminalreporter.write_line(_format_suite_summary(summary))
    terminalreporter.write_line("")
    terminalwriter = getattr(terminalreporter, "_tw", None)
    width = getattr(terminalwriter, "fullwidth", None)
    color = bool(getattr(terminalwriter, "hasmarkup", False))
    rendered_summary = render_console_summary(
        _ai_attempts_from_reports(reports),
        width=width,
        color=color,
    )
    for line in rendered_summary.splitlines():
        terminalreporter.write_line(line)


@pytest.hookimpl(hookwrapper=True)
def pytest_runtest_makereport(item, call):
    """Record performance checks and fail an otherwise-passing testcase when needed."""
    outcome = yield
    report = outcome.get_result()
    if report.when != "call":
        return

    attempt = _ai_properties(report)
    if not attempt:
        return

    metrics = performance_metrics_for_attempt(
        attempt,
        performance_thresholds=_performance_thresholds(item.config),
    )
    report.user_properties.extend(recorded_performance_properties(metrics))
    failures = [metric for metric in metrics if metric.quality != QUALITY_GOOD]
    if not report.passed or not failures:
        return

    report.outcome = "failed"
    report.longrepr = "AI Insights performance check failed: " + ", ".join(
        f"{metric.label}={metric.value!r} ({metric.quality}, threshold={metric.threshold})"
        for metric in failures
    )


def _ai_call_reports(terminalreporter) -> list:
    """Collect completed test-call reports that contain AI Insights metadata.

    Pytest creates separate reports for setup, call, and teardown phases. The
    evaluation metrics are recorded during the call phase, so this summary
    intentionally ignores setup/teardown reports.
    """
    reports = []
    seen = set()

    for outcome_reports in terminalreporter.stats.values():
        for report in outcome_reports:
            if getattr(report, "when", None) != "call":
                continue
            if report.nodeid in seen:
                continue
            if not _ai_properties(report):
                continue

            seen.add(report.nodeid)
            reports.append(report)

    reports.sort(key=lambda report: report.nodeid)
    return reports


def _ai_properties(report) -> dict[str, str]:
    return {
        name: str(value)
        for name, value in getattr(report, "user_properties", [])
        if name.startswith(AI_PROPERTY_PREFIX)
    }


def _ai_attempts_from_reports(reports: list) -> list[dict[str, str]]:
    attempts = []
    for report in reports:
        properties = _ai_properties(report)
        if not properties:
            continue
        attempts.append(
            {
                **properties,
                "pytest_outcome": str(getattr(report, "outcome", "unknown")),
            }
        )
    return attempts


def _performance_thresholds(config) -> dict[str, dict[str, int | float]]:
    # The report hook runs for every attempt, so parse the manifest only once
    # and keep its thresholds on the pytest configuration for the rest of the run.
    cached_thresholds = getattr(config, "_ai_insights_performance_thresholds", None)
    if cached_thresholds is not None:
        return cached_thresholds
    manifest_path = Path(config.getoption("--ai-manifest")).expanduser().resolve()
    thresholds = performance_thresholds_from_manifest(manifest_path)
    config._ai_insights_performance_thresholds = thresholds
    return thresholds


def _format_suite_summary(summary: dict[str, str]) -> str:
    fields = [
        _format_key_value("model", summary.get("model")),
        _format_key_value("reasoning", summary.get("reasoning_effort")),
        _format_key_value("judge_model", summary.get("judge_model")),
        _format_key_value("results", summary.get("results_dir")),
    ]
    return ", ".join(field for field in fields if field)


def _format_key_value(key: str, value: str | None) -> str:
    if not value:
        return ""
    return f"{key}={value}"
