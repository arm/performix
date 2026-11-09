#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Runs all engine & CLI tests (Go unit tests, Python unit tests)
# Robot Framework functional tests are run separately with task core:test:robot.

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)

# Run Go unit tests
"$script_dir/go/test.sh" $("$script_dir/go/all-modules.sh") "$@"

# Run Python unit tests
task --dir "$script_dir/../.." core:test:unit:scripts
task --dir "$script_dir/../.." core:test:unit:sysutil-timeline -- "$@"

echo "Unit test run completed for all Go and Python modules tracked by Git!"

echo "Note: This script/task does not run Robot Framework functional tests."
