// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package privilege

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

const rootEUID = 0
const userEUID = 1000

func TestLinuxChecker_isPrivileged(t *testing.T) {
	t.Run("relies on the effective user ID", func(t *testing.T) {
		assert.True(t, isPrivileged(rootEUID))
		assert.False(t, isPrivileged(userEUID))
	})
}

func TestLinuxChecker_IsPrivileged(t *testing.T) {
	checker := &LinuxChecker{}
	expected := isPrivileged(os.Geteuid())

	result, err := checker.IsPrivileged()

	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}
