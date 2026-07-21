#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

toolchain_dir="${BOOTLIN_TOOLCHAIN_DIR:-/opt/apap-toolchains/bootlin}"
artifactory_repo="${ARTIFACTORY_REPO:-${artifactory_repo:-}}"
github_repository="${GITHUB_REPOSITORY:-}"
repo_name="${GITHUB_REPOSITORY_NAME:-${github_repository##*/}}"

if [ -z "${artifactory_repo}" ]; then
  echo "ARTIFACTORY_REPO is required" >&2
  exit 1
fi

if [ -z "${repo_name}" ]; then
  echo "GITHUB_REPOSITORY_NAME or GITHUB_REPOSITORY is required" >&2
  exit 1
fi

mkdir -p "${toolchain_dir}"

install_toolchain() {
  local toolchain_name="$1"
  local bootlin_family="$2"
  local compiler_name="$3"
  local expected_sha256="$4"

  local tarball="${toolchain_name}.tar.bz2"
  local archive="/tmp/${tarball}"
  local bootlin_url="https://toolchains.bootlin.com/downloads/releases/toolchains/${bootlin_family}/tarballs/${tarball}"
  local artifactory_dir="${artifactory_repo}/${repo_name}/toolchains/bootlin/${toolchain_name}"

  verify_archive() {
    echo "${expected_sha256}  ${archive}" | sha256sum --check --status
  }

  if ! jf rt download \
    "${artifactory_dir}/${tarball}" \
    "${archive}" \
    --flat=true \
    --fail-no-op=true; then
    curl -fsSL -o "${archive}" "${bootlin_url}"
    verify_archive
    jf rt upload \
      "${archive}" \
      "${artifactory_dir}/" \
      --flat=true
  fi

  verify_archive

  rm -rf "${toolchain_dir:?}/${toolchain_name}"
  tar -xjf "${archive}" -C "${toolchain_dir}"

  "${toolchain_dir}/${toolchain_name}/relocate-sdk.sh"
  "${toolchain_dir}/${toolchain_name}/bin/${compiler_name}" --version
}

install_toolchain \
  "x86-64--glibc--stable-2022.08-1" \
  "x86-64" \
  "x86_64-buildroot-linux-gnu-gcc" \
  "861c1e8ad0a66e4c28e7a1f8319d68080ab0ff8d16a765e65540f1957203a190"

install_toolchain \
  "aarch64--glibc--stable-2022.08-1" \
  "aarch64" \
  "aarch64-buildroot-linux-gnu-gcc" \
  "844df3c99508030ee9cb1152cb182500bb9816ff01968f2e18591d51d766c9e7"
