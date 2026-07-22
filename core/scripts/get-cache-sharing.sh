#!/bin/bash -xe

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

CS_SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

source "$CS_SCRIPT_DIR/constants.sh"

# Get tool destination and recipe path
if [ "$#" -lt 1 ]; then
  CS_TOOL_DST_BASE=${CS_SCRIPT_DIR}/../apap-cli/tools
else
  CS_TOOL_DST_BASE=$1
fi
if [ "$#" -lt 2 ]; then
  CS_RECIPE_PATH=${CS_SCRIPT_DIR}/../apap-cli/recipes/cache_sharing.js
else
  CS_RECIPE_PATH=$2
fi

CS_ITS_PYPI_REPO=cache-sharing
CS_VERSION=$(
  grep -m1 'PY_CACHE_SHARING_VERSION' "$CS_RECIPE_PATH" |
  sed -n "s/.*const[[:space:]]*PY_CACHE_SHARING_VERSION[[:space:]]*=[[:space:]]*\([\"']\)\([^\"']*\)\1.*/\2/p"
)
if [ -z "${CS_VERSION}" ]; then
  echo "Failed to extract cache sharing version from: ${CS_RECIPE_PATH}"
  exit 1
fi

CS_ARTIFACT="cache_sharing-${CS_VERSION}-py3-none-any.whl"

# Get Artifactory credentials
if [ -z "${ARTIFACTORY_API_TOKEN}" ]; then
    echo "ARTIFACTORY_API_TOKEN env var should be exposed!"
    exit 1
fi

CS_ARTIFACT_URL="${ARTIFACTORY_BASE_URL}/its.pypi/${CS_ITS_PYPI_REPO}/${CS_VERSION}/${CS_ARTIFACT}"

CS_DST_DIR="${CS_TOOL_DST_BASE}/${CS_ARTIFACT}/${CS_VERSION}"
CS_DST_PATH="${CS_DST_DIR}/${CS_ARTIFACT}"

generate_target_directory() {
  mkdir -p "${CS_DST_DIR}/__tmp"
}

fetch_tools_from_artifactory() {
    curl_with_retry -LsSf -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        -o "${CS_DST_DIR}/__tmp/${CS_ARTIFACT}" "${CS_ARTIFACT_URL}"
}

tar_tools() {
  tar -czvf "${CS_DST_PATH}-Linux-aarch64.tar.gz" \
    -C "${CS_DST_DIR}" "license_terms" \
    -C "${CS_DST_DIR}/__tmp/" "${CS_ARTIFACT}"
  rm -r "${CS_DST_DIR}/__tmp/"
}

# Extract the TPIP and license information from the package and store them
# alongside the tool on the local machine, for easier retrieval by `go-releaser`
# later on.
retrieve_license_terms() {
  local out_dir="${CS_DST_DIR}/license_terms"
  local tpip_out_file="${out_dir}/third_party_licenses.txt"

  # file paths within the wheel (which is a zip file)
  local tpip_arch_file="license_terms/third_party_licenses.txt"

  mkdir -p "${out_dir}"

  # extract third_party_licenses.txt
  if ! unzip -p "${CS_DST_DIR}/__tmp/${CS_ARTIFACT}" \
      "${tpip_arch_file}" > "${tpip_out_file}"; then
      return 1
  fi
}

generate_target_directory || exit 1
fetch_tools_from_artifactory || exit 1
retrieve_license_terms || exit 1
tar_tools || exit 1
exit 0
