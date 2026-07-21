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
trap 'rm -f "$MOCKERY_BOILERPLATE"' EXIT

awk '{ print "// " $0 }' "$SHARED_HEADER" > "$MOCKERY_BOILERPLATE"
printf "\n" >> "$MOCKERY_BOILERPLATE"

(cd "$PROJECT_ROOT/$GO_CLIENT_DIR" && mockery --boilerplate-file "$MOCKERY_BOILERPLATE")
