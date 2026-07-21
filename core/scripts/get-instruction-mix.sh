#!/bin/bash -xe

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

IM_SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

source "$IM_SCRIPT_DIR/constants.sh"

# Get tool destination and recipe path
if [ "$#" -lt 1 ]; then
  IM_TOOL_DST_BASE=${IM_SCRIPT_DIR}/../apap-cli/tools
else
  IM_TOOL_DST_BASE=$1
fi
if [ "$#" -lt 2 ]; then
  IM_RECIPE_PATH=${IM_SCRIPT_DIR}/../apap-cli/recipes/instruction_mix.js
else
  IM_RECIPE_PATH=$2
fi

IM_ITS_PYPI_REPO=instruction-mix
IM_VERSION=$(
  grep -m1 'PY_IM_VERSION' "$IM_RECIPE_PATH" |
  sed -n "s/.*const[[:space:]]*PY_IM_VERSION[[:space:]]*=[[:space:]]*\([\"']\)\([^\"']*\)\1.*/\2/p"
)
if [ -z "${IM_VERSION}" ]; then
  echo "Failed to extract instruction mix version from: ${IM_RECIPE_PATH}"
  exit 1
fi

IM_ARTIFACT="instruction_mix-${IM_VERSION}-py3-none-any.whl"

# Get Artifactory credentials
if [ -z "${ARTIFACTORY_API_TOKEN}" ]; then
    echo "ARTIFACTORY_API_TOKEN env var should be exposed!"
    exit 1
fi

IM_ARTIFACT_URL="${ARTIFACTORY_BASE_URL}/its.pypi/${IM_ITS_PYPI_REPO}/${IM_VERSION}/${IM_ARTIFACT}"

IM_DST_DIR="${IM_TOOL_DST_BASE}/${IM_ARTIFACT}/${IM_VERSION}"
IM_DST_PATH="${IM_DST_DIR}/${IM_ARTIFACT}"

generate_target_directory() {
  mkdir -p "${IM_DST_DIR}/__tmp"
}

fetch_tools_from_artifactory() {
    curl_with_retry -LsSf -H "X-JFrog-Art-Api:$ARTIFACTORY_API_TOKEN" \
        -o "${IM_DST_DIR}/__tmp/${IM_ARTIFACT}" "${IM_ARTIFACT_URL}"
}

tar_tools() {
  tar -czvf "${IM_DST_PATH}-Linux-aarch64.tar.gz" \
    -C "${IM_DST_DIR}" "license_terms" \
    -C "${IM_DST_DIR}/__tmp/" "${IM_ARTIFACT}"
  rm -r "${IM_DST_DIR}/__tmp/"
}

# Extract the TPIP and license information from the package and store them
# alongside the tool on the local machine, for easier retrieval by `go-releaser`
# later on.
retrieve_license_terms() {
  local out_dir="${IM_DST_DIR}/license_terms"
  local tpip_out_file="${out_dir}/third_party_licenses.txt"
  local license_out_file="${out_dir}/license_agreement.txt"

  # file paths within the wheel (which is a zip file)
  local tpip_arch_file="license_terms/third_party_licenses.txt"
  local license_arch_file="license_terms/license_agreement.txt"

  mkdir -p "${out_dir}"

  # extract third_party_licenses.txt
  if ! unzip -p "${IM_DST_DIR}/__tmp/${IM_ARTIFACT}" \
      "${tpip_arch_file}" > "${tpip_out_file}"; then
      return 1
  fi

  # extract license_agreement.txt
  if ! unzip -p "${IM_DST_DIR}/__tmp/${IM_ARTIFACT}" \
      "${license_arch_file}" > "${license_out_file}"; then
      return 1
  fi
}

generate_target_directory || exit 1
fetch_tools_from_artifactory || exit 1
retrieve_license_terms || exit 1
tar_tools || exit 1
exit 0
