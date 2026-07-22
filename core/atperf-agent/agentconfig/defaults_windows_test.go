// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package agentconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func TestGetDefaultLockRootDirectory(t *testing.T) {
	t.Run("returns expected path on windows", func(t *testing.T) {
		expected := "\\tmp\\" + terminology.GetProductBinaryName()

		result := GetDefaultLockRootDirectory("windows")

		assert.Equal(t, expected, result)
	})
}
