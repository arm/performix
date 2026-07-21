// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDarwinChecker(t *testing.T) {
	checker := NewChecker()

	t.Run("relies on the effective user ID", func(t *testing.T) {
		expectedPrivileged := os.Geteuid() == 0

		privileged, err := checker.IsPrivileged()
		require.NoError(t, err)

		assert.Equal(t, expectedPrivileged, privileged)
	})
}
