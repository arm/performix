# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import sys
from pathlib import Path

import importlib.util
import io
import json
import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from contextlib import contextmanager
from types import SimpleNamespace
from unittest import mock

import pytest
import tomli

MODULE_DIR = Path(__file__).resolve().parent
HARNESS_DIR = MODULE_DIR / "ai-insights-evaluation"
sys.path.append(str(HARNESS_DIR))
from performix_attempt import prepare_performix_state
import performance_quality  # noqa: E402

EVALUATION_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_evaluation_test_module",
    HARNESS_DIR / "test_ai_insights_evaluation.py",
)
evaluation = importlib.util.module_from_spec(EVALUATION_SPEC)
sys.modules[EVALUATION_SPEC.name] = evaluation
EVALUATION_SPEC.loader.exec_module(evaluation)

CONFTEST_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_evaluation_conftest_test_module",
    HARNESS_DIR / "conftest.py",
)
conftest = importlib.util.module_from_spec(CONFTEST_SPEC)
sys.modules[CONFTEST_SPEC.name] = conftest
CONFTEST_SPEC.loader.exec_module(conftest)


class FakePytestConfig:
    def __init__(self, act: str, modes: str = "", recipes: str = ""):
        self.act = act
        self.modes = modes
        self.recipes = recipes

    def getoption(self, option: str):
        if option == "--ai-act":
            return self.act
        if option == "--ai-modes":
            return self.modes
        if option == "--ai-recipe":
            return self.recipes
        raise AssertionError(f"unexpected option: {option}")


class FakeXdistConfig:
    def __init__(self, manifest_path: Path):
        self.option = SimpleNamespace(collectonly=False)
        self.manifest_path = manifest_path

    def getoption(self, option: str):
        if option == "--ai-manifest":
            return str(self.manifest_path)
        raise AssertionError(f"unexpected option: {option}")


class FakeXdistNode:
    def __init__(self, config: FakeXdistConfig):
        self.config = config


def selected_testcases_from_nodeids(manifest_tests: list[dict], nodeids: list[str]):
    with tempfile.TemporaryDirectory() as tmpdir:
        manifest_path = Path(tmpdir) / "manifest.json"
        manifest_path.write_text(
            json.dumps(
                {
                    "defaults": {"prerecord": {"mode": "launch"}},
                    "tests": manifest_tests,
                }
            ),
            encoding="utf-8",
        )
        return conftest._selected_testcases_from_nodeids(
            FakeXdistConfig(manifest_path),
            nodeids,
        )


class AiInsightsManifestSelectionTests(unittest.TestCase):
    FIXTURE_FREE_CASE = {
        "id": "live", "acts": ["act4"], "mcp_prompt": "Investigate the workload on {target_host}.",
        "pathways_execution": {"mode": "host_ssh", "workload_source": {
            "repository": "https://example.test/workloads.git", "branch": "main"}},
    }

    def test_pathways_without_fixture_needs_no_recipe_or_prerecord_defaults(self):
        case = self.FIXTURE_FREE_CASE
        params = evaluation.iter_manifest_parameters(
            FakePytestConfig("act4", "performix_mcp"), {"tests": [case]})
        self.assertEqual([{"test_case": case, "mode": "performix_mcp"}], params)
        prompt = evaluation.resolve_mcp_prompt(case, None, {"target_host": "target"})
        self.assertIn("Investigate the workload on target.", prompt)
        self.assertIn("Initial Performix run: not supplied.", prompt)
        self.assertEqual({"run_id": None}, evaluation.import_attempt_run(case, {}, None))

    def test_recipe_filter_excludes_fixture_free_pathways(self):
        case = self.FIXTURE_FREE_CASE
        recorded = {**case, "id": "recorded", "run_artifact": "fixture/latest.zip",
                    "recipe": "code_hotspots", "prerecord": {"mode": "launch"}}
        for cases in ([case, recorded], [recorded]):
            with self.subTest(cases=[entry["id"] for entry in cases]):
                selected = evaluation.iter_manifest_parameters(
                    FakePytestConfig("act4", "performix_mcp", "code_hotspots"), {"tests": cases})
                self.assertEqual([{"test_case": recorded, "mode": "performix_mcp"}], selected)
        with self.assertRaisesRegex(pytest.UsageError, "Unknown AI Insights recipe selection"):
            evaluation.iter_manifest_parameters(
                FakePytestConfig("act4", "performix_mcp", "code_hotspots"), {"tests": [case]})

    def test_fixture_free_pathways_rejects_invalid_prompt_or_artifact(self):
        case = self.FIXTURE_FREE_CASE
        for changes in ({"mcp_prompt": "Inspect {run_id}."}, {"mcp_prompt": ""}, {"run_artifact": ""}):
            with self.subTest(changes=changes), self.assertRaises(pytest.UsageError):
                evaluation.iter_manifest_parameters(
                    FakePytestConfig("act4", "performix_mcp"), {"tests": [{**case, **changes}]})
        with self.assertRaisesRegex(ValueError, "without a supplied run"):
            evaluation.resolve_mcp_prompt({**case, "mcp_prompt": "Inspect {run_id}."}, None)

    def test_fixture_free_selection_does_not_prepare_prerecorded_inputs(self):
        case = {"id": "live", "pathways_execution": {"mode": "host_ssh"}}
        item = SimpleNamespace(callspec=SimpleNamespace(params={"test_case": case}))
        self.assertEqual([case], conftest._selected_testcases([item]))
        config = mock.Mock()
        with mock.patch.object(conftest, "_prepare_run_inputs") as prepare:
            conftest._prepare_selected_testcases(config, [case])
        config.getoption.assert_not_called()
        prepare.assert_not_called()

    def test_mixed_fixture_selection_prepares_only_supplied_runs(self):
        with tempfile.TemporaryDirectory() as tmp:
            live = {"id": "live", "pathways_execution": {"mode": "host_ssh"}}
            seeded = {**live, "id": "seeded", "run_artifact": "fixture/latest.zip"}
            ordinary = {"id": "ordinary", "run_artifact": "ordinary/latest.zip"}
            config = mock.Mock()
            config.getoption.return_value = tmp
            with mock.patch.object(conftest, "_prepare_run_inputs") as prepare:
                conftest._prepare_selected_testcases(config, [live, seeded, ordinary], write_line=mock.Mock())
            self.assertEqual([seeded, ordinary], prepare.call_args.args[0])

    def test_iter_manifest_parameters_returns_selected_valid_act(self):
        manifest = {
            "defaults": {
                "modes": ["performix_mcp"],
                "recipe": "code_hotspots",
                "prerecord": {"mode": "launch"},
            },
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act1"],
                },
                {
                    "id": "test_case_02",
                    "acts": ["act2"],
                },
            ],
        }

        params = evaluation.iter_manifest_parameters(FakePytestConfig("act2"), manifest)

        self.assertEqual(1, len(params))
        self.assertEqual("test_case_02", params[0]["test_case"]["id"])
        self.assertEqual("performix_mcp", params[0]["mode"])

    def test_insights_acts_exclude_pathways(self):
        manifest = evaluation.load_json(HARNESS_DIR / "ai_insights_evaluation.json")
        for acts in ("act1", "act2", "act3", "act1,act2,act3"):
            with self.subTest(acts=acts):
                params = evaluation.iter_manifest_parameters(FakePytestConfig(acts), manifest)
                self.assertTrue(params)
                self.assertFalse(any(p["test_case"].get("pathways_execution") for p in params))

    def test_act4_selects_and_resolves_the_pathways_scenario(self):
        manifest = evaluation.load_json(HARNESS_DIR / "ai_insights_evaluation.json")
        params = evaluation.iter_manifest_parameters(FakePytestConfig("act4", "performix_mcp"), manifest)
        self.assertEqual([("pathways_01", "performix_mcp")],
                         [(p["test_case"]["id"], p["mode"]) for p in params])
        target = {"target_host": "target.example.test", "target_port": "2222",
                  "target_user": "tester", "target_private_key_path": "/tmp/test-key"}
        prompt = evaluation.resolve_mcp_prompt(params[0]["test_case"], "baseline-run-id", target)
        for value in ("Initial Performix run: baseline-run-id.", *target.values()):
            self.assertIn(value, prompt)

    def test_pathways_execution_config_rejects_missing_or_unsupported_fields(self):
        manifest = evaluation.load_json(HARNESS_DIR / "ai_insights_evaluation.json")
        test_case = next(test_case for test_case in manifest["tests"] if test_case["id"] == "pathways_01")
        test_case["pathways_execution"]["workload_source"].pop("repository")

        with self.assertRaisesRegex(pytest.UsageError, "repository"):
            evaluation.iter_manifest_parameters(FakePytestConfig("act4", "performix_mcp"), manifest)

        for field in ("target", "measurement"):
            with self.subTest(field=field):
                manifest = evaluation.load_json(HARNESS_DIR / "ai_insights_evaluation.json")
                test_case = next(test_case for test_case in manifest["tests"] if test_case["id"] == "pathways_01")
                test_case["pathways_execution"][field] = {}

                with self.assertRaisesRegex(pytest.UsageError, field):
                    evaluation.iter_manifest_parameters(FakePytestConfig("act4", "performix_mcp"), manifest)

    def test_pathways_agent_timeout_defaults_and_overrides(self):
        manifest = evaluation.load_json(HARNESS_DIR / "ai_insights_evaluation.json")
        test_case = next(test_case for test_case in manifest["tests"] if test_case["id"] == "pathways_01")
        config = test_case["pathways_execution"]
        config.pop("agent_timeout_seconds", None)
        self.assertEqual(600, evaluation.pathways_agent_timeout_seconds(test_case))

        for timeout in (1, 1200):
            with self.subTest(timeout=timeout):
                config["agent_timeout_seconds"] = timeout
                evaluation.iter_manifest_parameters(FakePytestConfig("act4", "performix_mcp"), manifest)
                self.assertEqual(timeout, evaluation.pathways_agent_timeout_seconds(test_case))

        for timeout in (0, -1, True, 1.5, "600", None):
            with self.subTest(timeout=timeout):
                config["agent_timeout_seconds"] = timeout
                with self.assertRaisesRegex(pytest.UsageError, "agent_timeout_seconds must be a positive integer"):
                    evaluation.iter_manifest_parameters(FakePytestConfig("act4", "performix_mcp"), manifest)

    def test_iter_manifest_parameters_uses_requested_modes_for_each_testcase(self):
        manifest = {
            "defaults": {"recipe": "code_hotspots", "prerecord": {"mode": "launch"}},
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act2"],
                },
            ],
        }

        params = evaluation.iter_manifest_parameters(FakePytestConfig("act2", "performix_mcp"), manifest)

        self.assertEqual(["performix_mcp"], [param["mode"] for param in params])

    def test_iter_manifest_parameters_uses_requested_recipes(self):
        manifest = {
            "defaults": {"recipe": "code_hotspots", "prerecord": {"mode": "launch"}},
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act2"],
                },
                {
                    "id": "test_case_32",
                    "acts": ["act2"],
                    "recipe": "system_utilization",
                },
            ],
        }

        params = evaluation.iter_manifest_parameters(
            FakePytestConfig(
                "act2",
                modes="performix_mcp",
                recipes="system_utilization",
            ),
            manifest,
        )

        self.assertEqual(
            [("test_case_32", "system_utilization", "performix_mcp")],
            [
                (
                    param["test_case"]["id"],
                    param["test_case"]["recipe"],
                    param["mode"],
                )
                for param in params
            ],
        )

    def test_iter_manifest_parameters_selects_modes_supported_by_each_recipe(self):
        manifest = {
            "defaults": {"recipe": "code_hotspots", "prerecord": {"mode": "launch"}},
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act2"],
                },
                {
                    "id": "test_case_32",
                    "acts": ["act2"],
                    "recipe": "system_utilization",
                },
                {
                    "id": "test_case_41",
                    "acts": ["act2"],
                    "recipe": "syscall_trace_summary",
                },
                {
                    "id": "test_case_46",
                    "acts": ["act2"],
                    "recipe": "cpu_microarchitecture",
                },
                {
                    "id": "test_case_67",
                    "acts": ["act2"],
                    "recipe": "memory_access",
                },
            ],
        }

        params = evaluation.iter_manifest_parameters(
            FakePytestConfig("act2", "performix_mcp"),
            manifest,
        )

        self.assertEqual(
            [
                ("test_case_01", "code_hotspots", "performix_mcp"),
                ("test_case_32", "system_utilization", "performix_mcp"),
                ("test_case_41", "syscall_trace_summary", "performix_mcp"),
                ("test_case_46", "cpu_microarchitecture", "performix_mcp"),
                ("test_case_67", "memory_access", "performix_mcp"),
            ],
            [
                (
                    param["test_case"]["id"],
                    param["test_case"]["recipe"],
                    param["mode"],
                )
                for param in params
            ],
        )

    def test_iter_manifest_parameters_rejects_unsupported_recipe(self):
        manifest = {
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act2"],
                    "recipe": "unsupported_recipe",
                }
            ],
        }

        with self.assertRaisesRegex(
            ValueError,
            "unknown or unsupported recipe type",
        ):
            evaluation.iter_manifest_parameters(
                FakePytestConfig("act2", "performix_mcp"),
                manifest,
            )

    def test_iter_manifest_parameters_rejects_unknown_act(self):
        manifest = {
            "tests": [
                {
                    "id": "test_case_01",
                    "acts": ["act1"],
                },
            ],
        }

        with self.assertRaises(pytest.UsageError) as ctx:
            evaluation.iter_manifest_parameters(FakePytestConfig("act2"), manifest)

        message = str(ctx.exception)
        self.assertIn("Unknown AI Insights act selection: act2", message)
        self.assertIn("Available acts: act1", message)


class AiInsightsPromptTests(unittest.TestCase):
    def test_resolve_mcp_prompt_uses_generic_prompt_by_default(self):
        prompt = evaluation.resolve_mcp_prompt(
            {"id": "test_case_01"},
            "imported-run-id",
        )

        self.assertEqual(
            "Show me AI Insights for Performix run imported-run-id. Use Markdown.\n",
            prompt,
        )

    def test_resolve_mcp_prompt_uses_testcase_prompt(self):
        prompt = evaluation.resolve_mcp_prompt(
            {
                "id": "test_case_01",
                "mcp_prompt": "Why is run {run_id} spending time in this function?",
            },
            "imported-run-id",
        )

        self.assertEqual(
            "Why is run imported-run-id spending time in this function?\n",
            prompt,
        )

    def test_resolve_mcp_prompt_rejects_empty_prompt(self):
        with self.assertRaisesRegex(ValueError, "mcp_prompt must be a non-empty string"):
            evaluation.resolve_mcp_prompt(
                {"id": "test_case_01", "mcp_prompt": "  "},
                "imported-run-id",
            )

    def test_resolve_mcp_prompt_allows_prompt_without_run_id_placeholder(self):
        prompt = evaluation.resolve_mcp_prompt(
            {"id": "test_case_01", "mcp_prompt": "Show me the latest run."},
            "imported-run-id",
        )

        self.assertEqual("Show me the latest run.\n", prompt)

    def test_resolve_mcp_prompt_marks_optional_target_values_as_not_supplied(self):
        prompt = evaluation.resolve_mcp_prompt(
            {
                "id": "pathways_01",
                "mcp_prompt": "Connect to {target_host}:{target_port} as {target_user} with {target_private_key_path}.",
            },
            "imported-run-id",
            {"target_host": "target.example.test", "target_port": ""},
        )

        self.assertEqual(
            "Connect to target.example.test:not supplied as not supplied with not supplied.\n",
            prompt,
        )

    def test_target_prompt_values_allows_omitted_pytest_options(self):
        options = {"--ai-target-host": " target.example.test ", "--ai-target-user": "tester",
                   "--ai-target-port": None, "--ai-target-private-key-path": ""}
        config = mock.Mock()
        config.getoption.side_effect = options.get
        self.assertEqual({"target_host": "target.example.test", "target_user": "tester",
                          "target_port": "", "target_private_key_path": ""},
                         evaluation.target_prompt_values(config))


class AgentShutdownTests(unittest.TestCase):
    def test_timeout_and_cancellation_allow_graceful_exit(self):
        for error in (subprocess.TimeoutExpired("agent", 1), KeyboardInterrupt()):
            with self.subTest(error=type(error).__name__):
                process = mock.MagicMock()
                process.__enter__.return_value = process
                process.wait.side_effect = [error, 0, 0]
                process.poll.side_effect = [None, 0]
                with mock.patch.object(evaluation.subprocess, "Popen", return_value=process), \
                        mock.patch.object(evaluation.os, "name", "posix"):
                    with self.assertRaises(type(error)) as raised:
                        evaluation.run_agent(["agent"], timeout=1)
                self.assertIs(error, raised.exception)
                process.send_signal.assert_called_once_with(evaluation.signal.SIGINT)
                process.kill.assert_not_called()
                self.assertEqual([mock.call(timeout=1), mock.call(timeout=15), mock.call()],
                                 process.wait.call_args_list)

    def test_unresponsive_agent_is_killed_after_grace_period(self):
        process = mock.MagicMock()
        process.__enter__.return_value = process
        error = subprocess.TimeoutExpired("agent", 1)
        process.wait.side_effect = [error, subprocess.TimeoutExpired("agent", 15), 0]
        process.poll.return_value = None
        with mock.patch.object(evaluation.subprocess, "Popen", return_value=process), \
                mock.patch.object(evaluation.os, "name", "posix"):
            with self.assertRaises(subprocess.TimeoutExpired) as raised:
                evaluation.run_agent(["agent"], timeout=1)
        self.assertIs(error, raised.exception)
        process.send_signal.assert_called_once_with(evaluation.signal.SIGINT)
        process.kill.assert_called_once_with()
        self.assertEqual([mock.call(timeout=1), mock.call(timeout=15), mock.call()],
                         process.wait.call_args_list)


class PerformixIsolationTests(unittest.TestCase):
    def test_each_mode_and_attempt_imports_and_cleans_its_own_store(self):
        from performix_attempt import PerformixAttempt
        stores = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "latest.zip").write_bytes(b"fixture")
            case = {"id": "case", "recipe": "asct", "run_artifact": "latest.zip"}
            cfg = {"results_dir": root, "run_artifact_base": root, "cli_bin": Path("apx"),
                   "defaults": {}, "model_config_id": "test"}

            def invoke(directory, metadata, performix):
                state = metadata["performix_state"]
                stores.append(state["data_dir"])
                self.assertTrue(Path(state["data_dir"]).is_dir())
                self.assertEqual(state, performix.state)
                if directory.name == "002":
                    raise RuntimeError("invocation failed")
                return {"truncation_markers": 0}

            def mcp(directory, metadata, case, config, mode, workspace, *, performix):
                return invoke(directory, metadata, performix)

            with (mock.patch.object(PerformixAttempt, "import_fixture", return_value="run") as imports,
                  mock.patch.object(evaluation, "invoke_mcp_mode", side_effect=mcp),
                  mock.patch.object(evaluation.LOGGER, "exception"),
                  mock.patch.object(evaluation, "score_attempt", side_effect=lambda *a: {"final": {"final_label": "pass"}})):
                for mode in evaluation.MCP_MODES:
                    for attempt in (1, 2):
                        with self.subTest(mode=mode, attempt=attempt):
                            result = evaluation.run_attempt(case, mode, cfg, attempt)
                            self.assertEqual("pass" if attempt == 1 else "error", result["score"]["final"]["final_label"])
                            self.assertFalse((result["attempt_dir"] / "performix_state").exists())
                            self.assertFalse((result["attempt_dir"] / "agent_home").exists())
                self.assertEqual(len(stores), imports.call_count)
        self.assertEqual(len(stores), len(set(stores)))

    def test_all_mcp_profiles_use_private_state(self):
        from performix_attempt import PerformixAttempt
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            owner = PerformixAttempt(directory)
            owner.prepare()
            env = owner.environment()
            cfg = {"cli_bin": directory / "apx"}
            for mode in evaluation.MCP_MODES.values():
                with self.subTest(mode=mode.id):
                    home = evaluation.write_codex_home(directory, directory, cfg, mode, False, owner.state)
                    config = tomli.loads((home / "config.toml").read_text())
                    server = config["mcp_servers"][mode.server]["env"]
                    self.assertEqual(env["APXD_DATA_DIR"], server["APXD_DATA_DIR"])
                    self.assertEqual(env["HOME"], server["HOME"])
                    self.assertNotIn("APXD_SERVER_PORT", server)


class AiInsightsWorkspaceTests(unittest.TestCase):
    PATHWAYS_TEST_CASE = {
        "id": "pathways_01",
        "pathways_execution": {
            "workload_source": {
                "repository": "https://example.test/performix-workloads.git",
                "branch": "main",
            }
        },
    }

    def write_profile(self, attempt, *, pathways=True, host="target.example.test", state=None, cli_bin=None):
        workspace = attempt / "codex_workspace"
        workspace.mkdir(parents=True, exist_ok=True)
        state = state or prepare_performix_state(attempt)
        cfg = {"openai_api_key": "test-key", "cli_bin": cli_bin or attempt / "apx",
               "target_prompt_values": {"target_host": host}}
        home = evaluation.write_codex_home(
            attempt, workspace, cfg, evaluation.MCP_MODES["performix_mcp"],
            pathways_execution=pathways, performix_state=state,
        )
        return home, tomli.loads((home / "config.toml").read_text())

    def test_execution_profiles_scope_filesystem_network_and_shell_access(self):
        with tempfile.TemporaryDirectory() as tmp:
            attempt = Path(tmp) / "attempt"
            binary = Path(tmp) / "codex-install/codex"
            state = prepare_performix_state(attempt)
            with mock.patch.object(evaluation.shutil, "which", return_value=str(binary)):
                home, pathways = self.write_profile(attempt, state=state)
            self.assertFalse((home / "auth.json").exists())
            self.assertEqual("OPENAI_API_KEY", pathways["model_providers"]["proxy"]["env_key"])
            self.assertEqual({"OPENAI_API_KEY": "exclude", "OPENAI_PROXY_TOKEN": "exclude"},
                             pathways["shell_environment_policy"]["filters"])
            _, analysis = self.write_profile(attempt, pathways=False, state=state)

        self.assertEqual("ai-insights", pathways["default_permissions"])
        self.assertTrue(pathways["features"]["shell_tool"])
        self.assertTrue(pathways["features"]["network_proxy"])
        permissions = pathways["permissions"]["ai-insights"]
        self.assertEqual({"enabled": True, "allow_local_binding": True,
                          "domains": {"target.example.test": "allow"}}, permissions["network"])
        filesystem = permissions["filesystem"]
        self.assertEqual({".": "write"}, filesystem[":workspace_roots"])
        self.assertEqual("deny", filesystem[str((attempt / "apx").resolve())])
        self.assertEqual(str(attempt / "apx"), pathways["mcp_servers"]["arm-performix"]["command"])
        for runtime in (binary, Path(sys.prefix), Path(sys.base_prefix)):
            self.assertEqual("read", filesystem[str(runtime.resolve())])
        self.assertNotIn(str(binary.parent.resolve()), filesystem)
        env = pathways["mcp_servers"]["arm-performix"]["env"]
        self.assertEqual(state["config_dir"], env["APXD_CONFIG_DIR"])
        self.assertEqual(state["data_dir"], env["APXD_DATA_DIR"])
        self.assertNotIn("windows", pathways)

        self.assertEqual("ai-insights", analysis["default_permissions"])
        self.assertFalse(analysis["features"]["shell_tool"])
        permissions = analysis["permissions"]["ai-insights"]
        self.assertEqual({":minimal": "read", ":workspace_roots": {".": "read"},
                          str((attempt / "apx").resolve()): "deny"}, permissions["filesystem"])
        self.assertFalse(permissions["network"]["enabled"])

    def test_pathways_profile_disables_network_when_target_host_is_omitted(self):
        with tempfile.TemporaryDirectory() as tmp:
            _, profile = self.write_profile(Path(tmp), host="")
        self.assertFalse(profile["features"]["network_proxy"])
        self.assertEqual({"enabled": False, "allow_local_binding": False},
                         profile["permissions"]["ai-insights"]["network"])

    def test_target_network_domain_accepts_exact_hostnames_and_ip_literals(self):
        for host, expected in (("TARGET.example.test", "target.example.test"),
                               ("target.example.test.", "target.example.test."),
                               ("target_alias", "target_alias"), ("192.0.2.10", "192.0.2.10"),
                               ("[2001:db8::10]", "2001:db8::10"), ("  ", None)):
            with self.subTest(host=host):
                self.assertEqual(expected, evaluation.target_network_domain(host))

    def test_target_network_domain_rejects_values_that_widen_access(self):
        for host in ("target.example.test:2222", "*.example.test", "https://target.example.test",
                     "target other", "target\nother", "target?", "[target]"):
            with self.subTest(host=host):
                with self.assertRaisesRegex(evaluation.AiInsightsConfigError, "literal hostname or IP address"):
                    evaluation.target_network_domain(host)

    @unittest.skipIf(os.name == "nt", "Native Windows agent evaluations are unsupported; run inside WSL2")
    @unittest.skipUnless(
        os.environ.get("AI_INSIGHTS_RUN_CODEX_SANDBOX_TESTS") == "1",
        "set AI_INSIGHTS_RUN_CODEX_SANDBOX_TESTS=1 on a host with the Codex sandbox configured",
    )
    def test_pathways_profile_blocks_host_apx_and_previous_attempt_artifacts_at_runtime(self):
        """The agent can edit its workspace but cannot access APX or earlier evidence."""
        codex_bin = shutil.which("codex")
        self.assertIsNotNone(codex_bin, "The requested sandbox integration test requires Codex on PATH")
        with tempfile.TemporaryDirectory(dir=HARNESS_DIR) as tmpdir:
            attempts_dir = Path(tmpdir) / "attempts"
            previous_attempt = attempts_dir / "001"
            previous_attempt.mkdir(parents=True)
            artifact = previous_attempt / "llm_response.md"
            contents = "+previous-agent-fix\n"
            artifact.write_text(contents, encoding="utf-8")
            # Only retained artifacts remain; the previous agent is no longer running.
            attempt_dir = attempts_dir / "002"
            workspace = attempt_dir / "codex_workspace"
            workspace.mkdir(parents=True)
            (workspace / "permitted.txt").write_text("permitted", encoding="utf-8")
            # Put APX under a writable root to exercise the explicit deny rule.
            apx = workspace / "apx"
            apx.write_text("#!/bin/sh\nprintf 'apx-ran\\n'\n")
            apx.chmod(0o755)
            alias = workspace / "apx-alias"
            alias.symlink_to(apx)
            self.assertEqual("apx-ran\n", subprocess.check_output([str(apx)], text=True))
            probe = workspace / "probe.py"
            probe.write_text(textwrap.dedent("""
                import os
                import shutil
                import subprocess
                import sys
                from pathlib import Path
                assert not {"OPENAI_API_KEY", "OPENAI_PROXY_TOKEN"} & os.environ.keys()
                operation, filename = sys.argv[1:]
                path = Path(filename)
                try:
                    if operation == "read":
                        print(path.read_text(), end="")
                    elif operation == "write":
                        path.write_text("sandbox-write\\n")
                    elif operation == "execute":
                        subprocess.run([filename], check=True)
                    elif operation == "copy":
                        shutil.copyfile(path, "copied-apx")
                    else:
                        print([entry.name for entry in path.iterdir()])
                except (PermissionError, FileNotFoundError):
                    raise SystemExit(13)
            """))
            performix_state = prepare_performix_state(attempt_dir)
            agent_home = attempt_dir / "agent_home"
            agent_home.mkdir()
            (agent_home / "scoped-key").write_text("restricted credential")
            performix_state["home_dir"] = str(agent_home)
            codex_home, _ = self.write_profile(attempt_dir, state=performix_state, cli_bin=apx)
            env = {**os.environ, "CODEX_HOME": str(codex_home),
                   "PATH": str(workspace) + os.pathsep + os.environ["PATH"],
                   "OPENAI_API_KEY": "test-key", "OPENAI_PROXY_TOKEN": "test-key"}
            def run_probe(operation: str, path: Path | str):
                return subprocess.run(
                    [codex_bin, "sandbox", "--permission-profile", "ai-insights",
                     "--cd", str(workspace), sys.executable, str(probe), operation, str(path)],
                    capture_output=True,
                    text=True,
                    env=env,
                    timeout=30,
                )

            # Fail here if the sandbox cannot start the helper. A setup failure
            # must never count as proof that access to earlier evidence is denied.
            permitted = run_probe("read", "permitted.txt")
            self.assertEqual(0, permitted.returncode, permitted.stderr)
            self.assertEqual("permitted", permitted.stdout.strip())
            home_read = run_probe("read", agent_home / "scoped-key")
            self.assertEqual(0, home_read.returncode, home_read.stderr)
            self.assertEqual("restricted credential", home_read.stdout.strip())
            writable = run_probe("write", "written.txt")
            self.assertEqual(0, writable.returncode, writable.stderr)
            self.assertEqual("sandbox-write\n", (workspace / "written.txt").read_text(encoding="utf-8"))
            denied_operations = [
                ("read", artifact),
                ("list", previous_attempt),
                ("write", artifact),
                ("write", previous_attempt / "agent-output.txt"),
                ("read", HARNESS_DIR / "README.md"),
                ("read", apx), ("copy", apx), ("execute", apx), ("execute", alias),
            ]
            for operation, path in denied_operations:
                for probe_path in (os.path.relpath(path, workspace), path):
                    with self.subTest(operation=operation, path=str(probe_path)):
                        result = run_probe(operation, probe_path)
                        self.assertEqual(13, result.returncode, result.stdout + result.stderr)
                        self.assertNotIn(contents.strip(), result.stdout + result.stderr)
                        self.assertNotIn(artifact.name, result.stdout)
            by_name = run_probe("execute", "apx")
            self.assertEqual(13, by_name.returncode, by_name.stdout + by_name.stderr)
            self.assertEqual(contents, artifact.read_text(encoding="utf-8"))
            self.assertFalse((previous_attempt / "agent-output.txt").exists())

    def test_pathways_invocation_allows_shell_commands_and_passes_timeout(self):
        with tempfile.TemporaryDirectory() as tmp:
            attempt = Path(tmp)
            workspace = attempt / "codex_workspace"
            workspace.mkdir()
            home = attempt / "codex_home"
            session = home / "sessions/session.jsonl"
            session.parent.mkdir(parents=True)
            session.write_text("{}\n")

            def run_codex(command, **kwargs):
                if command == ["codex", "--version"]:
                    return mock.Mock(stdout="codex 1.0", stderr="", returncode=0)
                Path(command[command.index("--output-last-message") + 1]).write_text("Completed.")
                return mock.Mock(returncode=0)

            runtime = mock.Mock(state={})
            runtime.environment.return_value = {"PATH": "/runtime/bin"}
            cfg = {"model": "test-model", "reasoning_effort": "", "openai_api_key": "test-key", "target_prompt_values": {},
                   "pathways_ssh_command": "ssh -F /tmp/ssh-config pathways"}
            with (
                mock.patch.object(evaluation.shutil, "which", return_value="codex"),
                mock.patch.object(evaluation, "write_codex_home", return_value=home),
                mock.patch.object(evaluation.subprocess, "run", return_value=mock.Mock(stdout="codex 1.0")),
                mock.patch.object(evaluation, "run_agent", side_effect=run_codex) as run,
                mock.patch.object(evaluation, "validate_mcp_call", return_value={}),
                mock.patch.object(evaluation, "collect_mcp_tool_metrics", return_value={}),
                mock.patch.object(evaluation, "successful_external_commands", return_value=["ssh target true"]),
            ):
                for options, timeout in (({}, 600), ({"agent_timeout_seconds": 1200}, 1200)):
                    with self.subTest(timeout=timeout):
                        case = {**self.PATHWAYS_TEST_CASE, "pathways_execution": {
                            **self.PATHWAYS_TEST_CASE["pathways_execution"], **options}}
                        result = evaluation.invoke_mcp_mode(
                            attempt, {"test_id": "pathways_01", "run_id": "run-1"},
                            case, cfg, evaluation.MCP_MODES["performix_mcp"], workspace,
                            performix=runtime,
                        )
                        self.assertEqual(timeout, run.call_args.kwargs["timeout"])
                        self.assertEqual({"PATH": "/runtime/bin", "CODEX_HOME": str(home),
                                          "OPENAI_API_KEY": "test-key", "OPENAI_PROXY_TOKEN": "test-key"},
                                         run.call_args.kwargs["env"])
                        self.assertEqual(["ssh target true"], result["successful_external_commands"])

    def test_prepare_agent_workspace_clones_a_clean_pathways_checkout(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            # git clone, rev-parse, then status --porcelain.
            replies = [mock.Mock(returncode=0, stdout=value) for value in ("", "abc123\n", "")]
            with mock.patch.object(evaluation.subprocess, "run", side_effect=replies) as git:
                workspace, checkout = evaluation.prepare_agent_workspace(root, self.PATHWAYS_TEST_CASE)
            self.assertEqual(root / "codex_workspace", workspace)
            source = self.PATHWAYS_TEST_CASE["pathways_execution"]["workload_source"]
            self.assertEqual({**source, "commit": "abc123", "initial_status": "clean",
                              "workspace": str(workspace)}, checkout)
            self.assertEqual(["git", "clone", "--branch", "main", "--single-branch",
                              source["repository"], str(workspace)], git.call_args_list[0].args[0])
            self.assertEqual([1800] * 3, [call.kwargs["timeout"] for call in git.call_args_list])

    def test_prepare_agent_workspace_keeps_non_pathways_workspace_empty(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            workspace, checkout = evaluation.prepare_agent_workspace(
                Path(tmpdir),
                {"id": "test_case_01"},
            )
            self.assertTrue(workspace.is_dir())
            self.assertIsNone(checkout)

    def test_prepare_agent_workspace_reports_clone_failure(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            failed_clone = SimpleNamespace(returncode=1, stdout="", stderr="repository unavailable")
            with (
                mock.patch.object(evaluation.shutil, "which", return_value="git"),
                mock.patch.object(evaluation.subprocess, "run", return_value=failed_clone),
            ):
                with self.assertRaisesRegex(RuntimeError, "workload checkout clone failed: repository unavailable"):
                    evaluation.prepare_agent_workspace(Path(tmpdir), self.PATHWAYS_TEST_CASE)

    @contextmanager
    def prepared_attempt(self):
        """Exercise real coordination and cleanup with a fake target and agent."""
        from test_pathways_attempt import fake_pathways_attempt
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            directory = root / "pathways_01/performix_mcp/attempts/001"
            workspace = root / "workspace"
            workspace.mkdir()
            runtime = fake_pathways_attempt(directory)
            cfg = {"results_dir": root, "cli_bin": Path("apx"), "defaults": {},
                   "model_config_id": "test", "target_prompt_values": {"target_host": "target"}}
            with (
                mock.patch("pathways_attempt.create_pathways_attempt", return_value=runtime),
                mock.patch.object(evaluation, "import_attempt_run", wraps=evaluation.import_attempt_run) as import_run,
                mock.patch.object(evaluation, "prepare_agent_workspace", return_value=(workspace, {"commit": "baseline"})),
                mock.patch("pathways_attempt.persist_codex_session_log", wraps=evaluation.persist_codex_session_log) as capture,
                mock.patch.object(evaluation, "invoke_mcp_mode", return_value={"truncation_markers": 0}) as invoke,
                mock.patch.object(evaluation, "score_attempt", return_value={
                    "test_id": "pathways_01", "judge": judge_verdict(None),
                    "response_observations": {"issues": [], "details": []},
                    "final": {"final_label": "pass"}}) as score,
                mock.patch.object(evaluation.LOGGER, "exception"),
            ):
                yield SimpleNamespace(
                    directory=directory, workspace=workspace, runtime=runtime,
                    import_run=import_run, invoke=invoke, score=score, capture=capture,
                    run=lambda: evaluation.run_attempt(self.PATHWAYS_TEST_CASE, "performix_mcp", cfg, 1),
                )

    def test_run_attempt_passes_checkout_and_private_state_to_mcp(self):
        with self.prepared_attempt() as attempt:
            def invoke(directory, metadata, case, config, mode, workspace, *, performix):
                self.assertIsNone(metadata["run_id"])
                self.assertNotIn("archive_path", metadata)
                self.assertEqual(attempt.workspace, workspace)
                self.assertIs(attempt.runtime.performix, performix)
                self.assertEqual(performix.state, metadata["performix_state"])
                self.assertIsNot(performix.state, metadata["performix_state"])
                for key in ("config_dir", "data_dir", "state_dir"):
                    self.assertEqual([], list(Path(metadata["performix_state"][key]).iterdir()))
                return {"truncation_markers": 0}

            attempt.invoke.side_effect = invoke
            with mock.patch.object(attempt.runtime.performix, "import_fixture") as import_fixture:
                result = attempt.run()
            import_fixture.assert_not_called()
            self.assertEqual(str(attempt.workspace), result["run_meta"]["agent_workspace"])
            self.assertEqual({"commit": "baseline"}, result["run_meta"]["workload_checkout"])
            self.assertEqual("pass", result["score"]["final"]["final_label"])
            self.assertFalse((attempt.directory / "performix_state").exists())

    def write_attempt_diagnostics(self, directory, transcript):
        session = directory / "codex_home/sessions/session.jsonl"
        session.parent.mkdir(parents=True)
        session.write_bytes(transcript)
        for name in ("codex_exec.jsonl", "codex_stderr.txt"):
            (directory / name).write_text("diagnostic output")

    def test_failures_preserve_evidence(self):
        errors = {"setup": RuntimeError("setup failed"), "invocation": RuntimeError("agent failed"),
                  "judge": RuntimeError("judge failed")}
        for phase, error in errors.items():
            with self.subTest(phase=phase), self.prepared_attempt() as attempt:
                transcript = b'{"edit":"edit A; edit B; revert A"}\n{"partial":'

                def invoke(*args, **kwargs):
                    self.write_attempt_diagnostics(attempt.directory, transcript)
                    if phase == "invocation":
                        raise error
                    return {"truncation_markers": 0, "duration_seconds": 10}

                attempt.invoke.side_effect = invoke
                if phase == "setup":
                    attempt.import_run.side_effect = error
                if phase == "judge":
                    attempt.score.side_effect = error
                result = attempt.run()
                self.assertEqual("error", result["invoke"]["status"])
                self.assertTrue((attempt.directory / "performix_runs.json").is_file())
                self.assertIn("Verdict: ERROR", (attempt.directory / "score.md").read_text())
                self.assertFalse((attempt.directory / "invoke_metadata.json").exists())
                if phase != "setup":
                    self.assertEqual(transcript, (attempt.directory / "codex_session.jsonl").read_bytes())
                    for name in ("codex_exec.jsonl", "codex_stderr.txt"):
                        self.assertEqual("diagnostic output", (attempt.directory / name).read_text())
                if phase == "judge":
                    self.assertEqual(10, result["invoke"]["duration_seconds"])

    def test_timeout_preserves_diagnostics_and_reports_limit(self):
        with self.prepared_attempt() as attempt:
            transcript = b'{"edit":"unfinished work"}\n{"partial":'

            def timeout(*args, **kwargs):
                self.write_attempt_diagnostics(attempt.directory, transcript)
                raise subprocess.TimeoutExpired("codex", 600)

            attempt.invoke.side_effect = timeout
            with mock.patch.object(evaluation.time, "monotonic", side_effect=[100, 701.5]):
                result = attempt.run()
            self.assertEqual("error", result["invoke"]["status"])
            self.assertEqual(601.5, result["invoke"]["duration_seconds"])
            self.assertEqual(600, result["invoke"]["timeout_seconds"])
            attempt.score.assert_not_called()
            self.assertEqual(transcript, (attempt.directory / "codex_session.jsonl").read_bytes())
            for name in ("codex_exec.jsonl", "codex_stderr.txt"):
                self.assertEqual("diagnostic output", (attempt.directory / name).read_text())
            self.assertTrue((attempt.directory / "performix_runs.json").is_file())
            self.assertIn("Verdict: ERROR", (attempt.directory / "score.md").read_text())
            self.assertFalse((attempt.directory / "invoke_metadata.json").exists())

            properties = {}
            cfg = {"results_dir": attempt.directory.parents[3], "model": "test"}
            with (mock.patch.object(evaluation, "resolve_config", return_value=cfg),
                  mock.patch.object(evaluation, "run_attempt", return_value=result),
                  self.assertRaises(pytest.fail.Exception) as failure):
                evaluation.test_ai_insights(SimpleNamespace(_ai_insights_suite_properties_recorded=True),
                    properties.__setitem__, mock.Mock(), self.PATHWAYS_TEST_CASE, "performix_mcp", 1, 1)
            self.assertEqual(601.5, properties["ai_agent_duration_seconds"])
            self.assertEqual(600, properties["ai_agent_timeout_seconds"])
            message = str(failure.exception)
            for text in ("execution: TIMEOUT", "600s agent time limit", "judge: not run", "codex_stderr.txt", "codex_exec.jsonl"):
                self.assertIn(text, message)
            self.assertNotIn("did not satisfy the testcase rubric", message)

    def test_target_setup_error_reports_diagnostic_without_traceback(self):
        with self.prepared_attempt() as attempt, mock.patch.object(evaluation.LOGGER, "error") as log:
            message = "SSH command failed on ubuntu@target:22 (exit 1): /workspace already exists"
            attempt.runtime.target.seed.side_effect = evaluation.TargetAccessError(message)
            result = attempt.run()
            log.assert_called_once_with("%s/%s attempt %03d failed during %s: %s",
                                        "pathways_01", "performix_mcp", 1, "setup", message)
            evaluation.LOGGER.exception.assert_not_called()
            attempt.invoke.assert_not_called()
            self.assertEqual(message, result["invoke"]["error"])
            self.assertIn(message, (attempt.directory / "score.md").read_text())

    def test_cancellation_preserves_evidence_before_propagating(self):
        with self.prepared_attempt() as attempt:
            transcript = b'{"edit":"unfinished work"}\n{"partial":'

            def cancel(*args, **kwargs):
                session = attempt.directory / "codex_home/sessions/session.jsonl"
                session.parent.mkdir(parents=True)
                session.write_bytes(transcript)
                raise KeyboardInterrupt("cancelled")

            attempt.invoke.side_effect = cancel
            with self.assertRaisesRegex(KeyboardInterrupt, "cancelled"):
                attempt.run()
            self.assertEqual(transcript, (attempt.directory / "codex_session.jsonl").read_bytes())
            self.assertTrue((attempt.directory / "performix_runs.json").is_file())
            self.assertIn("Verdict: ERROR", (attempt.directory / "score.md").read_text())
            self.assertFalse((attempt.directory / "invoke_metadata.json").exists())

    def test_session_capture_failure_is_reported_and_keeps_primary_error(self):
        for operation, original_error in (("invoke", None), ("invoke", "agent failed"), ("score", "judge failed")):
            with self.subTest(error=original_error), self.prepared_attempt() as attempt:
                getattr(attempt, operation).side_effect = RuntimeError(original_error) if original_error else None
                def invoke(*args, **kwargs):
                    self.write_attempt_diagnostics(attempt.directory, b"{}\n")
                    if operation == "invoke" and original_error:
                        raise RuntimeError(original_error)
                    return {"truncation_markers": 0}

                attempt.invoke.side_effect = invoke
                attempt.capture.side_effect = RuntimeError("session unavailable")
                result = attempt.run()
                score = json.loads((attempt.directory / "score.json").read_text())
                self.assertEqual(result["score"], score)
                self.assertEqual("error", score["final"]["final_label"])
                self.assertIn("session unavailable", score["cleanup_errors"][0])
                report = (attempt.directory / "score.md").read_text()
                self.assertTrue(report.startswith("# Verdict: ERROR"))
                self.assertIn("session unavailable", report)
                if original_error:
                    self.assertIn(original_error, score["response_observations"]["details"])
                    self.assertIn(original_error, report)
                    self.assertIn(original_error, result["invoke"]["error"])
                else:
                    self.assertIn("- Judge: `pass`", report)
                    self.assertIn(score["judge"]["verdict_rationale"], report)
                properties = {}
                cfg = {"results_dir": attempt.directory.parents[3], "model": "test"}
                with (mock.patch.object(evaluation, "resolve_config", return_value=cfg),
                      mock.patch.object(evaluation, "run_attempt", return_value=result),
                      self.assertRaises(pytest.fail.Exception) as failure):
                    evaluation.test_ai_insights(SimpleNamespace(_ai_insights_suite_properties_recorded=True),
                        properties.__setitem__, mock.Mock(), self.PATHWAYS_TEST_CASE, "performix_mcp", 1, 1)
                self.assertEqual("pathways_01", properties["ai_test_id"])
                self.assertEqual("error", properties["ai_scores"])
                self.assertIn("session unavailable", " ".join(str(failure.exception).split()))

    def test_pathways_collision_does_not_delete_prior_evidence(self):
        with self.prepared_attempt() as attempt:
            attempt.directory.mkdir(parents=True)
            evidence = attempt.directory / "codex_exec.jsonl"
            evidence.write_text("previous attempt")
            with self.assertRaises(FileExistsError):
                attempt.run()
            self.assertEqual("previous attempt", evidence.read_text())


class AiInsightsPreRecordedInputTests(unittest.TestCase):
    def test_selected_testcases_from_xdist_nodeids_deduplicates_modes(self):
        testcases = selected_testcases_from_nodeids(
            [
                {
                    "id": "case-alpha",
                    "recipe": "code_hotspots",
                    "run_artifact": "case-alpha/latest.zip",
                },
                {
                    "id": "test_case_27",
                    "recipe": "code_hotspots",
                    "run_artifact": "test_case_27/latest.zip",
                },
            ],
            [
                "test_ai_insights_evaluation.py::test_ai_insights[case-alpha-summary-performix_mcp-attempt_001]",
                "test_ai_insights_evaluation.py::test_ai_insights[case-alpha-summary-other_mode-attempt_001]",
                "test_ai_insights_evaluation.py::test_ai_insights[test_case_27-summary-performix_mcp-attempt_001]",
            ],
        )

        self.assertEqual(
            ["case-alpha", "test_case_27"],
            [test_case["id"] for test_case in testcases],
        )

    def test_selected_testcases_from_xdist_nodeids_rejects_unmapped_testcase(self):
        with self.assertRaises(pytest.UsageError) as ctx:
            selected_testcases_from_nodeids(
                [
                    {
                        "id": "test_case_26",
                        "recipe": "code_hotspots",
                        "run_artifact": "test_case_26/latest.zip",
                    }
                ],
                [
                    "test_ai_insights_evaluation.py::test_ai_insights[unknown-summary-performix_mcp-attempt_001]",
                ],
            )

        self.assertIn("Could not map AI Insights pytest node ID", str(ctx.exception))
        self.assertIn("unknown-summary-performix_mcp-attempt_001", str(ctx.exception))

    def test_xdist_collection_prepares_once_from_worker_collection(self):
        nodeids = [
            "test_ai_insights_evaluation.py::test_ai_insights[test_case_26-summary-performix_mcp-attempt_001]",
            "test_ai_insights_evaluation.py::test_ai_insights[test_case_26-summary-performix_mcp-attempt_002]",
        ]

        with tempfile.TemporaryDirectory() as tmpdir:
            manifest_path = Path(tmpdir) / "manifest.json"
            manifest_path.write_text(
                json.dumps(
                    {
                        "defaults": {"prerecord": {"mode": "launch"}},
                        "tests": [
                            {
                                "id": "test_case_26",
                                "recipe": "code_hotspots",
                                "run_artifact": "test_case_26/latest.zip",
                            },
                        ],
                    }
                ),
                encoding="utf-8",
            )
            config = FakeXdistConfig(manifest_path)
            prepared = []

            with mock.patch.object(
                conftest,
                "_prepare_selected_testcases",
                side_effect=lambda _config, testcases: prepared.append(
                    [test_case["id"] for test_case in testcases]
                ),
            ):
                conftest.pytest_xdist_node_collection_finished(
                    FakeXdistNode(config),
                    nodeids,
                )
                conftest.pytest_xdist_node_collection_finished(
                    FakeXdistNode(config),
                    nodeids,
                )
                conftest.pytest_xdist_node_collection_finished(
                    FakeXdistNode(config),
                    nodeids,
                )

        self.assertEqual([["test_case_26"]], prepared)

    def test_download_missing_run_artifacts_raises_for_artifactory_misses(self):
        lines = []
        calls = []

        def write_line(line: str, **markup):
            lines.append((line, markup))

        def fail_download(
            artifactory_run_base,
            test_id,
            filename,
            destination_dir,
            destination_name,
            required=True,
        ):
            calls.append((filename, required))
            raise pytest.UsageError("download failed")

        testcases = [
            {
                "id": "test_case_26",
                "recipe": "code_hotspots",
                "run_artifact": "test_case_26/latest.zip",
            },
        ]

        with tempfile.TemporaryDirectory() as tmpdir:
            with mock.patch.object(conftest, "_download_artifactory_input", side_effect=fail_download):
                with self.assertRaises(pytest.UsageError):
                    conftest._download_missing_run_artifacts(
                        testcases,
                        Path(tmpdir),
                        "its.apx-prerecorded-runs/ai-insights-evaluation",
                        write_line,
                    )

        self.assertEqual([("latest.zip", True)], calls)
        self.assertTrue(any("test_case_26: latest.zip" in line for line, _ in lines))
        self.assertFalse(any("WARNING" in line for line, _ in lines))

    def test_prepare_run_inputs_raises_for_missing_local_inputs(self):
        testcases = [
            {
                "id": "test_case_26",
                "recipe": "code_hotspots",
                "run_artifact": "test_case_26/latest.zip",
            }
        ]

        with tempfile.TemporaryDirectory() as tmpdir, self.assertRaisesRegex(
            pytest.UsageError,
            "(?s)test_case_26 run archive.*test_case_26 source archive",
        ):
            conftest._prepare_run_inputs(
                testcases,
                Path(tmpdir),
                "",
                Path(tmpdir) / "results",
                lambda _line: None,
            )


class AiInsightsExpectedFailureTests(unittest.TestCase):
    def test_expected_failure_for_mode_accepts_mode_overrides(self):
        test_case = {
            "id": "test_case_01",
            "expected_failures": {
                "modes": {
                    "performix_mcp": {
                        "expected_failure": True,
                        "reason": "Known profiling-context limitation.",
                    },
                },
            },
        }

        mcp_expectation = evaluation.expected_failure_for_mode(test_case, "performix_mcp")
        other_expectation = evaluation.expected_failure_for_mode(test_case, "other_mode")

        self.assertTrue(mcp_expectation["expected_failure"])
        self.assertEqual("Known profiling-context limitation.", mcp_expectation["reason"])
        self.assertFalse(other_expectation["expected_failure"])

    def test_expected_failure_for_mode_accepts_testcase_default(self):
        test_case = {
            "id": "test_case_14",
            "expected_failures": {
                "expected_failure": True,
                "reason": "Known profiling-context limitation.",
            },
        }

        expectation = evaluation.expected_failure_for_mode(test_case, "performix_mcp")

        self.assertTrue(expectation["expected_failure"])
        self.assertEqual("Known profiling-context limitation.", expectation["reason"])

    def test_display_label_marks_expected_failures(self):
        self.assertEqual("xfail", evaluation.display_label("fail", expected_failure=True))
        self.assertEqual("error", evaluation.display_label("error", expected_failure=True))
        self.assertEqual("xpass", evaluation.display_label("pass", expected_failure=True))
        self.assertEqual("fail", evaluation.display_label("fail", expected_failure=False))

    def test_should_xfail_final_label_only_accepts_judged_failures(self):
        self.assertTrue(evaluation.should_xfail_final_label("fail", expected_failure=True))
        self.assertFalse(evaluation.should_xfail_final_label("error", expected_failure=True))
        self.assertFalse(evaluation.should_xfail_final_label("pass", expected_failure=True))
        self.assertFalse(evaluation.should_xfail_final_label("fail", expected_failure=False))

    def test_tool_output_truncation_policy(self):
        invoke_meta = {"truncation_markers": 1}

        self.assertTrue(evaluation.should_fail_on_tool_output_truncation("performix_mcp", invoke_meta))
        self.assertEqual("error", evaluation.tool_output_truncation_severity("performix_mcp", invoke_meta))
        self.assertEqual(
            "",
            evaluation.tool_output_truncation_severity("performix_mcp", {"truncation_markers": 0}),
        )
        self.assertFalse(
            evaluation.should_fail_on_tool_output_truncation("performix_mcp", {"truncation_markers": "unknown"}),
        )
        self.assertEqual(
            "",
            evaluation.tool_output_truncation_severity("performix_mcp", {"truncation_markers": "unknown"}),
        )

    def test_pathways_keeps_junit_summary_without_advertising_discarded_metadata(self):
        for pathways in (False, True):
            with self.subTest(pathways=pathways), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                directory = root / "case/performix_mcp/attempts/001"
                directory.mkdir(parents=True)
                case = {"id": "case", **({"pathways_execution": {"mode": "host_ssh"}} if pathways else {})}
                result = {"attempt_dir": directory, "pathways": pathways,
                          "run_meta": {"run_id": "fixture"}, "invoke": {"duration_seconds": 1.2},
                          "score": {"final": {"final_label": "pass"}}}
                properties = {}
                config = SimpleNamespace(_ai_insights_suite_properties_recorded=True)
                with (mock.patch.object(evaluation, "resolve_config", return_value={"results_dir": root, "model": "test"}),
                      mock.patch.object(evaluation, "run_attempt", return_value=result)):
                    evaluation.test_ai_insights(config, properties.__setitem__, mock.Mock(),
                                                case, "performix_mcp", 1, 10)
                self.assertEqual(not pathways, (directory / "aggregate.json").exists())
                for name in ("ai_aggregate_path", "ai_run_meta_path", "ai_invoke_metadata_path"):
                    self.assertEqual(not pathways, name in properties, name)
                expected = {"ai_test_id": "case", "ai_attempt": 1, "ai_attempts_total": 10,
                            "ai_passes": 1, "ai_pass_rate": 1.0, "ai_scores": "pass",
                            "ai_display_scores": "pass", "ai_run_ids": "fixture", "ai_agent_duration_seconds": 1.2}
                self.assertEqual(expected, {name: properties[name] for name in expected})

    def test_record_evaluation_properties_includes_truncation_metadata(self):
        properties = {}

        evaluation.record_evaluation_properties(
            lambda name, value: properties.setdefault(name, str(value)),
            {
                "test_id": "test_case_03",
                "mode": "performix_mcp",
                "attempt": 1,
                "attempts_total": 1,
                "attempts": 1,
                "passes": 1,
                "pass_rate": 1.0,
                "scores": ["pass"],
                "display_scores": ["pass"],
                "expected_failure": False,
                "expected_failure_reason": "",
                "artifact_dir": "/tmp/result",
                "artifact_rel_dir": "test_case_03/performix_mcp/attempts/001",
                "aggregate_path": "/tmp/result/aggregate.json",
            },
            [
                {
                    "attempt_dir": Path("/tmp/result"),
                    "run_meta": {"run_id": "run-1", "archive_sha256": "abc"},
                    "score": {"judge": {"label": "pass", "confidence": "high"}},
                    "invoke": {
                        "mode": "performix_mcp",
                        "duration_seconds": 1.2,
                        "mcp": {"completed_calls": 1},
                        "truncation_markers": 1,
                        "truncation_marker_token_counts": [1234],
                        "token_usage": [
                            {
                                "input_tokens": 12345,
                                "cached_input_tokens": 2345,
                                "output_tokens": 2812,
                                "reasoning_output_tokens": 1954,
                            }
                        ],
                    },
                },
            ],
        )

        self.assertEqual("True", properties["ai_tool_output_truncated"])
        self.assertEqual("1", properties["ai_tool_output_truncation_markers"])
        self.assertEqual("1234", properties["ai_tool_output_truncated_tokens"])
        self.assertEqual("1234", properties["ai_tool_output_truncation_token_counts"])
        self.assertEqual("error", properties["ai_tool_output_truncation_severity"])
        self.assertEqual("12345", properties["ai_input_tokens"])
        self.assertEqual("2345", properties["ai_cached_input_tokens"])
        self.assertEqual("858", properties["ai_output_tokens"])
        self.assertEqual("1954", properties["ai_reasoning_output_tokens"])

        properties["ai_mode"] = "performix_mcp"
        metrics = performance_quality.performance_metrics_for_attempt(
            properties,
            performance_thresholds={
                "test_case_03": {
                    "duration_seconds": 2,
                    "input_tokens": 20000,
                    "output_tokens": 1000,
                }
            },
        )
        output_metric = next(metric for metric in metrics if metric.key == "output_tokens")
        self.assertEqual(858, output_metric.value)
        self.assertEqual("GOOD", output_metric.quality)


def judge_verdict(policy, label="pass"):
    return {"label": label, "confidence": "high", "what_was_correct": "Improved runtime",
            "material_gaps": "", "verdict_rationale": "Assessment", "mcp_policy": policy}


class PathwaysPolicyTests(unittest.TestCase):
    def test_policy_requires_explicit_pass_without_violations(self):
        self.assertTrue(evaluation.mcp_policy_passes({"label": "pass", "violations": []}))
        for policy in (None, "pass", {}, {"label": "pass"},
                       {"label": "fail", "violations": []}, {"label": "pass", "violations": ["bypass"]}):
            with self.subTest(policy=policy):
                self.assertFalse(evaluation.mcp_policy_passes(policy))


class PathwaysScoringTests(unittest.TestCase):
    CASE = {"id": "pathways_01", "rubric": "rubrics/pathways_01.md",
            "pathways_execution": {"workload_source": {"repository": "example", "branch": "main"}}}

    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        (self.root / "llm_response.md").write_text("The optimization improves runtime.")
        (self.root / "run_meta.json").write_text('{"run_id":"fixture"}')
        self.transcript = self.write_call({"run_id": "new", "run_status": "completed"})
        patcher = mock.patch.object(evaluation, "judge_response",
                                    return_value=judge_verdict({"label": "pass", "violations": []}))
        self.judge = patcher.start()
        self.addCleanup(patcher.stop)

    def write_call(self, result, server="arm-performix"):
        event = {"payload": {"type": "mcp_tool_call_end", "call_id": "profile",
                 "invocation": {"server": server, "tool": "run_recipe", "arguments": {"recipe": "code_hotspots"}},
                 "result": {"Ok": result}}}
        transcript = json.dumps(event) + "\n"
        (self.root / "codex_session.jsonl").write_text(transcript)
        return transcript

    def replay(self, supplied_run_id, reasoning=None):
        import judge_ai_insights_response as replay
        args = ["judge", str(HARNESS_DIR / self.CASE["rubric"]), str(self.root / "llm_response.md"),
                "--pathways-session", str(self.root / "codex_session.jsonl")]
        if supplied_run_id:
            args += ["--supplied-run-id", supplied_run_id]
        if reasoning:
            args += ["--judge-reasoning-effort", reasoning]
        with (mock.patch.object(sys, "argv", args), mock.patch.dict(os.environ, {"OPENAI_API_KEY": "test"}),
              mock.patch.object(replay, "judge_response", self.judge), mock.patch("sys.stdout", new=io.StringIO())):
            return replay.main()

    def test_replay_judge_reasoning_defaults_and_overrides(self):
        for env, option, expected in (({}, None, "medium"),
                                      ({"AI_INSIGHTS_JUDGE_REASONING_EFFORT": "high"}, None, "high"),
                                      ({"AI_INSIGHTS_JUDGE_REASONING_EFFORT": "high"}, "low", "low")):
            with self.subTest(expected=expected), mock.patch.dict(os.environ, env, clear=True):
                self.assertEqual(0, self.replay("fixture", option))
                self.assertEqual(expected, self.judge.call_args.args[3]["judge_reasoning_effort"])

    def test_pathways_prompt_keeps_task_and_connection_details(self):
        prompt = evaluation.resolve_mcp_prompt(
            {**self.CASE, "mcp_prompt": "Investigate {run_id}."}, "fixture",
            {"target_host": "target.example.test", "target_port": "22", "target_user": "tester",
             "target_private_key_path": "/tmp/target-key"},
            ssh_command="ssh -F /tmp/ssh-config pathways",
        )
        for text in ("Investigate fixture.", "Initial Performix run: fixture.", "/workspace",
                     "target.example.test", "22", "tester", "/tmp/target-key",
                     "ssh -F /tmp/ssh-config pathways"):
            self.assertIn(text, prompt)
        self.assertNotIn("Do not use the Performix CLI", prompt)
        ordinary = evaluation.resolve_mcp_prompt({"id": "ordinary", "mcp_prompt": "Inspect {run_id}."}, "fixture")
        self.assertEqual("Inspect fixture.\n", ordinary)

    def test_evaluation_and_replay_send_the_same_run_evidence(self):
        score = evaluation.score_attempt(self.root, self.CASE, {})
        evidence = self.judge.call_args.kwargs["pathways_evidence"]
        self.assertEqual({"session_jsonl": self.transcript, "successful_recipe_run_ids": ["new"],
                          "supplied_run_id": "fixture"}, evidence)
        self.assertEqual("pass", score["final"]["final_label"])
        self.assertEqual(0, self.replay("fixture"))
        self.assertEqual(evidence, self.judge.call_args.kwargs["pathways_evidence"])

    def test_evaluation_and_replay_follow_judge_without_fresh_profiling(self):
        self.write_call({"isError": True})
        policy = {"label": "pass", "violations": []}
        for run_id, label in (("fixture", "pass"), (None, "pass"), (None, "fail")):
            with self.subTest(run_id=run_id, label=label):
                (self.root / "run_meta.json").write_text(json.dumps({"run_id": run_id}))
                self.judge.return_value = judge_verdict(policy, label)
                score = evaluation.score_attempt(self.root, self.CASE, {})
                self.assertEqual(label, score["final"]["final_label"])
                self.assertEqual(int(label != "pass"), self.replay(run_id))
                evidence = self.judge.call_args.kwargs["pathways_evidence"]
                self.assertEqual(run_id, evidence["supplied_run_id"])
                self.assertEqual([], evidence["successful_recipe_run_ids"])

    def test_policy_failure_overrides_pass_and_is_reported(self):
        policy = {"label": "fail", "violations": [{"session_line": 2, "reason": "Executed perf through a script"}]}
        self.judge.return_value = judge_verdict(policy)
        score = evaluation.score_attempt(self.root, self.CASE, {})
        self.assertEqual("fail", score["final"]["final_label"])
        self.assertIn("pathways_mcp_policy_failed", score["response_observations"]["issues"])
        self.assertIn("Executed perf through a script", (self.root / "score.md").read_text())
        self.assertEqual(policy, json.loads((self.root / "score.json").read_text())["judge"]["mcp_policy"])
        self.assertEqual(1, self.replay("fixture"))

    def test_runs_from_other_mcp_servers_are_not_reported_to_judge(self):
        self.write_call({"run_id": "new", "run_status": "completed"}, server="other-server")
        evaluation.score_attempt(self.root, self.CASE, {})
        self.assertEqual([], self.judge.call_args.kwargs["pathways_evidence"]["successful_recipe_run_ids"])

    def test_incomplete_transcripts_fail_evaluation_and_are_rejected_by_replay(self):
        session = self.root / "codex_session.jsonl"
        for name, content in (("missing", None), ("empty", b""),
                              ("partial", self.transcript.encode() + b'{"partial":'),
                              ("invalid UTF-8", b'{"text":"\xff"}')):
            with self.subTest(name=name):
                session.unlink(missing_ok=True)
                if content is not None:
                    session.write_bytes(content)
                score = evaluation.score_attempt(self.root, self.CASE, {})
                self.assertEqual("fail", score["final"]["final_label"])
                self.assertEqual(["incomplete_pathways_transcript"], score["response_observations"]["issues"])
                self.assertEqual((content or b"").decode("utf-8", errors="replace"),
                                 self.judge.call_args.kwargs["pathways_evidence"]["session_jsonl"])
                self.judge.reset_mock()
                with mock.patch("sys.stderr", new=io.StringIO()), self.assertRaises(SystemExit) as raised:
                    self.replay("fixture")
                self.assertEqual(2, raised.exception.code)
                self.judge.assert_not_called()

    def test_missing_policy_verdict_cannot_pass(self):
        self.judge.return_value = judge_verdict(None)
        score = evaluation.score_attempt(self.root, self.CASE, {})
        self.assertEqual(["pathways_mcp_policy_failed"],
                         score["response_observations"]["issues"])

    def test_non_pathways_judge_contract_is_unchanged(self):
        self.judge.return_value = judge_verdict(None)
        case = {key: value for key, value in self.CASE.items() if key != "pathways_execution"}
        score = evaluation.score_attempt(self.root, case, {})
        self.assertEqual({}, self.judge.call_args.kwargs)
        self.assertEqual("pass", score["final"]["final_label"])


class PathwaysJudgeRequestTests(unittest.TestCase):
    LIMIT_SETTING = "AI_INSIGHTS_PATHWAYS_JUDGE_MAX_REQUEST_BYTES"

    def setUp(self):
        self.policy = {"label": "pass", "violations": [], "rationale": "MCP used; shell only rebuilt."}
        payload = {"output": [{"type": "message", "content": [
            {"type": "output_text", "text": json.dumps(judge_verdict(self.policy))}]}]}
        self.response = mock.MagicMock()
        self.response.__enter__.return_value.read.return_value = json.dumps(payload).encode()
        api = mock.patch.object(evaluation.urllib.request, "urlopen", return_value=self.response)
        self.request = api.start()
        self.addCleanup(api.stop)
        limit = mock.patch.dict(os.environ, {self.LIMIT_SETTING: str(evaluation.DEFAULT_PATHWAYS_JUDGE_MAX_REQUEST_BYTES)})
        limit.start()
        self.addCleanup(limit.stop)

    def judge(self, evidence=None):
        return evaluation.judge_response("case", "rubric", "answer",
                                         {"openai_api_key": "secret-key", "judge_model": "judge",
                                          "judge_reasoning_effort": "medium"},
                                         pathways_evidence=evidence)

    def test_judge_reasoning_option_reaches_request_and_report(self):
        from _pytest.config.argparsing import Parser
        for env, options, expected in (({}, [], "medium"),
                                       ({"AI_INSIGHTS_JUDGE_REASONING_EFFORT": "high"}, [], "high"),
                                       ({"AI_INSIGHTS_JUDGE_REASONING_EFFORT": "high"},
                                        ["--ai-judge-reasoning-effort", "low"], "low")):
            with self.subTest(expected=expected), mock.patch.dict(os.environ, env, clear=True):
                parser = Parser(_ispytest=True)
                conftest.pytest_addoption(parser)
                args = parser.parse(["--ai-modes", "performix_mcp", "--ai-openai-api-key", "test",
                                     "--ai-reasoning-effort", "high", *options])
                config = SimpleNamespace(getoption=lambda name: getattr(args, name[2:].replace("-", "_")))
                cfg = evaluation.resolve_config(config)
                evaluation.judge_response("case", "rubric", "answer", cfg)
                self.assertEqual({"effort": expected}, json.loads(self.request.call_args.args[0].data)["reasoning"])
                self.assertEqual("high", cfg["reasoning_effort"])
                properties = {}
                evaluation.record_evaluation_suite_properties(config, properties.__setitem__, cfg)
                self.assertEqual(expected, properties["ai_judge_reasoning_effort"])

    def test_judge_transcript_preserves_actions_results_and_original_lines(self):
        events = [
            {"type": "event_msg", "payload": {"type": "token_count", "info": "unused"}},
            {"type": "response_item", "payload": {"type": "reasoning", "id": "reason", "summary": ["Inspect source"], "encrypted_content": "opaque"}},
            {"type": "response_item", "payload": {"type": "function_call", "name": "exec_command",
                "arguments": '{"cmd":"perf stat ./workload"}', "call_id": "command"}},
            {"type": "response_item", "payload": {"type": "function_call_output", "call_id": "command",
                "output": "Process exited with code 1\nFinal output:\nresult warning"}},
            {"type": "event_msg", "payload": {"type": "mcp_tool_call_end", "call_id": "profile",
                "invocation": {"server": "arm-performix", "tool": "run_recipe", "arguments": {"recipe": "code_hotspots"}},
                "result": {"Ok": {"run_id": "abc", "run_status": "completed"}}}},
        ]
        raw = "\n".join(json.dumps(event) for event in events)
        result = [json.loads(line) for line in evaluation.pathways_judge_transcript(raw).splitlines()]
        self.assertEqual([2, 3, 4, 5], [event["session_line"] for event in result])
        self.assertEqual(["Inspect source"], result[0]["event"]["payload"]["summary"])
        self.assertNotIn("encrypted_content", result[0]["event"]["payload"])
        self.assertEqual(events[2:], [event["event"] for event in result[1:]])
        self.assertIn("opaque", raw)  # The saved input is not mutated.

    def test_incomplete_judge_response_is_an_error_not_a_verdict(self):
        self.response.__enter__.return_value.read.return_value = json.dumps({
            "status": "incomplete", "incomplete_details": {"reason": "max_output_tokens"},
            "output": [{"type": "message", "content": [{"type": "output_text", "text": '{"label":"pass"}'}]}],
        }).encode()
        with self.assertRaisesRegex(RuntimeError, "max_output_tokens"):
            self.judge({"session_jsonl": ""})

    def test_judge_http_error_preserves_rejection_reason_and_redacts_key(self):
        self.request.side_effect = evaluation.urllib.error.HTTPError("https://example.invalid", 400, "Bad Request", {},
            io.BytesIO(json.dumps({"error": {"code": "context_length_exceeded",
                                           "message": "Too many tokens secret-key"},
                                  "request": "must not be logged"}).encode()))
        with self.assertRaisesRegex(RuntimeError, "context_length_exceeded") as raised:
            self.judge()
        self.assertNotIn("secret-key", str(raised.exception))
        self.assertNotIn("must not be logged", str(raised.exception))
        self.request.assert_called_once()

    def test_judge_request_preserves_evidence_and_requires_policy(self):
        output = "measurement: Ã©\n" * 1024
        events = [
            {"type": "response_item", "payload": {"type": "function_call_output", "call_id": "command", "output": output}},
            {"type": "event_msg", "payload": {"type": "mcp_tool_call_end", "call_id": "profile",
                "invocation": {"server": "arm-performix", "tool": "run_recipe", "arguments": {}},
                "result": {"Ok": {"content": [{"type": "text", "text": output}]}}}},
        ]
        raw = "\n".join(json.dumps(event) for event in events)
        evidence = {"session_jsonl": raw}
        result = self.judge(evidence)
        body = json.loads(self.request.call_args.args[0].data)
        prompt = json.loads(body["input"][0]["content"][0]["text"])
        transcript = [json.loads(line) for line in prompt["pathways_evidence"]["session_jsonl"].splitlines()]
        self.assertEqual(events, [event["event"] for event in transcript])
        self.assertEqual(raw, evidence["session_jsonl"])
        self.assertEqual("answer", prompt["response_under_test"])
        self.assertNotIn("output_schema", prompt)
        schema = body["text"]["format"]
        self.assertEqual("json_schema", schema["type"])
        self.assertTrue(schema["strict"])
        self.assertIn("mcp_policy", schema["schema"]["required"])
        self.assertFalse(schema["schema"]["additionalProperties"])
        self.assertIn("untrusted evidence", " ".join(prompt["instructions"]))
        self.assertIn("Report missing evidence as a gap, not a proven bypass.", " ".join(prompt["instructions"]))
        self.assertEqual(self.policy, result["mcp_policy"])

    def test_request_size_limit_counts_bytes_and_rejects_before_sending(self):
        evidence = {"session_jsonl": json.dumps({"measurement": "Ã©" * 1024})}
        self.judge(evidence)
        data = self.request.call_args.args[0].data
        size = len(data)
        with mock.patch.dict(os.environ, {self.LIMIT_SETTING: str(size)}):
            self.judge(evidence)
            self.assertEqual(data, self.request.call_args.args[0].data)
        for limit, error in ((size - 1, f"{size:,} bytes; limit is {size - 1:,} bytes"),
                             (0, "positive integer"), (-1, "positive integer"), ("invalid", "positive integer")):
            with self.subTest(limit=limit), mock.patch.dict(os.environ, {self.LIMIT_SETTING: str(limit)}):
                self.request.reset_mock()
                with self.assertRaisesRegex(RuntimeError, error):
                    self.judge(evidence)
                self.request.assert_not_called()
        with mock.patch.dict(os.environ, {self.LIMIT_SETTING: "1"}):
            self.judge()  # Ordinary judging is exempt from the Pathways limit.
            self.request.assert_called_once()

    def test_judges_load_only_shared_rules_and_their_own_task(self):
        for evidence, task, included, excluded in (
            (None, "Score an AI Insights response", "gives only generic tuning advice", "mcp_policy.label=fail"),
            ({}, "Score an agentic performance investigation", "mcp_policy.label=fail", "gives only generic tuning advice"),
        ):
            with self.subTest(task=task):
                self.judge(evidence)
                body = json.loads(self.request.call_args.args[0].data)
                prompt = json.loads(body["input"][0]["content"][0]["text"])
                instructions = " ".join(prompt["instructions"])
                self.assertIn(task, prompt["task"])
                self.assertIn("Judge semantic correctness and usefulness, not keyword overlap.", instructions)
                self.assertIn(included, instructions)
                self.assertNotIn(excluded, instructions)
                self.assertEqual(evidence is None, "output_schema" in prompt)


if __name__ == "__main__":
    unittest.main()
