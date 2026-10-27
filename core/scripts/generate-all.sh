#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Generate protobufs, mocks, message codes, and CPU data.

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)

"$script_dir/protobuf/generate.sh"

(
  cd "$script_dir/../apap-engine/message-gen"
  go run gen_codes.go
)

(
  cd "$script_dir/../atperf-agent/systeminfo/armcpus"
  go run generate_cpus.go -in cpus.json -out cpus.go
)
