#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Wrapper that activates the Python venv and delegates to scripts/run-robot.py.
# Flags parsed by the caller of run-robot.sh are forwarded directly to run-robot.py, e.g.:
#   run-robot.sh --target my_target --exclude-tags cpu_microarchitecture
# Run `python3 scripts/run-robot.py --help` for the full list of options.

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

source "$script_dir/setup-venv.sh"
export PATH="$(dirname "$VENV_PYTHON"):$PATH"

project_root=`$script_dir/../get-project-root.sh`
"$VENV_PYTHON" -m pip install -r "$project_root/robot/requirements.txt"

"$VENV_PYTHON" "$script_dir/../run-robot.py" "$@"
