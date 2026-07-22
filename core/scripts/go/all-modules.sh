#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script detects all the Go modules in the project

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
project_root=`$script_dir/../get-project-root.sh`

pushd "$project_root" > /dev/null
# Consider anything under core with a 'go.mod' a module that we should build / lint / test.
find . -type f -name go.mod |
  while read -r line; do dirname "${line#./}"; done |
  sort |
  tr '\n' ' '
popd > /dev/null
