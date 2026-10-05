#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
project_root=`$script_dir/../get-project-root.sh`

# The Task workflow supplies the venv directory and layout explicitly because
# OSTYPE is inherited from the shell and does not reliably identify an MSYS2
# UCRT64 invocation. This keeps venvs for incompatible interpreter families
# separate when they share a checkout.
venv_directory="${PERFORMIX_VENV_DIRECTORY:-env}"
venv="$project_root/$venv_directory"

if [[ "${PERFORMIX_VENV_LAYOUT:-}" == "windows" ]]; then
  # run-bash.py supplies the native Windows interpreter selected by the Task
  # workflow.
  python_cmd="${PERFORMIX_PYTHON:-python}"
  venv_python="$venv/Scripts/python.exe"
else
  python_cmd="python3"
  venv_python="$venv/bin/python"
fi

if [ ! -d "$venv" ]; then
  "$python_cmd" -m venv "$venv"
fi

# Ensure pip exists in the venv and is up to date
"$venv_python" -m ensurepip --upgrade >/dev/null 2>&1 || true
"$venv_python" -m pip install --upgrade pip >/dev/null 2>&1 || true

export VENV_PYTHON="$venv_python"
export project_root
