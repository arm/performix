#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Resolve prerecord cases for GitHub Actions matrices."""

import argparse
import json
import re
import sys
from pathlib import Path, PurePosixPath

sys.path.append(str(Path(__file__).resolve().parent))
from recipe_support import SPE_RECIPES, resolve_prerecord_config, resolve_recipe

MANIFEST = Path(__file__).with_name("ai_insights_evaluation.json")
SAFE_CASE_ID = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$")


parser = argparse.ArgumentParser()
parser.add_argument("--act", default="")
parser.add_argument("--manifest", type=Path, default=MANIFEST)
parser.add_argument("--case-id", default="")
parser.add_argument("--workload", default="")
parser.add_argument("--recipe", default="")
parser.add_argument("--workload-args", default="")
parser.add_argument("--recipe-params", default="")
parser.add_argument(
    "--testcase",
    default="",
    help="Optional comma-separated testcase ids. If set, this overrides --act.",
)
parser.add_argument(
    "--group-by-instance-type",
    action="store_true",
    help="Group resolved testcases for reusable workflow dispatch.",
)

def parse_testcase_ids(value: str) -> list[str]:
    """Parse and validate a comma-separated testcase selection."""

    if not value:
        return []

    testcase_ids = [testcase_id.strip() for testcase_id in value.split(",")]
    if any(not testcase_id for testcase_id in testcase_ids):
        raise ValueError("testcase must be a comma-separated list of non-empty IDs")
    if any(any(char.isspace() for char in testcase_id) for testcase_id in testcase_ids):
        raise ValueError("testcase IDs must not contain whitespace")
    if len(testcase_ids) != len(set(testcase_ids)):
        raise ValueError("testcase IDs must be unique")
    return testcase_ids


def resolve_workloads(
    tests: list[dict], act: str, testcase_ids: list[str]
) -> list[dict]:
    """Return prerecord fixture entries for the requested act or testcase IDs."""

    if not testcase_ids:
        if not act:
            raise ValueError("--act is required when --testcase is not set")
        selected = [test for test in tests if act in test.get("acts", [])]
        if not selected:
            raise ValueError("no matching AI Insights workloads")
    else:
        selected = []
        unknown_ids = []
        for testcase_id in testcase_ids:
            matches = [test for test in tests if test["id"] == testcase_id]
            if not matches:
                unknown_ids.append(testcase_id)
            elif len(matches) != 1:
                raise ValueError(f"testcase ID is not unique: {testcase_id}")
            else:
                selected.append(matches[0])

        if unknown_ids:
            raise ValueError(
                f"unknown AI Insights workload(s): {','.join(unknown_ids)}"
            )

    fixtures = []
    seen_fixture_ids = set()
    for test in selected:
        run_artifact = test.get("run_artifact")
        fixture = test
        if isinstance(run_artifact, str):
            fixture_id = PurePosixPath(run_artifact).parent.name
            matches = [
                candidate for candidate in tests if candidate["id"] == fixture_id
            ]
            if len(matches) != 1:
                raise ValueError(
                    f"run artifact owner for {test['id']} is not a unique testcase: "
                    f"{fixture_id!r}"
                )
            fixture = matches[0]
        if fixture["id"] not in seen_fixture_ids:
            fixtures.append(fixture)
            seen_fixture_ids.add(fixture["id"])
    return fixtures

def resolve_case_config(test: dict, defaults: dict) -> dict:
    """Resolve the generic prerecord inputs for one evaluation testcase."""

    recipe_params = test.get("recipe_params", [])
    if not isinstance(recipe_params, list) or not all(
        isinstance(param, str) for param in recipe_params
    ):
        raise ValueError(f"recipe_params for {test['id']} must be a list of strings")
    instance_type = test.get("instance_type", defaults.get("instance_type"))
    if instance_type is not None and (
        not isinstance(instance_type, str) or not instance_type
    ):
        raise ValueError(f"instance_type for {test['id']} must be a non-empty string")
    prerecord = resolve_prerecord_config(test, defaults)
    if "run_modification" in test:
        prerecord["run_modification"] = test["run_modification"]
    profile_mode = prerecord["mode"]
    if profile_mode == "system-wide" and any(
        prerecord.get(name) is not None for name in ("setup", "cleanup")
    ):
        raise ValueError("system-wide prerecord cannot use setup or cleanup lifecycle commands")
    case = {
        "id": test["id"],
        "workload": "" if profile_mode == "system-wide" else f"ai_insights_tests/{test['id']}",
        "recipe": resolve_recipe(test, defaults),
        "recipe_params": recipe_params,
        "prerecord": prerecord,
    }
    if instance_type is not None:
        case["instance_type"] = instance_type
    return case


def apply_case_overrides(
    case: dict, args: argparse.Namespace, *, manifest_case: bool = False
) -> dict:
    """Apply generic workflow inputs to one resolved case."""

    case = dict(case)
    if args.recipe and case["recipe"] != args.recipe:
        raise ValueError(
            f"recipe mismatch for {case['id']}: "
            f"manifest={case['recipe']}, argument={args.recipe}"
        )
    if args.workload:
        case["workload"] = args.workload
    if args.workload_args:
        case["workload_args"] = args.workload_args
    extra_params = [line.strip() for line in args.recipe_params.splitlines() if line.strip()]
    if manifest_case and extra_params:
        if case["recipe_params"] != extra_params:
            raise ValueError(
                f"recipe params mismatch for {case['id']}: "
                f"manifest={case['recipe_params']!r}, argument={extra_params!r}"
            )
    else:
        case["recipe_params"] = [*case["recipe_params"], *extra_params]
    return case


def generic_case(args: argparse.Namespace) -> dict:
    """Build a manifest-free launch case from explicit workflow inputs."""

    if not args.workload:
        raise ValueError("--workload is required with --case-id")
    if not args.recipe:
        raise ValueError("--recipe is required with --case-id")
    case = {
        "id": args.case_id,
        "workload": args.workload,
        "recipe": args.recipe,
        "recipe_params": [],
        "prerecord": {"mode": "launch"},
    }
    return apply_case_overrides(case, args)


def validate_cases(cases: list[dict]) -> None:
    """Validate the resolved values consumed by the reusable workflow."""

    if not cases:
        raise ValueError("no prerecord cases were resolved")
    case_ids = []
    for case in cases:
        case_id = case.get("id")
        if not isinstance(case_id, str) or not SAFE_CASE_ID.fullmatch(case_id):
            raise ValueError(f"invalid prerecord case ID: {case_id!r}")
        case_ids.append(case_id)
        if not isinstance(case.get("prerecord"), dict):
            raise ValueError(f"prerecord for {case_id} must be an object")
        prerecord = resolve_prerecord_config(case)
        profile_mode = prerecord["mode"]
        workload = case.get("workload")
        workload_required = profile_mode != "system-wide"
        if not isinstance(workload, str) or bool(workload) != workload_required:
            requirement = "non-empty" if workload_required else "empty"
            raise ValueError(
                f"workload for {case_id} must be {requirement} for the execution mode"
            )
        if not isinstance(case.get("recipe"), str) or not case["recipe"]:
            raise ValueError(f"recipe for {case_id} must be a non-empty string")
        if not isinstance(case.get("recipe_params"), list) or not all(
            isinstance(param, str) for param in case["recipe_params"]
        ):
            raise ValueError(f"recipe_params for {case_id} must be a list of strings")
    if len(case_ids) != len(set(case_ids)):
        raise ValueError("prerecord case IDs must be unique")


def main() -> int:
    """Resolve the requested cases and print a GitHub Actions matrix."""

    args = parser.parse_args()
    try:
        if args.case_id:
            if args.act or args.testcase:
                raise ValueError("--case-id cannot be combined with --act or --testcase")
            cases = [generic_case(args)]
        else:
            manifest = json.loads(args.manifest.read_text(encoding="utf-8"))
            selected = resolve_workloads(
                manifest["tests"], args.act, parse_testcase_ids(args.testcase)
            )
            cases = [
                apply_case_overrides(
                    resolve_case_config(test, manifest.get("defaults", {})),
                    args,
                    manifest_case=True,
                )
                for test in selected
            ]
        validate_cases(cases)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    if args.group_by_instance_type:
        grouped = {}
        for case in cases:
            instance_type = case.get("instance_type")
            if not instance_type:
                raise ValueError(
                    "--group-by-instance-type requires manifest cases with instance_type"
                )
            grouped.setdefault(instance_type, []).append(case)
        output = [
            {
                "instance_type": instance_type,
                "cases": grouped_cases,
                "enable_spe": any(case["recipe"] in SPE_RECIPES for case in grouped_cases),
            }
            for instance_type, grouped_cases in grouped.items()
        ]
    else:
        output = cases

    print(json.dumps(output, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
