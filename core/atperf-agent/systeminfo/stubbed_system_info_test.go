// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package systeminfo

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func getExpectedUnsupportedError(methodName string) string {
	return fmt.Sprintf("SystemInfo.%s is not supported on %s", methodName, runtime.GOOS)
}

func TestCommonSystemInfo(t *testing.T) {
	s := NewSystemInfo()
	t.Run("ListProcesses errors with unsupported platform", func(t *testing.T) {
		processes, err := s.ListProcesses()
		assert.ErrorContains(t, err, getExpectedUnsupportedError("ListProcesses"))
		assert.Nil(t, processes)
	})
	t.Run("GetKernelVersion returns value", func(t *testing.T) {
		version, err := s.GetKernelVersion()
		assert.NoError(t, err)
		assert.NotEmpty(t, version)
	})
	t.Run("GetOSDescription returns value", func(t *testing.T) {
		desc, err := s.GetOSDescription()
		assert.NoError(t, err)
		assert.NotEmpty(t, desc)
	})
}
