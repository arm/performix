#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Runs all engine & CLI tests (Go unit tests, Python unit tests)
# NOTE: Robot Framework functional are not run from this script! See ./scripts/python/run-robot.sh

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

# Run Go unit tests
$script_dir/go/test-all.sh $@

# Run Python unit tests
$script_dir/python/test-all.sh $@

echo "Unit test run completed for all Go and Python modules tracked by Git!"

# See also ./scripts/python/run-robot.sh
echo "Note: This script/task does not run Robot Framework functional tests."
