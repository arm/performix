//go:build dependabot

// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package api

// This file exists only to ensure that `go mod tidy` does not delete
// dependencies which are only used by generated code (e.g. protobuf
// bindings and mocks), as Dependabot runs `go mod tidy` before those
// sources have been generated.
import (
	_ "github.com/stretchr/testify/mock"
	_ "google.golang.org/grpc"
	_ "google.golang.org/protobuf/proto"
)
