#!/usr/bin/env python

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Usage: python3 update_matrix.py <compatibility_matrix.json> <new_version>

This script updates the compatibility matrix by replacing "NEXT_VERSION" placeholders
with the specified new version. The format of the new version must a valid semantic version
(MAJOR.MINOR.PATCH).
"""

import json
import re
import sys
from pathlib import Path

SEMVER_REGEX = r"^\d+\.\d+\.\d+$"
VERSION_KEY = "version"
MATRIX_KEY = "compatibility"
PLACEHOLDER = "NEXT_VERSION"

def validate_version_format(version: str):
    if not re.match(SEMVER_REGEX, version):
        raise ValueError(f"Version '{version}' is not a valid semantic version (MAJOR.MINOR.PATCH).")

def check_version_does_not_exist(matrix: dict, version: str):
    for recipe_name, version_map in matrix.items():
        if version in version_map:
            raise ValueError(f"Version '{version}' already exists in recipe '{recipe_name}'.")

def rollover(filename: str, new_version: str):
    path = Path(filename)
    data = json.loads(path.read_text())

    # 1) Top-level checks
    if MATRIX_KEY not in data or VERSION_KEY not in data:
        raise ValueError(
            f"{filename} must contain both '{MATRIX_KEY}' and '{VERSION_KEY}' keys at the top level."
        )

    validate_version_format(new_version)
    matrix = data[MATRIX_KEY]
    check_version_does_not_exist(matrix, new_version)

    # 2) Validate shape and gather prior NEXT_VERSION
    prior_mins = {}
    for recipe, vmap in matrix.items():
        if "NEXT_VERSION" not in vmap:
            raise ValueError(f'Recipe "{recipe}" is missing a "NEXT_VERSION" entry.')

        nx = vmap["NEXT_VERSION"]
        if not isinstance(nx, dict) or "minimum_version" not in nx:
            raise ValueError(
                f'Recipe "{recipe}" NEXT_VERSION must be an object with "minimum_version". Got: {nx!r}'
            )

        prior = nx["minimum_version"]
        # Allow placeholder or real semver here
        if prior != PLACEHOLDER and not re.match(SEMVER_REGEX, prior):
            raise ValueError(
                f'Recipe "{recipe}" has invalid NEXT_VERSION.minimum_version {prior!r}'
            )
        prior_mins[recipe] = prior

    # 3) Apply rollout
    for recipe, vmap in matrix.items():
        planned = prior_mins[recipe]
        # Determine the new entry’s minimum_version:
        # - If placeholder, use new_version (breaking change introduced)
        # - Otherwise, carry forward the prior semver
        effective_min = new_version if planned == PLACEHOLDER else planned

        # 3a) Add the new version entry
        vmap[new_version] = {"minimum_version": effective_min}

        # 3b) Bump NEXT_VERSION only when it was the placeholder
        if planned == PLACEHOLDER:
            vmap["NEXT_VERSION"]["minimum_version"] = new_version

    # 4) Post‐conditions
    for recipe, vmap in matrix.items():
        entry = vmap[new_version]
        expected = new_version if prior_mins[recipe] == PLACEHOLDER else prior_mins[recipe]
        if entry.get("minimum_version") != expected:
            raise AssertionError(
                f'Recipe "{recipe}": expected {new_version}.minimum_version == {expected!r}, '
                f'but got {entry.get("minimum_version")!r}'
            )
        # Check NEXT_VERSION bump for placeholders
        if prior_mins[recipe] == PLACEHOLDER:
            if vmap["NEXT_VERSION"]["minimum_version"] != new_version:
                raise AssertionError(
                    f'Recipe "{recipe}": NEXT_VERSION.minimum_version should be {new_version!r}'
                )

    # 5) Write back
    path.write_text(json.dumps(data, indent=2, sort_keys=True))
    print(f"Updated {filename} for new release version {new_version}")

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print("Usage: python3 update_matrix.py <compatibility_matrix.json> <new_version>")
        sys.exit(1)

    filename = sys.argv[1]
    new_version = sys.argv[2]

    try:
        rollover(filename, new_version)
    except Exception as e:
        print(str(e))
        sys.exit(1)
