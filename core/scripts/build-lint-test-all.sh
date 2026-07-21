#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script runs the build, lint and test scripts for all sub-languages in the project

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

echo "Generating..."
$script_dir/generate-all.sh $@

echo "Building..."
$script_dir/build-all.sh $@

echo "Linting..."
$script_dir/lint-all.sh $@

echo "Testing..."
$script_dir/test-all.sh $@

echo "Success!"