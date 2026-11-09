// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package conductor

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestHelperCommandHasNoConsole(t *testing.T) {
	command := newLocalCommand("cmd.exe", "/d", "/c", "echo readiness")
	require.True(t, command.SysProcAttr.HideWindow)
	require.Equal(t, uint32(windows.CREATE_NO_WINDOW), command.SysProcAttr.CreationFlags)
	output, err := command.Output()
	require.NoError(t, err)
	require.Equal(t, "readiness\r\n", string(output))
}
