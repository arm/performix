#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

TARGET_IP="${TARGET_IP:-}"
TARGET_USER="${TARGET_USER:-}"
SSH_KEY="${SSH_KEY:-}"
WORKLOAD="${WORKLOAD:-}"
WORKLOAD_REF="${WORKLOAD_REF:-main}"
WORKLOAD_REPO="${WORKLOAD_REPO:-https://github.com/Arm-Debug/performix-workloads.git}"
GH_TOKEN="${GH_TOKEN:-}"
REMOTE_ROOT="${REMOTE_ROOT:-}"
WORKLOAD_ARGS="${WORKLOAD_ARGS:-}"
SKIP_WORKLOAD_CHECKOUT="${SKIP_WORKLOAD_CHECKOUT:-false}"

missing=()
for required in TARGET_USER TARGET_IP SSH_KEY WORKLOAD REMOTE_ROOT; do
  if [ -z "${!required}" ]; then
    missing+=( "${required}" )
  fi
done
if [ "${SKIP_WORKLOAD_CHECKOUT}" != "true" ] && [ -z "${GH_TOKEN}" ]; then
  missing+=( "GH_TOKEN" )
fi
if [ "${#missing[@]}" -gt 0 ]; then
  echo "Missing required environment variables: ${missing[*]}"
  exit 1
fi

TARGET="${TARGET_USER}@${TARGET_IP}"
REMOTE_WORKLOADS="${REMOTE_ROOT}/workloads"
REMOTE_DCPERF_DIR="${REMOTE_ROOT}/DCPerf"

echo "Building workload '${WORKLOAD}' on target ${TARGET}..."
ssh -i "${SSH_KEY}" ${TARGET} "WORKLOAD=${WORKLOAD} WORKLOAD_REF=${WORKLOAD_REF} WORKLOAD_REPO=${WORKLOAD_REPO} GH_TOKEN=${GH_TOKEN} REMOTE_ROOT=${REMOTE_ROOT} REMOTE_WORKLOADS=${REMOTE_WORKLOADS} REMOTE_DCPERF_DIR=${REMOTE_DCPERF_DIR} SKIP_WORKLOAD_CHECKOUT=${SKIP_WORKLOAD_CHECKOUT} bash -s" <<'ENDSSH'
set -euo pipefail

mkdir -p "${REMOTE_ROOT}"
cd "${REMOTE_ROOT}"

# Retry wrapper for apt-get to handle transient failures and lock contention
apt_get_with_retry() {
  local max_attempts=5
  local attempt=1
  local delay=10
  while [ "${attempt}" -le "${max_attempts}" ]; do
    if sudo DEBIAN_FRONTEND=noninteractive apt-get "$@"; then
      return 0
    fi
    echo "apt-get $* failed on attempt ${attempt}/${max_attempts}. Retrying in ${delay}s..." >&2
    attempt=$((attempt + 1))
    sleep "${delay}"
  done
  echo "apt-get $* failed after ${max_attempts} attempts." >&2
  return 1
}

mkdir -p "${REMOTE_ROOT}"
cd "${REMOTE_ROOT}"

# Ensure prerequisites for building workloads are present on target
apt_get_with_retry update
apt_get_with_retry install -y git build-essential

# Fetch workloads repository on target (shallow clone)
if [ "${SKIP_WORKLOAD_CHECKOUT}" != "true" ]; then
  if [ ! -d "${REMOTE_WORKLOADS}" ]; then
    git clone --depth 1 --branch "${WORKLOAD_REF}" "https://${GH_TOKEN}@github.com/Arm-Debug/performix-workloads.git" "${REMOTE_WORKLOADS}"
  else
    cd "${REMOTE_WORKLOADS}"
    git fetch --depth 1 origin "${WORKLOAD_REF}"
    git checkout -f "${WORKLOAD_REF}" || git checkout -f "origin/${WORKLOAD_REF}" || true
    git reset --hard "origin/${WORKLOAD_REF}" || true
    cd "${REMOTE_ROOT}"
  fi
else
  echo "Using existing workload checkout at ${REMOTE_WORKLOADS}"
fi

cd "${REMOTE_WORKLOADS}"

# AI Insights tests use their own CMake project under ai_insights_tests.
case "${WORKLOAD}" in
  ai_insights_tests/*)
    test_id="${WORKLOAD#ai_insights_tests/}"
    ai_root="${REMOTE_WORKLOADS}/ai_insights_tests"
    if [ ! -f "${ai_root}/CMakeLists.txt" ]; then
      echo "AI Insights CMake project not found: ${ai_root}/CMakeLists.txt"
      exit 1
    fi
    apt_get_with_retry install -y \
      cmake \
      openjdk-21-jdk-headless \
      openjdk-11-jdk-headless \
      curl \
      file
    cmake -S "${ai_root}" -B "${ai_root}/build-release" -DCMAKE_BUILD_TYPE=Release
    cmake --build "${ai_root}/build-release" --target "${test_id}"
    workload_bin="${ai_root}/build-release/${test_id}"
    if [ ! -e "${workload_bin}" ]; then
      echo "AI Insights workload output not found: ${workload_bin}"
      exit 1
    fi
    file_output="$(file "${workload_bin}")"
    echo "${file_output}"
    if [[ "${file_output}" == *"ELF"* && "${file_output}" != *"with debug_info"* ]]; then
      echo "AI Insights workload ELF binary lacks debug info: ${workload_bin}"
      exit 1
    fi
    exit 0
    ;;
esac

# Megabench, stress-ng & sysbench use the default build script path <workload>/<workload>.sh
case "${WORKLOAD}" in
  dcperf-mediawiki)
    SCRIPT="dcperf/mediawiki/bootstrap_mediawiki.sh"
    ;;
  nosqlbench-cassandra)
    SCRIPT="nosqlbench-cassandra/bootstrap-nosqlbench-cassandra.sh"
    ;;
  *)
    SCRIPT="${WORKLOAD}/${WORKLOAD}.sh"
    ;;
esac

echo "Using build script: ${SCRIPT}"
if [ ! -f "${SCRIPT}" ]; then
  echo "Build script not found: ${SCRIPT}"
  ls -lah .
  exit 1
fi

SCRIPT_DIR="$(dirname "${SCRIPT}")"
SCRIPT_FILE="$(basename "${SCRIPT}")"
echo "Changing to script directory: ${SCRIPT_DIR}"
pushd "${SCRIPT_DIR}" >/dev/null

NEEDS_SUDO=false
BUILD_ARGS=()
case "${WORKLOAD}" in
  dcperf-mediawiki)
    BUILD_ARGS+=( "${REMOTE_DCPERF_DIR}" )
    NEEDS_SUDO=true
    ;;
  nosqlbench-cassandra)
    NEEDS_SUDO=true
    ;;
  sysbench)
    NEEDS_SUDO=true
    ;;
esac
BUILD_CMD=( "./${SCRIPT_FILE}" "${BUILD_ARGS[@]}" )

if [ "$NEEDS_SUDO" = true ]; then
  sudo bash "${BUILD_CMD[@]}"
else
  bash "${BUILD_CMD[@]}"
fi

popd >/dev/null
ENDSSH

# Determine workload command as array
WORKLOAD_NAME="${WORKLOAD}"
DEFAULT_ARGS=""

if [ "$WORKLOAD_NAME" = "dcperf-mediawiki" ]; then
  RUN_SCRIPT="${REMOTE_WORKLOADS}/dcperf/mediawiki/run_mediawiki.sh"
  RUN_SCRIPT_Q="$(printf '%q' "${RUN_SCRIPT}")"
  WORKLOAD_EXEC="$(
    ssh -i "${SSH_KEY}" "${TARGET}" "RUN_SCRIPT=${RUN_SCRIPT_Q} bash -lc 'set -euo pipefail; if [ ! -f \"\$RUN_SCRIPT\" ]; then echo \"::error:: MediaWiki run script not found: \$RUN_SCRIPT\" >&2; exit 1; fi; chmod +x \"\$RUN_SCRIPT\" || true; printf \"%s\n\" \"\$RUN_SCRIPT\"'"
  )"
  WORKLOAD_ARR=( "${WORKLOAD_EXEC}" )
  DEFAULT_ARGS="${REMOTE_DCPERF_DIR} 10s 1"

elif [ "$WORKLOAD_NAME" = "megabench" ]; then
  WORKLOAD_ARR=( "${REMOTE_ROOT}/workloads/megabench/megabench" )
  DEFAULT_ARGS="0 0"

elif [ "$WORKLOAD_NAME" = "nosqlbench-cassandra" ]; then
  RUN_SCRIPT="${REMOTE_WORKLOADS}/nosqlbench-cassandra/run-nosqlbench-cassandra.sh"
  RUN_SCRIPT_Q="$(printf '%q' "${RUN_SCRIPT}")"
  WORKLOAD_EXEC="$(
    ssh -i "${SSH_KEY}" "${TARGET}" "RUN_SCRIPT=${RUN_SCRIPT_Q} bash -lc 'set -euo pipefail; if [ ! -f \"\$RUN_SCRIPT\" ]; then echo \"::error:: nosqlbench-cassandra run script not found: \$RUN_SCRIPT\" >&2; exit 1; fi; chmod +x \"\$RUN_SCRIPT\" || true; printf \"%s\n\" \"\$RUN_SCRIPT\"'"
  )"
  WORKLOAD_ARR=( "${WORKLOAD_EXEC}" )
  DEFAULT_ARGS="--workload /opt/nosqlbench/workloads/cql_keyvalue.yaml --driver cql --main-block main_write --main-cycles 50M --threads auto"

elif [ "$WORKLOAD_NAME" = "stress-ng" ]; then
  WORKLOAD_ARR=( "${REMOTE_ROOT}/workloads/stress-ng/stress-ng-src/stress-ng" )
  DEFAULT_ARGS="--cpu 90 --cpu-method matrixprod --timeout 30s --metrics-brief"

elif [ "$WORKLOAD_NAME" = "sysbench" ]; then
  WORKLOAD_ARR=( "sysbench" )
  DEFAULT_ARGS="cpu --threads=10 --time=5 --rand-seed=1 run"

elif [[ "$WORKLOAD_NAME" == ai_insights_tests/* ]]; then
  TEST_ID="${WORKLOAD_NAME#ai_insights_tests/}"
  WORKLOAD_ARR=( "${REMOTE_WORKLOADS}/ai_insights_tests/build-release/${TEST_ID}" )

else
  echo "Unsupported workload: $WORKLOAD_NAME"
  exit 1
fi

# Append additional workload args to run command if provided, otherwise use default args
if [ -n "${WORKLOAD_ARGS}" ]; then
  # Split by whitespace; quoted segments will be preserved by read -a
  read -r -a EXTRA_ARGS <<< "${WORKLOAD_ARGS}"
  WORKLOAD_ARR+=( "${EXTRA_ARGS[@]}" )
else
  if [ -n "$DEFAULT_ARGS" ]; then
    read -r -a EXTRA_ARGS <<< "$DEFAULT_ARGS"
    WORKLOAD_ARR+=( "${EXTRA_ARGS[@]}" )
  fi
fi

# Serialize array into a safely quoted command string
WORKLOAD_CMD="$(printf '%q ' "${WORKLOAD_ARR[@]}")"
WORKLOAD_CMD="${WORKLOAD_CMD% }"

echo "Resolved workload run command: ${WORKLOAD_CMD}"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  printf 'workload_cmd=%s\n' "${WORKLOAD_CMD}" >> "${GITHUB_OUTPUT}"
fi
printf 'workload_cmd=%s\n' "${WORKLOAD_CMD}"
