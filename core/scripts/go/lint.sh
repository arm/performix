#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script lints one or more go modules, whose paths are relative to project root; if --fix is passed, it also fixes issues
# Depending on the version of golangci-lint, the output format flags differ, so we detect the version and adjust accordingly

set -euo pipefail

args=()
fix_arg=
for arg in "${@}"; do
  if [ "$arg" == "--fix" ]; then
    fix_arg="--fix"
  else
    args+=("$arg")
  fi
done

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint not found on PATH" >&2
  exit 1
fi

version_output=$(golangci-lint --version 2>/dev/null || golangci-lint version 2>/dev/null || true)
version=$(echo "$version_output" | head -n1 | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+')
version=${version#v}

major_version=2
if [ -n "$version" ]; then
  major_version=${version%%.*}
fi

if [ "$major_version" -ge 2 ]; then
  command="golangci-lint run --build-tags=duckdb_arrow ./... $fix_arg --output.text.print-issued-lines --output.text.print-linter-name --output.text.colors --timeout 300s --max-issues-per-linter 0 --max-same-issues 0"
else
  command="golangci-lint run --build-tags=duckdb_arrow ./... $fix_arg --print-issued-lines --print-linter-name --out-format colored-line-number --timeout 300s --max-issues-per-linter 0 --max-same-issues 0"
fi
"$script_dir"/for-each-go-module.sh "$command" ${args[@]}
