// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package agentconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
)

func TestGetDefaultLockRootDirectory(t *testing.T) {
	tests := []struct {
		goos       string
		pathPrefix string
	}{
		{"linux", "/tmp/"},
		{"darwin", "/tmp/"},
		{"android", "/data/local/tmp/"},
	}

	for _, test := range tests {
		t.Run("returns expected path on "+test.goos, func(t *testing.T) {
			expected := test.pathPrefix + terminology.GetProductBinaryName()

			result := GetDefaultLockRootDirectory(test.goos)

			assert.Equal(t, expected, result)
		})
	}
}
