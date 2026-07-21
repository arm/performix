#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script runs the linter script for all sub-languages in the project

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

echo "Linting Go code"
$script_dir/go/lint-all.sh $@

echo "Linting JavaScript code"
$script_dir/node/lint-all.py $@

echo "Linting Robot code"
$script_dir/python/lint-robot.sh
