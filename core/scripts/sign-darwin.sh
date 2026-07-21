#!/bin/bash -xe

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

if [ -z "${ARM_USERNAME}" ]; then
    echo "ARM_USERNAME env var should be exposed!"
    exit 1
fi


BINARY_PATH=$1
BINARY_DIR="$(dirname ${BINARY_PATH})"
cd ${BINARY_DIR}
BINARY_FILENAME="$(basename -- ${BINARY_PATH})"

echo "Signing ${BINARY_FILENAME}"
cs-client run-all \
    -t mach-o \
    --deep \
    --force \
    --signature-flag runtime \
    --timestamp-server default \
    --entitlement com.apple.security.cs.allow-jit \
    --entitlement com.apple.security.cs.allow-unsigned-executable-memory \
    --entitlement com.apple.security.cs.disable-executable-page-protection \
    --entitlement com.apple.security.cs.disable-library-validation \
    --entitlement com.apple.security.cs.allow-dyld-environment-variables \
    "${ARM_USERNAME}" "${BINARY_FILENAME}" --output "${BINARY_FILENAME}"
echo "${BINARY_FILENAME} signed"
exit 0
