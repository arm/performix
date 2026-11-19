// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestFailureReasonClassifiesWindowsDirectoryErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		cause  error
		reason FailureReason
	}{
		{"not directory", windows.ERROR_DIRECTORY, FailureNotDirectory},
		{"missing path", windows.ERROR_PATH_NOT_FOUND, FailureNotFound},
		{"missing file", windows.ERROR_FILE_NOT_FOUND, FailureNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given a wrapped Windows error describing a directory or missing path.
			err := fmt.Errorf("private-path: %w", test.cause)

			// When the error is classified for a caller.
			reason := FailureReasonForError(err)

			// Then missing paths remain distinct from invalid directories.
			require.Equal(t, test.reason, reason)
		})
	}
}
