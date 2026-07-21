#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script builds all detected Go modules in the project

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

# Generated code must exist in order for the build to work
$script_dir/generate-all.sh $@
$script_dir/build.sh `$script_dir/all-modules.sh`v
