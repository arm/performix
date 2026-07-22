#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script generates all detected Go modules in the project

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
$script_dir/generate.sh `$script_dir/all-modules.sh` $@
