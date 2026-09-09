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


SCRIPT = Path(__file__).with_name("resolve-workloads.py")
MANIFEST = Path(__file__).with_name("ai_insights_evaluation.json")
WORKFLOW = Path(__file__).resolve().parents[3] / ".github/workflows/ai-insights-prerecord-run.yaml"


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

    def test_workflow_forwards_group_target_and_dry_run(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")

        self.assertIn(
            "instance_type: ${{ matrix.group.instance_type }}",
            workflow,
        )
        self.assertIn(
            "group: ${{ fromJson(needs.prepare-cases.outputs.groups) }}",
            workflow,
        )
        self.assertIn("enable_spe: ${{ matrix.group.enable_spe }}", workflow)
        self.assertNotIn("inputs.instance_type", workflow)
        self.assertIn("dry_run: ${{ inputs.dry_run }}", workflow)

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
            (("--testcase", "test_case_99"), "unknown AI Insights workload"),
        ):
            with self.subTest(args=args):
                result = self.run_resolver(*args)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)


if __name__ == "__main__":
    unittest.main()
