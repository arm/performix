#!/bin/bash -xe

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# check env vars
if [ -z "${ARTIFACTORY_API_TOKEN}" ]; then
    echo "ARTIFACTORY_API_TOKEN env var should be exposed!"
    exit 1
fi

BINARY_PATH=$1
BINARY_DIR="$(dirname ${BINARY_PATH})"
cd ${BINARY_DIR}
BINARY_FILENAME="$(basename -- ${BINARY_PATH})"

VERSION=$2
ARCH=$3
ARTIFACTORY_URL="https://eu-west-1.artifactory.aws.arm.com/artifactory/ste-apap.cli"

windows_sign() {
    curl -sSf -X POST \
        -d "{\"signer\":\"authenticode\",\"source\":\"${ARTIFACTORY_URL}/${VERSION}/${ARCH}/${BINARY_FILENAME}\",\"dest\":\"${ARTIFACTORY_URL}/${VERSION}/${ARCH}/signed/${BINARY_FILENAME}\"}" \
        -H "Content-Type: application/json" \
        -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        https://eu-west-1.authenticode.aws.arm.com/sign-artifact
}

download_artifactory() {
    curl -LsSf -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        -O "${ARTIFACTORY_URL}/${VERSION}/${ARCH}/signed/${BINARY_FILENAME}"
}

upload_artifactory() {
    curl -sSf -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" -X PUT -T ${BINARY_FILENAME} \
        "${ARTIFACTORY_URL}/${VERSION}/${ARCH}/${BINARY_FILENAME}"
}

delete_artifactory() {
    curl -sSf -X DELETE \
        -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        "${ARTIFACTORY_URL}/${VERSION}/${ARCH}"
}

echo "Signing ${BINARY_FILENAME}"
upload_artifactory || exit 1
windows_sign || exit 1
download_artifactory || exit 1
delete_artifactory || exit 1
echo "${BINARY_FILENAME} signed"
exit 0
