#!/usr/bin/env bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
PROJECT_ROOT=`$SCRIPT_DIR/../get-project-root.sh`

# === CONFIGURATION ===
# Adjust these as needed
PROTOBUF_SOURCE_DIR="$PROJECT_ROOT/api"                   # Directory containing your .proto files
PROTOBUF_SOURCE_FILES=$(find "$PROTOBUF_SOURCE_DIR" -name "*.proto")  # All .proto files
OUTPUT_DIR="$PROJECT_ROOT"         # Where generated files will go
GO_CLIENT_DIR="clients/go"                          # Subdirectory for Go output
MOCK_OUTPUT_DIR="$OUTPUT_DIR/$GO_CLIENT_DIR/mocks"

# === TOOLS SETUP ===

echo "Installing protoc if not present..."
if ! command -v protoc &> /dev/null; then
    echo "Please install protoc manually: https://grpc.io/docs/protoc-installation/"
    exit 1
fi

echo "Installing Go gRPC plugins..."
GO_EXECUTABLE=$(command -v go)
"$GO_EXECUTABLE" install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
"$GO_EXECUTABLE" install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.4.0

GO_BIN_DIR=$("$GO_EXECUTABLE" env GOBIN)
if [[ -z "$GO_BIN_DIR" ]]; then
    GO_PATHS=$("$GO_EXECUTABLE" env GOPATH)
    if [[ "$("$GO_EXECUTABLE" env GOHOSTOS)" == "windows" ]]; then
        GO_BIN_DIR="${GO_PATHS%%;*}/bin"
    else
        GO_BIN_DIR="${GO_PATHS%%:*}/bin"
    fi
fi
export PATH="$GO_BIN_DIR:$PATH"

# === REPORT VERSIONS ===

echo "== Tool Versions =="
echo "protoc: $(protoc --version)"
echo "protoc-gen-go: $(protoc-gen-go --version 2>&1)"
echo "protoc-gen-go-grpc: $(protoc-gen-go-grpc --version)"

# === GENERATE CODE ===

echo "Generating Go gRPC code..."
mkdir -p "$OUTPUT_DIR/$GO_CLIENT_DIR"
protoc \
  --proto_path="$PROTOBUF_SOURCE_DIR" \
  --go_out="$OUTPUT_DIR/$GO_CLIENT_DIR" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$OUTPUT_DIR/$GO_CLIENT_DIR" \
  --go-grpc_opt=paths=source_relative \
  $PROTOBUF_SOURCE_FILES

echo "Code generation complete. Output is in $OUTPUT_DIR/$GO_CLIENT_DIR"

# === GENERATE MOCKS ===

echo "Installing mockery..."
"$GO_EXECUTABLE" install github.com/vektra/mockery/v3@v3.7.4

echo "Generating testify mocks from interfaces in $OUTPUT_DIR/$GO_CLIENT_DIR ..."
"$SCRIPT_DIR/generate-mocks.sh"

echo "Mocks generated in $MOCK_OUTPUT_DIR"
