#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script is a central place to define constants shared by all scripts
export readonly ARTIFACTORY_BASE_URL="https://artifactory.arm.com/artifactory"
export readonly ARTIFACTORY_TOOLS_BASE_URL="https://artifacts.tools.arm.com"
export readonly ARTIFACTORY_INTERNAL_TOOLS_BASE_URL="https://artifacts.internal.tools.arm.com"

curl_with_retry() {
  curl \
    --retry 5 \
    --retry-all-errors \
    --retry-delay 10 \
    --connect-timeout 30 \
    --max-time 600 \
    "$@"
}
