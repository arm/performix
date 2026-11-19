#!/bin/bash -xe

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# check env vars
if [ -z "${ARTIFACTORY_API_TOKEN}" ]; then
    echo "ARTIFACTORY_API_TOKEN env var should be exposed!"
    exit 1
fi

BINARY_PATH=$1
BINARY_DIR="$(dirname -- "${BINARY_PATH}")"
cd "${BINARY_DIR}" || exit 1
BINARY_FILENAME="$(basename -- "${BINARY_PATH}")"

VERSION=$2
ARCH=$3
ARTIFACTORY_URL="https://eu-west-1.artifactory.aws.arm.com/artifactory/ste-apap.cli"
# Tool packaging runs concurrently. Each request must own its upload directory.
SIGNED_BINARY="$(mktemp)" || exit 1
trap 'rm -f "${SIGNED_BINARY}"' EXIT
SIGNING_URL="${ARTIFACTORY_URL}/${VERSION}/${ARCH}/$(basename -- "${SIGNED_BINARY}")"

# Use the same certificate validation on Linux and Windows. OpenSSL does not
# perform revocation checks by default, so disable Schannel's additional
# checks; the configured CA still verifies the certificate chain.
CURL_OPTIONS=(--ssl-no-revoke -sSf)

windows_sign() {
    curl "${CURL_OPTIONS[@]}" \
        --retry 3 --retry-delay 10 --retry-max-time 300 \
        --connect-timeout 30 --max-time 600 \
        -X POST \
        -d "{\"signer\":\"authenticode\",\"source\":\"${SIGNING_URL}/${BINARY_FILENAME}\",\"dest\":\"${SIGNING_URL}/signed/${BINARY_FILENAME}\"}" \
        -H "Content-Type: application/json" \
        -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        https://eu-west-1.authenticode.aws.arm.com/sign-artifact
}

download_artifactory() {
    curl "${CURL_OPTIONS[@]}" -L -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        -o "${SIGNED_BINARY}" "${SIGNING_URL}/signed/${BINARY_FILENAME}"
}

upload_artifactory() {
    curl "${CURL_OPTIONS[@]}" -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" -X PUT -T "${BINARY_FILENAME}" \
        "${SIGNING_URL}/${BINARY_FILENAME}"
}

delete_artifactory() {
    curl "${CURL_OPTIONS[@]}" -X DELETE \
        -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        "${SIGNING_URL}"
}

echo "Signing ${BINARY_FILENAME}"
upload_artifactory || exit 1
windows_sign || exit 1
download_artifactory || exit 1
# Keep the original file and its permissions until the download succeeds.
cp "${SIGNED_BINARY}" "${BINARY_FILENAME}" || exit 1
delete_artifactory || exit 1
echo "${BINARY_FILENAME} signed"
exit 0
