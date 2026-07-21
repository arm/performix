#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script tests one or more Go module, whose paths are relative to project root

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
"$script_dir"/for-each-go-module.sh "GOFLAGS='-tags=duckdb_arrow' go test ./..." $@
