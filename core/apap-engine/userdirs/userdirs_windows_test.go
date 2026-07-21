// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package userdirs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/stretchr/testify/assert"
)

func TestUserDirs(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	testCases := []struct {
		name string
		fn   func() (string, error)
		want string
	}{
		{
			"User state dir",
			StateDir,
			filepath.Join(homeDir, "AppData", "Local", terminology.GetDaemonDirName()),
		},
		{
			"User config dir",
			ConfigDir,
			filepath.Join(homeDir, "AppData", "Local", terminology.GetDaemonDirName()),
		},
		{
			"User data dir",
			DataDir,
			filepath.Join(homeDir, "AppData", "Local", terminology.GetDaemonDirName()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fn()
			assert.NoError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}
