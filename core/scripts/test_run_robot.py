#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Focused tests for the Robot launcher command."""

import importlib.util
import json
import os
import shlex
import subprocess
import sys
import tarfile
import unittest
from argparse import Namespace
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import Mock, patch


SPEC = importlib.util.spec_from_file_location(
    "run_robot", Path(__file__).with_name("run-robot.py")
)
run_robot = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(run_robot)


def make_args(**overrides):
    values = {
        "target": "test-target",
        "force_remote_localhost_setup": False,
        "prepare_workloads": None,
        "runner_os": "Linux",
        "run_remote_localhost": False,
        "exclude_tags": [],
        "results_dir": "robot/results",
        "include_tags": [],
        "workloads": None,
        "launch_workload": None,
        "live_recipe_runs": False,
        "dry_run": False,
        "fail_fast": False,
        "retry_failed_count": 0,
        "test_suite": [],
        "tests_dir": "robot/tests",
    }
    values.update(overrides)
    return Namespace(**values)


class BuildRobotCommandTests(unittest.TestCase):
    def test_defaults_to_prerecorded_runs(self):
        command, _ = run_robot.build_robot_command(make_args(), "test-target")

        self.assertNotIn("USE_PRERECORDED_RUNS:False", command)

    def test_live_mode_disables_prerecorded_runs_without_overriding_workload(self):
        command, _ = run_robot.build_robot_command(
            make_args(live_recipe_runs=True), "test-target"
        )

        self.assertIn("USE_PRERECORDED_RUNS:False", command)
        self.assertFalse(any(arg.startswith("LAUNCH_WORKLOAD:") for arg in command))

    def test_explicit_launch_workload_is_passed_to_robot(self):
        command, _ = run_robot.build_robot_command(
            make_args(live_recipe_runs=True, launch_workload="/tmp/custom-workload --flag"), "test-target"
        )

        self.assertIn('LAUNCH_WORKLOAD:"/tmp/custom-workload --flag"', command)

    def test_empty_launch_workload_remains_empty_for_robot_defaults(self):
        command, _ = run_robot.build_robot_command(
            make_args(live_recipe_runs=True, launch_workload=""), "test-target"
        )
        self.assertIn("LAUNCH_WORKLOAD:", command)

    def test_custom_workload_reaches_host_process_as_one_argument(self):
        workloads = [
            "/tmp/custom-workload --flag",
            '"/tmp/custom workload" --name "two words"',
            "tool --name O'Reilly",
            'tool --value "a&b" --other a|b',
            'tool --value %ROBOT_QUOTING_CHECK% --other $ROBOT_QUOTING_CHECK',
            'tool --value !ROBOT_QUOTING_CHECK! --other `echo unexpected`',
            'tool --path "C:\\workload files\\"',
        ]
        with TemporaryDirectory() as directory:
            probe = Path(directory) / "capture_args.py"
            probe.write_text("import json, sys; print(json.dumps(sys.argv[1:]))", encoding="utf-8")
            probe_args = [sys.executable, str(probe)]
            prefix = subprocess.list2cmdline(probe_args) if os.name == "nt" else shlex.join(probe_args)
            for workload in workloads:
                with self.subTest(workload=workload):
                    command, _ = run_robot.build_robot_command(
                        make_args(live_recipe_runs=True, launch_workload=workload), "test-target"
                    )
                    encoded = next(arg.removeprefix("LAUNCH_WORKLOAD:") for arg in command
                                   if arg.startswith("LAUNCH_WORKLOAD:"))
                    result = subprocess.run(
                        f"{prefix} --workload {encoded} --timeout 10",
                        shell=True, check=True, capture_output=True, text=True,
                        env={**os.environ, "ROBOT_QUOTING_CHECK": "must-not-expand"},
                    )
                    self.assertEqual(json.loads(result.stdout), ["--workload", workload, "--timeout", "10"])


class PreRecordedRunSelectionTests(unittest.TestCase):
    def test_selects_only_capture_target_runs(self):
        for os_name, arch, expected_count in (
            ("Linux", "aarch64", 9), ("Linux", "x86-64", 4), ("Windows", "aarch64", 2)
        ):
            with self.subTest(os=os_name, arch=arch):
                ids = run_robot.robot_prerecorded_run_ids({"os": os_name, "arch": arch})
                self.assertEqual(len(ids), expected_count)
                self.assertTrue(all(run_id.endswith(f"-{os_name.lower()}-{arch}") for run_id in ids))

    def test_empty_tag_lists_keep_all_selected_suite_recordings(self):
        target = {"os": "Linux", "arch": "aarch64"}
        suites = ["robot/tests/render", "robot/tests/run", "robot/tests/support"]
        ids = run_robot.robot_prerecorded_run_ids(
            target, suites, include_tags=[], exclude_tags=[]
        )
        self.assertEqual(ids, run_robot.robot_prerecorded_run_ids(target, suites))
        self.assertEqual(len(ids), 9)

    def test_capture_only_suites_need_no_prerecorded_runs(self):
        self.assertEqual(run_robot.robot_prerecorded_run_ids(
            {"os": "Linux", "arch": "aarch64"}, ["robot/tests/recipe/run.robot"]
        ), [])

    def test_suite_selection_avoids_unused_prerecorded_runs(self):
        self.assertEqual(run_robot.robot_prerecorded_run_ids(
            {"os": "Linux", "arch": "x86-64"}, ["robot/tests/run"]
        ), ["code-hotspots-baseline-linux-x86-64"])

    def test_tag_filtering_keeps_recordings_needed_by_suite_setup(self):
        # A selected test still runs the whole time-filter suite setup.
        ids = run_robot.robot_prerecorded_run_ids(
            {"os": "Linux", "arch": "aarch64"},
            ["robot/tests"],
            include_tags=["code-hotspotsANDtime-filter"],
        )
        self.assertEqual(ids, [
            "time-filter-code-hotspots-linux-aarch64",
            "time-filter-cpu-microarchitecture-linux-aarch64",
            "time-filter-instruction-mix-dynamic-linux-aarch64",
        ])

    def test_suite_paths_and_tag_filters_are_combined(self):
        target = {"os": "Linux", "arch": "aarch64"}
        for suites, includes, excludes, expected in (
            (["robot/tests"], ["run", "time-filter"], ["time-*"],
             ["code-hotspots-baseline-linux-aarch64"]),
            (["robot/tests/render"], ["run"], [], []),
            (["robot/tests"], ["no-matching-tests"], [], []),
            (["robot/tests"], ["remote-localhost", "run"], [],
             ["code-hotspots-baseline-linux-aarch64"]),
        ):
            with self.subTest(suites=suites, includes=includes, excludes=excludes):
                self.assertEqual(
                    run_robot.robot_prerecorded_run_ids(target, suites, includes, excludes),
                    expected,
                )


class PreRecordedRunDownloadTests(unittest.TestCase):
    def test_downloads_are_enabled_only_in_default_execution_mode(self):
        for mode, overrides, expected in (
            ("default", {}, True),
            ("live", {"live_recipe_runs": True}, False),
            ("dry run", {"dry_run": True}, False),
        ):
            with self.subTest(mode=mode):
                self.assertEqual(
                    run_robot.should_download_prerecorded_runs(make_args(**overrides)), expected
                )

    @patch.object(run_robot.requests, "get")
    def test_capture_only_run_needs_no_download_or_token(self, get):
        with patch.dict(os.environ, {}, clear=True):
            run_robot.download_missing_prerecorded_runs(
                {"os": "Linux", "arch": "aarch64"}, ["robot/tests/recipe"]
            )
        get.assert_not_called()

    @patch.object(run_robot.requests, "get")
    @patch.object(run_robot, "setup_remote_localhost")
    @patch.object(run_robot, "run", return_value=0)
    @patch.object(run_robot, "load_target_config", return_value={"os": "Linux", "arch": "aarch64"})
    @patch.object(run_robot, "parse_args")
    def test_remote_shard_downloads_nothing_without_changing_capture_mode(
        self, parse_args, _load_target, run, setup_remote, get
    ):
        parse_args.return_value = make_args(
            run_remote_localhost=True, include_tags=["remote-localhost"]
        )
        with patch.dict(os.environ, {}, clear=True), self.assertRaises(SystemExit) as result:
            run_robot.main()

        self.assertEqual(result.exception.code, 0)
        get.assert_not_called()
        setup_remote.assert_called_once()
        command = run.call_args.args[0]
        self.assertNotIn("USE_PRERECORDED_RUNS:False", command)
        self.assertIn("remote-localhost", command)

    @patch.object(run_robot, "robot_prerecorded_run_ids", return_value=["run-one"])
    @patch.object(run_robot.requests, "get")
    def test_downloads_only_missing_files_and_reuses_complete_cache(self, mock_get, _mock_run_ids):
        artifacts = {"latest.zip", "metadata.json"}
        mock_get.return_value = Mock(content=b"downloaded-input")
        for cached, token in (
            (set(), "test-token"),
            ({"latest.zip"}, "test-token"),
            ({"metadata.json"}, "test-token"),
            (artifacts, None),
        ):
            with self.subTest(cached=sorted(cached)), TemporaryDirectory() as directory:
                mock_get.reset_mock()
                runs_dir = Path(directory)
                run_dir = runs_dir / "run-one"
                run_dir.mkdir()
                for artifact in cached:
                    (run_dir / artifact).write_bytes(b"cached-input")
                environment = {"ARTIFACTORY_API_TOKEN": token} if token else {}
                with (
                    patch.object(run_robot, "ROBOT_PRERECORDED_RUNS_DIR", runs_dir),
                    patch.dict(os.environ, environment, clear=True),
                ):
                    run_robot.download_missing_prerecorded_runs({"os": "Linux"})

                requested = [call.args[0].rsplit("/", 1)[-1] for call in mock_get.call_args_list]
                self.assertCountEqual(requested, artifacts - cached)
                for artifact in artifacts:
                    expected = b"cached-input" if artifact in cached else b"downloaded-input"
                    self.assertEqual((run_dir / artifact).read_bytes(), expected, artifact)
                for call in mock_get.call_args_list:
                    self.assertEqual(call.kwargs["headers"], {"X-JFrog-Art-Api": token})

    @patch.object(run_robot, "robot_prerecorded_run_ids", return_value=["run-one"])
    def test_missing_token_fails_with_empty_cache(self, _mock_run_ids):
        with TemporaryDirectory() as temporary_directory:
            with (
                patch.object(run_robot, "ROBOT_PRERECORDED_RUNS_DIR", Path(temporary_directory)),
                patch.dict(os.environ, {}, clear=True),
                self.assertRaisesRegex(RuntimeError, "ARTIFACTORY_API_TOKEN"),
            ):
                run_robot.download_missing_prerecorded_runs({"os": "Linux"})


class RemoteLocalhostArchiveTests(unittest.TestCase):
    @patch.object(run_robot.subprocess, "run", return_value=Mock(returncode=0, stdout=""))
    @patch.object(run_robot, "RemoteHost")
    def test_archive_excludes_cached_recordings_but_keeps_remote_tools(self, remote_host, _run):
        with TemporaryDirectory() as directory:
            included = {
                "core/apap-cli/tools/neoprof-Linux-aarch64.tar.gz",
                "core/robot/resources/files/recipes/render_params_recipe.js",
            }
            excluded = {
                "core/robot/resources/files/prerecorded-runs/run-one/latest.zip",
                "core/robot/resources/files/prerecorded-runs/run-one/metadata.json",
                "core/robot/resources/files/export/run.zip",
                "core/apap-cli/tools/neoprof-Windows-aarch64.tar.gz",
            }
            for name in included | excluded:
                path = Path(directory) / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("test input", encoding="utf-8")

            def inspect_archive(archive, _remote_path):
                with tarfile.open(archive) as tar:
                    files = {member.name.removeprefix("./") for member in tar if member.isfile()}
                self.assertEqual(files, included)

            remote_host.return_value.scp.side_effect = inspect_archive
            with patch.object(run_robot, "REPO_ROOT", directory):
                run_robot.setup_remote_localhost(
                    {"os": "Linux", "arch": "aarch64", "key": "key", "port": 22,
                     "user": "user", "host": "host"},
                    force=True,
                )
            remote_host.return_value.scp.assert_called_once()


if __name__ == "__main__":
    unittest.main()
