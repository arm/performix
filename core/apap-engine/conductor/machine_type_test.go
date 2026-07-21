// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"errors"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

func TestIdentifyTarget(t *testing.T) {

	unix_identify_command := "uname -a"
	windows_identify_command := "echo %OS% && echo %PROCESSOR_ARCHITECTURE%"

	t.Run("valid input successfully identifies architecture and OS", func(t *testing.T) {
		type successfulTestData struct {
			input      string
			targetType PlatformConfiguration
		}

		identifyTestData := []successfulTestData{
			{
				input:      "Linux e134643 6.8.0-47-generic #47~22.04.1-Ubuntu SMP PREEMPT_DYNAMIC Wed Oct  2 16:16:55 UTC 2 x86_64 x86_64 x86_64 GNU/Linux",
				targetType: PlatformConfiguration{OS: Linux, Architecture: X86_64},
			},
			{
				input:      "Linux ip-10-252-38-102.eu-west-1.compute.internal 6.1.61-85.141.amzn2023.aarch64 #1 SMP Wed Nov  8 00:38:50 UTC 2023 aarch64 aarch64 aarch64 GNU/Linux",
				targetType: PlatformConfiguration{OS: Linux, Architecture: AArch64},
			},
			{
				input:      "Darwin VDW30FN91H 23.6.0 Darwin Kernel Version 23.6.0: Mon Jul 29 21:14:30 PDT 2024; root:xnu-10063.141.2~1/RELEASE_ARM64_T6030 arm64",
				targetType: PlatformConfiguration{OS: Darwin, Architecture: AArch64},
			},
			{
				input:      "Windows_NT\nx86_64",
				targetType: PlatformConfiguration{OS: Win, Architecture: X86_64},
			},
			{
				input:      "Windows_NT\nARM64",
				targetType: PlatformConfiguration{OS: Win, Architecture: AArch64},
			},
		}

		for _, testData := range identifyTestData {
			mcr := &MockCommandRunner{}
			mcr.On("RunCommand", unix_identify_command).Return(testData.input, "", nil)
			mcr.On("RunCommand", windows_identify_command).Return(testData.input, "", nil)
			tt, err := IdentifyTarget(mcr, "")
			assert.NoError(t, err)
			assert.Equal(t, testData.targetType.Architecture, tt.Architecture)
			assert.Equal(t, testData.targetType.OS, tt.OS)
		}
	})

	t.Run("android connection type identifies Android without running commands", func(t *testing.T) {
		mcr := &MockCommandRunner{}

		tt, err := IdentifyTarget(mcr, "android")

		assert.NoError(t, err)
		assert.Equal(t, AArch64, tt.Architecture)
		assert.Equal(t, Android, tt.OS)
		mcr.AssertNotCalled(t, "RunCommand", unix_identify_command)
		mcr.AssertNotCalled(t, "RunCommand", windows_identify_command)
	})

	t.Run("local connection type identifies platform", func(t *testing.T) {
		mcr := &MockCommandRunner{}

		tt, err := IdentifyTarget(mcr, "local")

		require.NoError(t, err)
		switch runtime.GOOS {
		case "linux":
			assert.Equal(t, Linux, tt.OS)
		case "darwin":
			assert.Equal(t, Darwin, tt.OS)
		case "windows":
			assert.Equal(t, Win, tt.OS)
		default:
			t.Fatalf("unsupported runtime GOOS in test: %s", runtime.GOOS)
		}
		switch runtime.GOARCH {
		case "amd64":
			assert.Equal(t, X86_64, tt.Architecture)
		case "arm64":
			assert.Equal(t, AArch64, tt.Architecture)
		default:
			t.Fatalf("unsupported runtime GOARCH in test: %s", runtime.GOARCH)
		}
		mcr.AssertNotCalled(t, "RunCommand", unix_identify_command)
		mcr.AssertNotCalled(t, "RunCommand", windows_identify_command)
	})

	t.Run("Unknown architecture produces error", func(t *testing.T) {
		cmdReturnString := "Linux e134643 6.8.0-47-generic #47~22.04.1-Ubuntu SMP PREEMPT_DYNAMIC Wed Oct  2 16:16:55 UTC 2 x87_64 x87_64 x87_64 GNU/Linux"
		mcr := &MockCommandRunner{}
		mcr.On("RunCommand", unix_identify_command).Return(cmdReturnString, "", nil)

		_, err := IdentifyTarget(mcr, "")

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorMachineTypeIdentifyInfoFromOutput, msgErr.Code())
		assert.Equal(t, cmdReturnString, msgErr.Metadata()["stdOut"])
	})

	t.Run("Command failure error handled", func(t *testing.T) {
		rektError := errors.New("rekt")
		mcr := &MockCommandRunner{}
		mcr.On("RunCommand", unix_identify_command).Return("", "", rektError)
		mcr.On("RunCommand", windows_identify_command).Return("", "", rektError)
		_, err := IdentifyTarget(mcr, "")

		var msgErr message.Message
		ok := errors.As(err, &msgErr)
		assert.True(t, ok)
		assert.Equal(t, message.EngineConductorMachineTypeRunMachineInfoCmd, msgErr.Code())
		assert.Contains(t, msgErr.Unwrap().Error(), "rekt")
	})
}

func TestGetTargetArchitecture(t *testing.T) {
	t.Run("rejects darwin target platforms by default", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "uname -a").Return(
			"Darwin host 23.6.0 Darwin Kernel Version 23.6.0: RELEASE_ARM64_T6030 arm64",
			"",
			nil,
		).Once()

		platformConfig, platform, err := GetTargetArchitecture(cmdRunner, NewAferoTargetFilesystem(afero.NewMemMapFs()), "", TargetSupported)
		require.Error(t, err)
		require.Nil(t, platform)
		assert.Equal(t, PlatformConfiguration{OS: Darwin, Architecture: AArch64}, platformConfig)
		cmdRunner.AssertExpectations(t)
	})

	t.Run("rejects windows x86_64 target platforms by default", func(t *testing.T) {
		cmdRunner := &MockCommandRunner{}
		cmdRunner.On("RunCommand", "uname -a").Return("", "", errors.New("uname failed")).Once()
		cmdRunner.On("RunCommand", "echo %OS% && echo %PROCESSOR_ARCHITECTURE%").Return("Windows_NT\nx86_64", "", nil).Once()

		platformConfig, platform, err := GetTargetArchitecture(cmdRunner, NewAferoTargetFilesystem(afero.NewMemMapFs()), "", TargetSupported)
		require.Error(t, err)
		require.Nil(t, platform)
		assert.Equal(t, PlatformConfiguration{OS: Win, Architecture: X86_64}, platformConfig)
		cmdRunner.AssertExpectations(t)
	})
}

func TestBuildTargetPlatform(t *testing.T) {
	t.Run("builds darwin target platform for aarch64 hosts", func(t *testing.T) {
		platform, err := buildTargetPlatform(
			PlatformConfiguration{OS: Darwin, Architecture: AArch64},
			HostSupported,
			&MockCommandRunner{},
			NewAferoTargetFilesystem(afero.NewMemMapFs()),
		)
		require.NoError(t, err)
		require.NotNil(t, platform)
		assert.Equal(t, PlatformConfiguration{OS: Darwin, Architecture: AArch64}, platform.PlatformConfiguration)
		assert.IsType(t, &LinuxPathUtils{}, platform.Path)
		assert.IsType(t, &LinuxTargetActions{}, platform.Actions)
	})

	t.Run("rejects android for host-supported platforms", func(t *testing.T) {
		platform, err := buildTargetPlatform(
			PlatformConfiguration{OS: Android, Architecture: AArch64},
			HostSupported,
			&MockCommandRunner{},
			NewAferoTargetFilesystem(afero.NewMemMapFs()),
		)
		require.Error(t, err)
		require.Nil(t, platform)
	})

	t.Run("builds windows target platform for x86_64 hosts", func(t *testing.T) {
		platform, err := buildTargetPlatform(
			PlatformConfiguration{OS: Win, Architecture: X86_64},
			HostSupported,
			&MockCommandRunner{},
			NewAferoTargetFilesystem(afero.NewMemMapFs()),
		)
		require.NoError(t, err)
		require.NotNil(t, platform)
		assert.Equal(t, PlatformConfiguration{OS: Win, Architecture: X86_64}, platform.PlatformConfiguration)
		assert.IsType(t, &WindowsPathUtils{}, platform.Path)
		assert.IsType(t, &WindowsTargetActions{}, platform.Actions)
	})
}
