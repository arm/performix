//go:build dependabot

// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tetherproto

// This file exists only to ensure that `go mod tidy` does not delete
// dependencies which are only used by generated code (e.g. protobuf
// bindings and mocks), as Dependabot runs `go mod tidy` before those
// sources have been generated.
import (
	_ "google.golang.org/grpc"
	_ "google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/status"
	_ "google.golang.org/protobuf/reflect/protoreflect"
	_ "google.golang.org/protobuf/runtime/protoimpl"
)
