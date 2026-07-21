#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script runs a command from each of the supplied Go module directories

set -euo pipefail

function usage() {
  echo "Usage: $0 COMMAND GO_MODULE [GO_MODULE...]"
}

if [ "$#" -lt 1 ]; then
  echo "Missing argument for COMMAND"
  usage
  exit 1
fi
command=$1

if [ "$#" -lt 2 ]; then
  echo "Missing argument for GO_MODULE"
  usage
  exit 1
fi

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
project_root=`$script_dir/../get-project-root.sh`

# shift
# for arg in "${@}"; do
#   pushd "$project_root/$arg" > /dev/null
#   bash -c "$command"
#   popd > /dev/null
# done

# Skip any modules that don't have a go.mod file
shift
for arg in "${@}"; do
  if [ ! -f "$project_root/$arg/go.mod" ]; then
    echo "Skipping $arg (no go.mod)"
    continue
  fi

  pushd "$project_root/$arg" > /dev/null
  bash -c "$command"
  popd > /dev/null
done