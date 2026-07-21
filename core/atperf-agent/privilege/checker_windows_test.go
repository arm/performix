// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestWindowsChecker(t *testing.T) {
	checker := NewChecker()

	t.Run("relies on TOKEN_ELEVATION", func(t *testing.T) {
		expectedPrivileged := windows.GetCurrentProcessToken().IsElevated()

		privileged, err := checker.IsPrivileged()
		require.NoError(t, err)

		assert.Equal(t, expectedPrivileged, privileged)
	})
}
