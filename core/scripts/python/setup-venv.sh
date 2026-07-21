#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
project_root=`$script_dir/../get-project-root.sh`

venv="$project_root/env"
if [ ! -d "$venv" ]; then
  python3 -m venv "$venv"
fi

# Ensure pip exists in the venv and is up to date
"$venv/bin/python" -m ensurepip --upgrade >/dev/null 2>&1 || true
"$venv/bin/python" -m pip install --upgrade pip >/dev/null 2>&1 || true

export VENV_PYTHON="$venv/bin/python"
export project_root
