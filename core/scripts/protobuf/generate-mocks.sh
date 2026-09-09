#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
PROJECT_ROOT=`$SCRIPT_DIR/../get-project-root.sh`
REPOSITORY_ROOT="$PROJECT_ROOT/.."
GO_CLIENT_DIR="clients/go"
SHARED_HEADER="$REPOSITORY_ROOT/copyright-license-header.txt"

MOCKERY_BOILERPLATE=$(mktemp)
MOCKERY_CONFIG=$(mktemp "$PROJECT_ROOT/$GO_CLIENT_DIR/.mockery.generated.yaml.XXXXXX")
trap 'rm -f "$MOCKERY_BOILERPLATE" "$MOCKERY_CONFIG"' EXIT

awk '{ print "// " $0 }' "$SHARED_HEADER" > "$MOCKERY_BOILERPLATE"
printf "\n" >> "$MOCKERY_BOILERPLATE"

awk -v boilerplate="$MOCKERY_BOILERPLATE" '
  { print }
  /^template-data:$/ { printf "  boilerplate-file: \"%s\"\n", boilerplate }
' "$PROJECT_ROOT/$GO_CLIENT_DIR/.mockery.yaml" > "$MOCKERY_CONFIG"

(cd "$PROJECT_ROOT/$GO_CLIENT_DIR" && mockery --config "$MOCKERY_CONFIG")
