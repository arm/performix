// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package process

import (
	"fmt"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailureReasonClassifiesUnixNotDirectory(t *testing.T) {
	// Given a wrapped Unix error for a path component that is not a directory.
	err := fmt.Errorf("private-path: %w", syscall.ENOTDIR)

	// When the error is classified for a caller.
	reason := FailureReasonForError(err)

	// Then the caller receives the specific directory diagnostic.
	require.Equal(t, FailureNotDirectory, reason)
}
