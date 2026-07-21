#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Deprecated wrapper retained for compatibility; prefer `task core:deps:tools`.
# Keeping this script for backward compability in CI and other workflows
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
exec python3 "${SCRIPT_DIR}/get-tools.py" "$@"
