#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -xe

# cmd_os and cmd_arch should be values passed by goreleaser
cmd_os=$1
cmd_arch=$2

os=$cmd_os
arch=$cmd_arch
if [ "$os" == "darwin" ]; then
  os=osx
  arch=universal
fi

duckdb_version=v2.0.0
filename=duckdb-shared-libs-$os-$arch.tar.gz
url=https://github.com/duckdb/duckdb/releases/download/$duckdb_version/$filename
output_dir=$cmd_os/$cmd_arch

mkdir -p deps/duckdb
cd deps/duckdb
wget -O "$filename" "$url"

if [ -x "$output_dir" ]; then
  rm -r "$output_dir"
fi

mkdir -p "$output_dir"
tar -xzf "$filename" -C "$output_dir"
