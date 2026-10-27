#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
PROJECT_ROOT=$("$SCRIPT_DIR/../get-project-root.sh")
API_DIR="$PROJECT_ROOT/api"

for tool in go protoc protoc-gen-go protoc-gen-go-grpc mockery; do
    if ! command -v "$tool" &> /dev/null; then
        echo "Required tool '$tool' is not installed. Run ./bootstrap first." >&2
        exit 1
    fi
done

echo "== Tool Versions =="
echo "protoc: $(protoc --version)"
echo "protoc-gen-go: $(protoc-gen-go --version 2>&1)"
echo "protoc-gen-go-grpc: $(protoc-gen-go-grpc --version)"

PROTOBUF_SOURCE_FILES=()
while IFS= read -r -d '' file; do
    PROTOBUF_SOURCE_FILES+=("$file")
done < <(find "$API_DIR" -name '*.proto' -print0)

echo "Generating Go gRPC code..."
mkdir -p "$API_DIR"
# Remove stale outputs for renamed or deleted schemas.
find "$API_DIR" -type f -name '*.pb.go' -delete
protoc \
  --proto_path="$API_DIR" \
  --go_out="$API_DIR" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$API_DIR" \
  --go-grpc_opt=paths=source_relative \
  "${PROTOBUF_SOURCE_FILES[@]}"

echo "Generating testify mocks..."
"$SCRIPT_DIR/generate-mocks.sh"
echo "Code generation complete. Output is in $API_DIR"
