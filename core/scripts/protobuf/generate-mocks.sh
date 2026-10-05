#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
PROJECT_ROOT=`$SCRIPT_DIR/../get-project-root.sh`
REPOSITORY_ROOT="$PROJECT_ROOT/.."
GO_CLIENT_DIR="clients/go"
SHARED_HEADER="$REPOSITORY_ROOT/copyright-license-header.txt"
MOCKERY_DIR="$PROJECT_ROOT/$GO_CLIENT_DIR"

MOCKERY_BOILERPLATE=$(mktemp "$MOCKERY_DIR/.mockery.boilerplate.XXXXXX")
MOCKERY_CONFIG=$(mktemp "$MOCKERY_DIR/.mockery.generated.yaml.XXXXXX")
MOCKERY_BOILERPLATE_REF="./$(basename "$MOCKERY_BOILERPLATE")"
trap 'rm -f "$MOCKERY_BOILERPLATE" "$MOCKERY_CONFIG"' EXIT

awk '{ print "// " $0 }' "$SHARED_HEADER" > "$MOCKERY_BOILERPLATE"
printf "\n" >> "$MOCKERY_BOILERPLATE"

awk -v boilerplate="$MOCKERY_BOILERPLATE_REF" '
  { print }
  /^template-data:$/ { printf "  boilerplate-file: \"%s\"\n", boilerplate }
' "$MOCKERY_DIR/.mockery.yaml" > "$MOCKERY_CONFIG"

(cd "$MOCKERY_DIR" && mockery --config "$MOCKERY_CONFIG")
