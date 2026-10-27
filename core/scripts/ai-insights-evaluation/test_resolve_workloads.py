# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Contract tests for the AI Insights pre-record workflow matrix resolver."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import yaml


SCRIPT = Path(__file__).with_name("resolve-workloads.py")
MANIFEST = Path(__file__).with_name("ai_insights_evaluation.json")
WORKFLOW = Path(__file__).resolve().parents[3] / ".github/workflows/ai-insights-prerecord-run.yaml"
CORE_WORKFLOW = Path(__file__).resolve().parents[3] / ".github/workflows/prerecord-run-core.yaml"


def load_workflow(path: Path) -> dict:
    # BaseLoader keeps GitHub's "on" key as a string instead of a YAML 1.1 boolean.
    return yaml.load(path.read_text(encoding="utf-8"), Loader=yaml.BaseLoader)


class ResolveWorkloadsTests(unittest.TestCase):
    def run_resolver(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(SCRIPT), *args],
            check=False,
            capture_output=True,
            text=True,
        )

    def test_act_selection_returns_mixed_recipe_matrix_rows(self) -> None:
        result = self.run_resolver("--act", "act1")

        self.assertEqual(result.returncode, 0, result.stderr)
        matrix = json.loads(result.stdout)
        self.assertTrue(matrix)
        self.assertEqual(
            {row["recipe"] for row in matrix},
            {
                "asct",
                "cache_sharing",
                "code_hotspots",
                "cpu_microarchitecture",
                "instruction_mix",
                "memory_access",
                "syscall_trace_summary",
                "system_utilization",
            },
        )
        self.assertTrue(
            all(
                {
                    "id",
                    "workload",
                    "recipe",
                    "instance_type",
                    "recipe_params",
                    "prerecord",
                }
                <= set(row)
                for row in matrix
            )
        )
        self.assertEqual(
            {row["instance_type"] for row in matrix},
            {"m8g.xlarge", "c8g.metal-24xl"},
        )

    def test_testcase_selection_overrides_act(self) -> None:
        result = self.run_resolver(
            "--act", "act1", "--testcase", "test_case_36"
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(result.stdout),
            [
                {
                    "id": "test_case_36",
                    "workload": "ai_insights_tests/test_case_36",
                    "recipe": "instruction_mix",
                    "instance_type": "m8g.xlarge",
                    "recipe_params": ["mode=both"],
                    "prerecord": {"mode": "launch"},
                }
            ],
        )

    def test_java_jfr_cases_resolve_capture_configuration(self) -> None:
        result = self.run_resolver(
            "--testcase", "test_case_20,test_case_71,test_case_72,test_case_73"
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        matrix = json.loads(result.stdout)
        self.assertEqual({row["id"] for row in matrix}, {
            "test_case_20", "test_case_71", "test_case_72", "test_case_73"
        })
        for row in matrix:
            self.assertEqual(row["recipe"], "code_hotspots")
            self.assertEqual(row["recipe_params"], ["collect_java_stacks=true"])
            self.assertEqual(row["prerecord"]["mode"], "launch")
            self.assertIs(row["prerecord"]["enable_jfr_capture"], True)
            if row["id"] != "test_case_20":
                self.assertEqual(row["prerecord"]["timeout_seconds"], 60)

    def test_cpu_microarchitecture_uses_manifest_target(self) -> None:
        result = self.run_resolver(
            "--testcase",
            "test_case_46",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(result.stdout),
            [
                {
                    "id": "test_case_46",
                    "workload": "ai_insights_tests/test_case_46",
                    "recipe": "cpu_microarchitecture",
                    "instance_type": "c8g.metal-24xl",
                    "recipe_params": ["sampling_freq=normal"],
                    "prerecord": {"mode": "launch"},
                }
            ],
        )

    def test_asct_uses_system_wide_execution(self) -> None:
        result = self.run_resolver("--testcase", "test_case_53")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(result.stdout),
            [
                {
                    "id": "test_case_53",
                    "workload": "",
                    "recipe": "asct",
                    "instance_type": "c8g.metal-24xl",
                    "recipe_params": ["default_benchmarks=true"],
                    "prerecord": {"mode": "system-wide"},
                }
            ],
        )

    def test_memory_access_cases_use_spe_target_setup(self) -> None:
        result = self.run_resolver(
            "--testcase",
            "test_case_67,test_case_68,test_case_69,test_case_70",
            "--group-by-instance-type",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            json.loads(result.stdout),
            [
                {
                    "instance_type": "c8g.metal-24xl",
                    "cases": [
                        {
                            "id": f"test_case_{case}",
                            "workload": f"ai_insights_tests/test_case_{case}",
                            "recipe": "memory_access",
                            "instance_type": "c8g.metal-24xl",
                            "recipe_params": [],
                            "prerecord": {"mode": "launch"},
                        }
                        for case in range(67, 71)
                    ],
                    "enable_spe": True,
                }
            ],
        )

    def test_cases_are_grouped_by_manifest_target(self) -> None:
        result = self.run_resolver(
            "--act", "act2", "--group-by-instance-type"
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        groups = json.loads(result.stdout)
        self.assertEqual(
            [group["instance_type"] for group in groups],
            ["m8g.xlarge", "c8g.metal-24xl"],
        )
        for group in groups:
            self.assertTrue(group["cases"])
            self.assertTrue(
                all(
                    row["instance_type"] == group["instance_type"]
                    for row in group["cases"]
                )
            )

    def test_robot_catalog_resolves_reviewed_pre_recorded_run_contracts(self) -> None:
        result = self.run_resolver(
            "--catalog", "robot", "--act", "robot", "--group-by-instance-type"
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        groups = json.loads(result.stdout)
        groups_by_target = {(group["platform"], group["arch"]): group for group in groups}
        expected = {
            ("linux", "aarch64"): ("c8g.metal-24xl", "Ubuntu 24.04 (arm64)", 9),
            ("linux", "x86-64"): ("c6a.metal", "Ubuntu 22.04 (amd64)", 4),
            ("windows", "aarch64"): ("cobalt-100", "windows-11-pro-arm", 2),
        }
        self.assertEqual(set(groups_by_target), set(expected))
        self.assertEqual(
            {case["logical_id"] for group in groups for case in group["cases"]},
            {
                "code-hotspots-baseline",
                "cpu-microarchitecture-baseline",
                "instruction-mix-dynamic-baseline",
                "instruction-mix-static-baseline",
                "system-utilization-baseline",
                "time-filter-code-hotspots",
                "time-filter-cpu-microarchitecture",
                "time-filter-instruction-mix-dynamic",
                "rerender-parameters",
            },
        )
        self.assertTrue(all(not group["enable_spe"] for group in groups))
        for (platform, arch), group in groups_by_target.items():
            instance, image, count = expected[(platform, arch)]
            self.assertEqual((group["instance_type"], group["instance_os"]), (instance, image))
            self.assertEqual(len(group["cases"]), count)
            for case in group["cases"]:
                self.assertEqual(case["id"], f"{case['logical_id']}-{platform}-{arch}")
                self.assertEqual((case["platform"], case["arch"]), (platform, arch))
                self.assertEqual(case["workload"], "")
                self.assertEqual(case["workload_command"],
                    "python -c 10**10**10" if platform == "windows" else
                    "dd if=/dev/zero of=/dev/null bs=1M count=102400")
                self.assertEqual(case["prerecord"], {"mode": "launch", "timeout_seconds": 10})
        cases = {case["logical_id"]: case for case in groups_by_target[("linux", "aarch64")]["cases"]}
        self.assertEqual(
            cases["rerender-parameters"]["recipe"],
            "core/robot/resources/files/recipes/render_params_recipe.js",
        )
        system_utilization = cases["system-utilization-baseline"]
        self.assertEqual(
            (
                system_utilization["recipe"],
                system_utilization["recipe_params"],
                system_utilization["prerecord"],
            ),
            (
                "system_utilization",
                ["interval=0.1"],
                {"mode": "launch", "timeout_seconds": 10},
            ),
        )
        self.assertEqual(
            cases["time-filter-instruction-mix-dynamic"]["recipe_params"],
            ["rich_data_capture=true", "mode=dynamic", "sampling_freq=high"],
        )

    def test_robot_catalog_is_not_part_of_ai_selection(self) -> None:
        result = self.run_resolver("--act", "robot")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no matching pre-recorded runs", result.stderr)

    def test_robot_catalog_rejects_unknown_and_duplicate_pre_recorded_run_ids(self) -> None:
        unknown = self.run_resolver(
            "--catalog", "robot", "--testcase", "not-a-pre-recorded-run"
        )

        self.assertNotEqual(unknown.returncode, 0)
        self.assertIn("unknown pre-recorded run ID(s)", unknown.stderr)

        manifest = {
            "defaults": {"recipe": "code_hotspots", "prerecord": {"mode": "launch"}},
            "tests": [
                {"id": "duplicate", "workload": "megabench"},
                {"id": "duplicate", "workload": "megabench"},
            ],
        }
        with tempfile.TemporaryDirectory() as directory:
            manifest_path = Path(directory) / "robot.json"
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            duplicate = self.run_resolver(
                "--catalog",
                "robot",
                "--manifest",
                str(manifest_path),
                "--testcase",
                "duplicate",
            )

        self.assertNotEqual(duplicate.returncode, 0)
        self.assertIn("testcase ID is not unique", duplicate.stderr)

    def test_custom_recipe_is_restricted_to_robot_catalog(self) -> None:
        manifest = {
            "defaults": {"prerecord": {"mode": "launch"}},
            "tests": [
                {
                    "id": "rerender-parameters-linux-aarch64",
                    "logical_id": "rerender-parameters",
                    "platform": "linux",
                    "arch": "aarch64",
                    "instance_os": "Ubuntu 24.04 (arm64)",
                    "workload": "test-workload",
                    "recipe": "core/robot/resources/files/recipes/render_params_recipe.js",
                }
            ],
        }
        with tempfile.TemporaryDirectory() as directory:
            manifest_path = Path(directory) / "catalog.json"
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            ai_result = self.run_resolver(
                "--manifest", str(manifest_path), "--testcase", "rerender-parameters-linux-aarch64"
            )
            robot_result = self.run_resolver(
                "--catalog",
                "robot",
                "--manifest",
                str(manifest_path),
                "--testcase",
                "rerender-parameters-linux-aarch64",
            )

        self.assertNotEqual(ai_result.returncode, 0)
        self.assertIn("unknown or unsupported recipe type", ai_result.stderr)
        self.assertEqual(robot_result.returncode, 0, robot_result.stderr)

    def test_workflow_forwards_group_target_and_dry_run(self) -> None:
        job = load_workflow(WORKFLOW)["jobs"]["prerecord"]

        self.assertEqual(job["needs"], "prepare-cases")
        self.assertEqual(job["uses"], "./.github/workflows/prerecord-run-core.yaml")
        self.assertEqual(
            job["strategy"]["matrix"]["group"],
            "${{ fromJson(needs.prepare-cases.outputs.groups) }}",
        )
        expected_inputs = {
            "cases": "${{ toJson(matrix.group.cases) }}",
            "platform": "${{ matrix.group.platform }}",
            "instance_type": "${{ matrix.group.instance_type }}",
            "instance_os": "${{ matrix.group.instance_os || inputs.instance_os }}",
            "enable_spe": "${{ matrix.group.enable_spe }}",
            "dry_run": "${{ inputs.dry_run }}",
            "artifactory_run_base": "${{ needs.prepare-cases.outputs.artifactory_run_base }}",
        }
        for name, expected in expected_inputs.items():
            with self.subTest(input=name):
                self.assertEqual(job["with"][name], expected)

    def test_workflow_offers_robot_catalog_and_exposes_resolved_outputs(self) -> None:
        workflow = load_workflow(WORKFLOW)
        inputs = workflow["on"]["workflow_dispatch"]["inputs"]
        self.assertIn("robot", inputs["act_or_test"]["options"])
        outputs = workflow["jobs"]["prepare-cases"]["outputs"]
        for name in ("groups", "artifactory_run_base"):
            with self.subTest(output=name):
                self.assertEqual(outputs[name], f"${{{{ steps.resolve.outputs.{name} }}}}")

    def test_platform_jobs_use_expected_dependencies_and_forward_workload(self) -> None:
        jobs = load_workflow(CORE_WORKFLOW)["jobs"]
        self.assertEqual(jobs["provision-linux"]["needs"], "validate-inputs")
        self.assertEqual(
            jobs["provision-linux"]["if"],
            "${{ inputs.platform == '' || inputs.platform == 'linux' }}",
        )
        self.assertEqual(jobs["provision-windows"]["needs"], "validate-inputs")
        self.assertEqual(jobs["provision-windows"]["if"], "${{ inputs.platform == 'windows' }}")
        for platform, workload in (
            ("linux", "${{ matrix.case.workload_command || steps.build_workload.outputs.workload_cmd }}"),
            ("windows", "${{ matrix.case.workload_command }}"),
        ):
            with self.subTest(platform=platform):
                recorder = jobs[f"prerecord-{platform}"]
                self.assertCountEqual(recorder["needs"], ["validate-inputs", f"provision-{platform}"])
                self.assertEqual(recorder["strategy"]["matrix"]["case"], "${{ fromJson(inputs.cases) }}")
                capture = next(step for step in recorder["steps"] if "WORKLOAD_CMD" in step.get("env", {}))
                self.assertEqual(capture["env"]["WORKLOAD_CMD"], workload)
                self.assertEqual(capture["env"]["DRY_RUN"], "${{ inputs.dry_run }}")
                self.assertCountEqual(
                    jobs[f"cleanup-{platform}"]["needs"],
                    [f"provision-{platform}", f"prerecord-{platform}"],
                )

    def test_cpu_microarchitecture_uses_default_collect_all(self) -> None:
        manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
        cases = [
            test
            for test in manifest["tests"]
            if test.get("recipe") == "cpu_microarchitecture"
        ]

        self.assertEqual(len(cases), 7)
        self.assertEqual(manifest["defaults"]["instance_type"], "m8g.xlarge")
        for test in cases:
            self.assertEqual(test["instance_type"], "c8g.metal-24xl")
            params = test["recipe_params"]
            self.assertIn("sampling_freq=normal", params)
            self.assertFalse(
                any(param.startswith("metrics_group=") for param in params)
            )
            self.assertFalse(
                any(param.startswith("collect_all=") for param in params)
            )

    def test_run_modification_is_forwarded_in_prerecord_config(self) -> None:
        manifest = {
            "defaults": {
                "instance_type": "m8g.xlarge",
                "prerecord": {"mode": "launch"},
            },
            "tests": [
                {
                    "id": "test_case_59",
                    "recipe": "asct",
                    "run_modification": "low_peak_bandwidth",
                }
            ],
        }
        with tempfile.TemporaryDirectory() as directory:
            manifest_path = Path(directory) / "manifest.json"
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            result = self.run_resolver(
                "--manifest", str(manifest_path), "--testcase", "test_case_59"
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        case = json.loads(result.stdout)[0]
        self.assertEqual(
            case["prerecord"],
            {"mode": "launch", "run_modification": "low_peak_bandwidth"},
        )

    def test_missing_or_unknown_selection_is_rejected(self) -> None:
        for args, message in (
            ((), "--act is required"),
            (("--act", "act9"), "no matching"),
            (("--testcase", "test_case_99"), "unknown pre-recorded run ID"),
        ):
            with self.subTest(args=args):
                result = self.run_resolver(*args)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)


if __name__ == "__main__":
    unittest.main()
