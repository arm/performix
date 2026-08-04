#!/bin/bash -xe

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Validate required environment variables.
if [ -z "${ARTIFACTORY_API_TOKEN}" ]; then
    echo "ARTIFACTORY_API_TOKEN env var should be exposed!"
    exit 1
fi

# If no destination is provided, default to apap-cli/tools in this repository.
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
if [ "$#" -lt 1 ]; then
  TOOL_DST_BASE=${SCRIPT_DIR}/../apap-cli/tools
else
  TOOL_DST_BASE=$1
fi

source "$SCRIPT_DIR/constants.sh"

ASCT_NAME="asct"
ASCT_VERSION="0.6.1"
ASCT_RELEASE_COMMIT_ID="d2f4360"

ARTIFACTORY_ARCHIVE_NAME="${ASCT_NAME}-${ASCT_VERSION}+${ASCT_RELEASE_COMMIT_ID}-release.tar.gz"   # archive name pulled from artifactory
ARTIFACTORY_INTERNAL_PACKAGE_NAME="${ASCT_NAME}-${ASCT_VERSION}+${ASCT_RELEASE_COMMIT_ID}.tar.gz"   # package name pulled from artifactory
STATIC_ARCHIVE_NAME_AARCH64="${ASCT_NAME}-Linux-aarch64.tar.gz"
STATIC_ARCHIVE_NAME_X86_64="${ASCT_NAME}-Linux-x86_64.tar.gz"
ASCT_DOCS_PAYLOAD_DIR="${ASCT_NAME}_docs"                                           # docs directory inside the top-level release archive
TOOL_DST_DIR=${TOOL_DST_BASE}/${ASCT_NAME}/${ASCT_VERSION}

# ASCT artifacts are currently downloaded from the internal tools host.
ARTIFACTORY_URL="${ARTIFACTORY_INTERNAL_TOOLS_BASE_URL}/asct/dist/${ASCT_VERSION}"

generate_target_directory() {
  mkdir -p ${TOOL_DST_DIR}
}

fetch_tools_from_artifactory() {
  # Download the top-level ASCT release bundle.
  curl_with_retry -LsSf -o "${TOOL_DST_DIR}/${ARTIFACTORY_ARCHIVE_NAME}" "${ARTIFACTORY_URL}/${ARTIFACTORY_ARCHIVE_NAME}"
}

retrieve_license_terms() {
  local out_dir="${TOOL_DST_DIR}/license_terms"
  local tpip_out_file="${out_dir}/third_party_licenses.txt"
  local license_out_file="${out_dir}/license_agreement.txt"
  local package_root="${ARTIFACTORY_INTERNAL_PACKAGE_NAME%.tar.gz}"
  local tmp_file=""

  # Paths of license files inside the installable package archive.
  local tpip_arch_file="${package_root}/license_terms/third_party_licenses.txt"
  local license_arch_file="${package_root}/license_terms/license_agreement.txt"

  mkdir -p "${out_dir}"
  tmp_file=$(mktemp) || return 1

  # Extract third_party_licenses.txt from the package tarball.
  if tar -xOzf "${TOOL_DST_DIR}/${ARTIFACTORY_INTERNAL_PACKAGE_NAME}" \
      "${tpip_arch_file}" > "${tmp_file}"; then
      mv "${tmp_file}" "${tpip_out_file}"
  else
      rm -f "${tmp_file}"
      return 1
  fi

  # Extract license_agreement.txt from the package tarball.
  if tar -xOzf "${TOOL_DST_DIR}/${ARTIFACTORY_INTERNAL_PACKAGE_NAME}" \
      "${license_arch_file}" > "${tmp_file}"; then
      mv "${tmp_file}" "${license_out_file}"
  else
      rm -f "${tmp_file}"
      return 1
  fi
}

# The top-level release bundle contains one installable package tarball.
# Extract that inner tarball as a file (without unpacking its contents).
extract_package() {
  local installable_entry=""

  # Locate the nested installable tarball inside the top-level release bundle.
  installable_entry=$(tar -tzf "${TOOL_DST_DIR}/${ARTIFACTORY_ARCHIVE_NAME}" | grep -F "${ARTIFACTORY_INTERNAL_PACKAGE_NAME}" | head -n 1 || true)
  if [ -z "${installable_entry}" ]; then
    echo "Could not find ${ARTIFACTORY_INTERNAL_PACKAGE_NAME} in ${ARTIFACTORY_ARCHIVE_NAME}"
    return 1
  fi

  # Extract the nested tarball as a file and keep it intact.
  tar -xOzf "${TOOL_DST_DIR}/${ARTIFACTORY_ARCHIVE_NAME}" "${installable_entry}" > "${TOOL_DST_DIR}/${ARTIFACTORY_INTERNAL_PACKAGE_NAME}"
}

# ASCT ships a single package tarball, but the CLI expects per-architecture
# filenames. Copy and rename the same package to satisfy that interface.
create_static_bundle_aliases() {

  tar -czvf "${TOOL_DST_DIR}/${STATIC_ARCHIVE_NAME_AARCH64}" -C ${TOOL_DST_DIR} ${ARTIFACTORY_INTERNAL_PACKAGE_NAME}
  tar -czvf "${TOOL_DST_DIR}/${STATIC_ARCHIVE_NAME_X86_64}" -C ${TOOL_DST_DIR} ${ARTIFACTORY_INTERNAL_PACKAGE_NAME}
  
  # Clean-up intermediate files
  rm "${TOOL_DST_DIR}/${ARTIFACTORY_INTERNAL_PACKAGE_NAME}"
  rm "${TOOL_DST_DIR}/${ARTIFACTORY_ARCHIVE_NAME}"
}

generate_target_directory || exit 1
fetch_tools_from_artifactory || exit 1
extract_package || exit 1
retrieve_license_terms || exit 1
create_static_bundle_aliases || exit 1

exit 0
