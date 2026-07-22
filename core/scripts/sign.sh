#!/bin/bash -xe

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

SIGN_REQUIRED=${SIGN_REQUIRED:-false}
if [ "${SIGN_REQUIRED}" != "true" ]; then
    echo "Skip signing as it is not required"
    exit 0
fi

script_dir=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

PLATFORM=$1
shift
if [ "${PLATFORM}" == "windows" ]; then
    echo "$(pwd)"
    source "${script_dir}/sign-windows.sh" "$@"
elif [ "${PLATFORM}" == "darwin" ]; then
    echo "$(pwd)"
    source "${script_dir}/sign-darwin.sh" "$@"
else
    exit 0
fi
