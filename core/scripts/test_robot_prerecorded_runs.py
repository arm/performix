# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for selecting pre-recorded runs and checking their metadata."""

import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from lib import robot_prerecorded_runs as prerecorded_runs


class PreRecordedRunSelectionTests(unittest.TestCase):
    def test_resolution_uses_capture_architecture(self):
        for os_name, arch in (("Linux", "aarch64"), ("Linux", "x86-64"), ("Windows", "aarch64")):
            self.assertEqual(prerecorded_runs.resolve_robot_prerecorded_run("code-hotspots-baseline", os_name, arch),
                             f"code-hotspots-baseline-{os_name.lower()}-{arch}")

    def test_missing_or_unsupported_target_never_falls_back(self):
        for logical_id, os_name, arch in (
            ("code-hotspots-baseline", "Linux", None),
            ("code-hotspots-baseline", "Darwin", "aarch64"),
            ("code-hotspots-baseline", "Windows", "x86-64"),
            ("cpu-microarchitecture-baseline", "Linux", "x86-64"),
            ("system-utilization-baseline", "Windows", "aarch64"),
            ("time-filter-code-hotspots", "Windows", "aarch64"),
            ("missing", "Linux", "aarch64"),
        ):
            with self.subTest(logical_id=logical_id, os=os_name, arch=arch):
                with self.assertRaisesRegex(RuntimeError, "No pre-recorded"):
                    prerecorded_runs.resolve_robot_prerecorded_run(logical_id, os_name, arch)

    def test_suite_paths_accept_files_directories_and_absolute_paths(self):
        target = {"os": "Windows", "arch": "aarch64"}
        for suite in ("robot/tests/support", "robot/tests/support/support.robot",
                      "core/robot/tests/support", prerecorded_runs.MANIFEST.parents[2] / "robot/tests/support"):
            with self.subTest(suite=suite):
                self.assertEqual(prerecorded_runs.robot_prerecorded_run_ids(target, [suite]),
                                 ["code-hotspots-baseline-windows-aarch64"])

    def test_identity_rejects_unsafe_logical_id_even_when_physical_id_matches(self):
        case = {
            "id": "../escape-windows-aarch64",
            "logical_id": "../escape",
            "platform": "windows",
            "arch": "aarch64",
        }
        with self.assertRaisesRegex(ValueError, "Invalid Robot logical_id"):
            prerecorded_runs.validate_robot_case_identity(case)

    def test_rejects_duplicate_and_wrong_architecture_ids(self):
        entry = {"id": "code-hotspots-baseline-linux-aarch64", "logical_id": "code-hotspots-baseline",
                 "platform": "linux", "arch": "aarch64"}
        with TemporaryDirectory() as directory:
            manifest = Path(directory) / "catalog.json"
            for entries in ([entry, entry], [dict(entry, arch="x86-64")]):
                manifest.write_text(json.dumps({"tests": entries}), encoding="utf-8")
                with self.assertRaisesRegex(RuntimeError, "Invalid Robot pre-recorded run catalog"):
                    prerecorded_runs.robot_prerecorded_run_ids({"os": "Linux", "arch": "aarch64"}, manifest=manifest)


class MetadataTests(unittest.TestCase):
    def test_rejects_stale_target_or_workload_metadata(self):
        run_id = "code-hotspots-baseline-linux-x86-64"
        command = "dd if=/dev/zero of=/dev/null bs=1M count=102400"
        with TemporaryDirectory() as directory:
            path = Path(directory) / "metadata.json"
            for metadata in (
                {"testcase_id": "code-hotspots-baseline-linux-aarch64", "workload_command": command},
                {"testcase_id": run_id, "workload_command": "megabench 0 0"},
            ):
                path.write_text(json.dumps(metadata), encoding="utf-8")
                with self.assertRaisesRegex(RuntimeError, "does not match"):
                    prerecorded_runs.validate_robot_prerecorded_metadata(path, run_id, command)

            matching = {"testcase_id": run_id, "workload_command": command, "recipe": "code_hotspots",
                        "recipe_params": [], "profile_mode": "launch", "run_modification": None}
            path.write_text(json.dumps(matching), encoding="utf-8")
            prerecorded_runs.validate_robot_prerecorded_metadata(path, run_id, f'"{command}"')
            with self.assertRaisesRegex(RuntimeError, "live-recipe-runs"):
                prerecorded_runs.validate_robot_prerecorded_metadata(path, run_id, "custom-workload")
            for change in ({"recipe": "system_utilization"}, {"recipe_params": ["sampling_freq=high"]},
                           {"profile_mode": "attach"}, {"run_modification": "low_peak_bandwidth"}):
                path.write_text(json.dumps(dict(matching, **change)), encoding="utf-8")
                with self.assertRaisesRegex(RuntimeError, "recipe configuration does not match"):
                    prerecorded_runs.validate_robot_prerecorded_metadata(path, run_id, command)


if __name__ == "__main__":
    unittest.main()
