#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "$script_dir/setup-venv.sh"   # sets $VENV_PYTHON and project_root

pushd "$project_root/robot" > /dev/null

# Install deps into the venv
"$VENV_PYTHON" -m pip install -r "requirements.txt" >/dev/null 2>&1 || true

# Run robocop linter
"$VENV_PYTHON" -m robocop check --config robocop.toml

popd > /dev/null
