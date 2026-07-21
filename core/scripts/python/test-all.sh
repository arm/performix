#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Runs Python unit tests

set -euo pipefail

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
project_root=`$script_dir/../get-project-root.sh`

pushd "$project_root" > /dev/null

source "$script_dir/setup-venv.sh"
"$VENV_PYTHON" -m pip install pytest

"$VENV_PYTHON" -m unittest scripts/test_run_export_helper.py -v
"$VENV_PYTHON" -m unittest discover -s scripts/atperf-compat -v

# Cross-platform build-orchestration helpers (lib package). Use the package's
# import root (scripts) as the top-level dir so "from lib.x import y" resolves.
"$VENV_PYTHON" -m unittest discover -s scripts/lib -t scripts -v

PYTHONPATH=apap-cli/tools-builtin/sysutil-timeline \
  "$VENV_PYTHON" -m pytest apap-cli/tools-builtin/sysutil-timeline/tests -v "$@"

popd > /dev/null
