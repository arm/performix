# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import unittest
from pathlib import Path

MODULE_DIR = Path(__file__).resolve().parent
RESOLVER_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_resolve_workloads",
    MODULE_DIR / "ai-insights-evaluation" / "resolve-workloads.py",
)
resolver = importlib.util.module_from_spec(RESOLVER_SPEC)
RESOLVER_SPEC.loader.exec_module(resolver)


class ParseTestcaseIdsTests(unittest.TestCase):
    def test_splits_ids_and_trims_surrounding_whitespace(self):
        self.assertEqual(
            resolver.parse_testcase_ids(" test_case_21, test_case_27 "),
            ["test_case_21", "test_case_27"],
        )

    def test_rejects_whitespace_within_id(self):
        with self.assertRaisesRegex(ValueError, "must not contain whitespace"):
            resolver.parse_testcase_ids("test_case_2 4")

    def test_rejects_empty_id(self):
        for value in (",test_case_21", "test_case_21,", "test_case_21,,test_case_27"):
            with self.subTest(value=value), self.assertRaisesRegex(
                ValueError, "non-empty IDs"
            ):
                resolver.parse_testcase_ids(value)

    def test_rejects_duplicate_id(self):
        with self.assertRaisesRegex(ValueError, "must be unique"):
            resolver.parse_testcase_ids("test_case_21,test_case_21")


class ResolveWorkloadsTests(unittest.TestCase):
    def test_preserves_requested_order(self):
        tests = [{"id": "test_case_21"}, {"id": "test_case_27"}]

        selected = resolver.resolve_workloads(
            tests, "act3", ["test_case_27", "test_case_21"]
        )

        self.assertEqual(
            [test["id"] for test in selected], ["test_case_27", "test_case_21"]
        )

    def test_rejects_duplicate_manifest_id(self):
        tests = [{"id": "test_case_21"}, {"id": "test_case_21"}]

        with self.assertRaisesRegex(ValueError, "testcase ID is not unique"):
            resolver.resolve_workloads(tests, "act3", ["test_case_21"])


class ResolveCaseConfigTests(unittest.TestCase):
    def test_resolves_recipe_params_and_prerecord_defaults(self):
        case = resolver.resolve_case_config(
            {
                "id": "test_case_27",
                "recipe_params": ["collect_dotnet_stacks=true"],
                "prerecord": {"mode": "attach", "timeout_seconds": 25},
            },
            {
                "recipe": "code_hotspots",
                "prerecord": {"mode": "launch"},
            },
        )

        self.assertEqual(case["recipe"], "code_hotspots")
        self.assertEqual(case["workload"], "ai_insights_tests/test_case_27")
        self.assertEqual(case["recipe_params"], ["collect_dotnet_stacks=true"])
        self.assertEqual(case["prerecord"]["mode"], "attach")
        self.assertEqual(case["prerecord"]["timeout_seconds"], 25)

    def test_resolves_system_wide_case_without_workload(self):
        case = resolver.resolve_case_config(
            {"id": "test_case_48", "prerecord": {"mode": "system-wide"}},
            {"recipe": "system_utilization"},
        )

        self.assertEqual(case["prerecord"]["mode"], "system-wide")
        self.assertEqual(case["workload"], "")

    def test_rejects_system_wide_lifecycle_commands(self):
        with self.assertRaisesRegex(ValueError, "system-wide prerecord cannot use"):
            resolver.resolve_case_config(
                {
                    "id": "test_case_48",
                    "prerecord": {
                        "mode": "system-wide",
                        "setup": ["bash", "sidecar.sh", "setup"],
                        "cleanup": ["bash", "sidecar.sh", "cleanup"],
                    },
                },
                {"recipe": "system_utilization"},
            )

    def test_builds_manifest_free_generic_case(self):
        args = resolver.parser.parse_args(
            [
                "--case-id",
                "openssl-speed-regression",
                "--workload",
                "openssl-speed",
                "--recipe",
                "code_hotspots",
                "--recipe-params",
                "duration=60\nthreads=1",
            ]
        )

        case = resolver.generic_case(args)

        self.assertEqual(case["workload"], "openssl-speed")
        self.assertEqual(case["recipe"], "code_hotspots")
        self.assertEqual(case["recipe_params"], ["duration=60", "threads=1"])
        self.assertEqual(case["prerecord"], {"mode": "launch"})

    def test_rejects_manifest_recipe_override(self):
        args = resolver.parser.parse_args(["--recipe", "system_utilization"])
        case = {
            "id": "test_case_21",
            "workload": "ai_insights_tests/test_case_21",
            "recipe": "code_hotspots",
            "recipe_params": [],
            "prerecord": {"mode": "launch"},
        }

        with self.assertRaisesRegex(ValueError, "recipe mismatch"):
            resolver.apply_case_overrides(case, args)

    def test_rejects_manifest_recipe_params_override(self):
        args = resolver.parser.parse_args(
            ["--recipe-params", "mode=static"]
        )
        case = {
            "id": "test_case_36",
            "workload": "ai_insights_tests/test_case_36",
            "recipe": "instruction_mix",
            "recipe_params": ["mode=dynamic"],
            "prerecord": {"mode": "launch"},
        }

        with self.assertRaisesRegex(ValueError, "recipe params mismatch"):
            resolver.apply_case_overrides(case, args, manifest_case=True)


class ValidateCasesTests(unittest.TestCase):
    def test_accepts_resolved_cases(self):
        resolver.validate_cases(
            [
                {
                    "id": "test_case_21",
                    "workload": "ai_insights_tests/test_case_21",
                    "recipe": "code_hotspots",
                    "recipe_params": [],
                    "prerecord": {"mode": "launch"},
                }
            ]
        )

    def test_rejects_unsafe_case_id(self):
        with self.assertRaisesRegex(ValueError, "invalid prerecord case ID"):
            resolver.validate_cases(
                [
                    {
                        "id": "../test_case_21",
                        "workload": "ai_insights_tests/test_case_21",
                        "recipe": "code_hotspots",
                        "recipe_params": [],
                        "prerecord": {"mode": "launch"},
                    }
                ]
            )

    def test_rejects_duplicate_case_ids(self):
        case = {
            "id": "test_case_21",
            "workload": "ai_insights_tests/test_case_21",
            "recipe": "code_hotspots",
            "recipe_params": [],
            "prerecord": {"mode": "launch"},
        }

        with self.assertRaisesRegex(ValueError, "must be unique"):
            resolver.validate_cases([case, case])


if __name__ == "__main__":
    unittest.main()
