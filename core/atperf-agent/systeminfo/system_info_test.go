// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package systeminfo_test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo"
)

func TestSystemInfo(t *testing.T) {
	t.Run("GetPlatformType returns expected values", func(t *testing.T) {
		pt, err := systeminfo.GetPlatformType()
		assert.NoError(t, err)
		expectedArch, _ := systeminfo.ToArchType(runtime.GOARCH)
		expectedOS, _ := systeminfo.ToOSType(runtime.GOOS)
		assert.Equal(t, expectedArch, pt.Arch)
		assert.Equal(t, expectedOS, pt.OS)
	})

	t.Run("systeminfo.ToArchType returns correct ArchType", func(t *testing.T) {
		arch, err := systeminfo.ToArchType("amd64")
		assert.NoError(t, err)
		assert.Equal(t, systeminfo.ArchAmd64, arch)

		arch, err = systeminfo.ToArchType("arm64")
		assert.NoError(t, err)
		assert.Equal(t, systeminfo.ArchArm64, arch)

		_, err = systeminfo.ToArchType("unknown")
		assert.Error(t, err)
	})

	t.Run("systeminfo.ToOSType returns correct OSType", func(t *testing.T) {
		osType, err := systeminfo.ToOSType("android")
		assert.NoError(t, err)
		assert.Equal(t, systeminfo.AndroidType, osType)

		osType, err = systeminfo.ToOSType("linux")
		assert.NoError(t, err)
		assert.Equal(t, systeminfo.LinuxType, osType)

		osType, err = systeminfo.ToOSType("darwin")
		assert.NoError(t, err)
		assert.Equal(t, systeminfo.MacOSType, osType)

		osType, err = systeminfo.ToOSType("windows")
		assert.NoError(t, err)
		assert.Equal(t, systeminfo.WindowsType, osType)

		_, err = systeminfo.ToOSType("unknown")
		assert.Error(t, err)
	})

	t.Run("GetTargetInfo succeeds and returns expected info", func(t *testing.T) {
		expectedOsDesc := "OS Description"

		expectedCpuTopology := systeminfo.CPUTopology{
			PrimaryCPUName: "CPU Name",
			CPUs: []systeminfo.CPUDescription{
				{
					CoreNumber: 1,
					ClusterID:  2,
					Midr:       "Midr 1",
					Name:       "CPU 1",
				},
			},
			ClusterInfo: []systeminfo.ClusterDescription{
				{
					ClusterID: 2,
					Name:      "Cluster 2",
				},
			},
		}

		mockSystemInfo := &systeminfo.MockSystemInfo{}
		mockSystemInfo.On("GetKernelVersion").Return("1.0.0", nil)
		mockSystemInfo.On("GetOSDescription").Return(expectedOsDesc, nil)
		mockSystemInfo.On("GetCPUTopology").Return(expectedCpuTopology, nil)

		expectedArch, _ := systeminfo.ToArchType(runtime.GOARCH)
		expectedOS, _ := systeminfo.ToOSType(runtime.GOOS)

		checker := &privilege.MockChecker{}
		checker.On("IsPrivileged").Return(true, nil)

		info, err := systeminfo.GetTargetInfo(mockSystemInfo, checker)
		assert.NoError(t, err)
		assert.True(t, info.IsRoot)
		assert.Equal(t, info.KernelVersion, "1.0.0")
		assert.Equal(t, info.PlatformType.OS, expectedOS)
		assert.Equal(t, info.PlatformType.Arch, expectedArch)
		assert.Equal(t, expectedOsDesc, info.OSDescription)
		assert.Equal(t, expectedCpuTopology, info.CPUTopology)

		checker.AssertExpectations(t)
	})

	t.Run("GetTargetInfo fails when platform is unsupported", func(t *testing.T) {
		commonSystemInfo := &systeminfo.UnsupportedSystemInfo{}
		checker := &privilege.MockChecker{}
		_, err := systeminfo.GetTargetInfo(commonSystemInfo, checker)
		assert.ErrorContains(t, err, "is not supported on "+runtime.GOOS)
	})

	t.Run("GetOSDescription fails when platform is unsupported", func(t *testing.T) {
		commonSystemInfo := &systeminfo.UnsupportedSystemInfo{}
		_, err := commonSystemInfo.GetOSDescription()
		assert.ErrorContains(t, err, "is not supported on "+runtime.GOOS)
	})

	t.Run("GetCPUTopology fails when platform is unsupported", func(t *testing.T) {
		commonSystemInfo := &systeminfo.UnsupportedSystemInfo{}
		_, err := commonSystemInfo.GetCPUTopology()
		assert.ErrorContains(t, err, "is not supported on "+runtime.GOOS)
	})
}
