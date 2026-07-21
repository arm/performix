// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

type Architecture string

const (
	AArch64 Architecture = "aarch64"
	X86_64  Architecture = "x86_64" //nolint:all
)

func (arch Architecture) GetNameAliases() []string {
	switch arch {
	case AArch64:
		// Common aliases for ARM64 architecture (Linux, Mac M1/M2, Windows)
		return []string{"aarch64", "arm64", "ARM64"}
	case X86_64:
		// Common aliases for x86_64 architecture (Linux & MacOS, Windows)
		return []string{"x86_64", "AMD64"}
	default:
		return []string{}
	}
}

type OS string

const (
	Linux   OS = "Linux"
	Darwin  OS = "Darwin"
	Win     OS = "Windows"
	Android OS = "Android"
)

func (os OS) IsSupported() bool {
	switch os {
	case Linux, Win, Android:
		return true
	}
	return false
}

type MachineType int

const (
	MachineTypeUnknown MachineType = iota
	MachineTypeContainer
	MachineTypeVM
	MachineTypeBareMetal
)

type PlatformConfiguration struct {
	Architecture Architecture
	OS           OS
}

type PlatformGate int

const (
	TargetSupported PlatformGate = iota
	HostSupported
)

var PlatformCombinations []PlatformConfiguration = []PlatformConfiguration{
	{Architecture: AArch64, OS: Linux},
	{Architecture: X86_64, OS: Linux},
	{Architecture: AArch64, OS: Win},
	{Architecture: X86_64, OS: Win},
	{Architecture: AArch64, OS: Darwin},
	{Architecture: X86_64, OS: Darwin},
	{Architecture: AArch64, OS: Android},
}

func IdentifyTarget(cmdRunner CommandRunner, targetConnectionType string) (PlatformConfiguration, error) {
	// if the connection type is Android we know it must be an Android target
	if targetConnectionType == "android" {
		return PlatformConfiguration{Architecture: AArch64, OS: Android}, nil
	}
	if targetConnectionType == "local" {
		return identifyLocalTarget()
	}

	tt := PlatformConfiguration{}
	stdOut, _, err := cmdRunner.RunCommand("uname -a")
	if err != nil {
		// Treat it as a Windows platform if uname fails
		var winError error
		stdOut, _, winError = cmdRunner.RunCommand("echo %OS% && echo %PROCESSOR_ARCHITECTURE%")
		if winError != nil {
			return tt, message.New(message.EngineConductorMachineTypeRunMachineInfoCmd).WithCause(fmt.Errorf(
				"unix command failure: %v; Windows command failure: %v", err, winError))
		}
	}

	for _, arch := range []Architecture{AArch64, X86_64} {
		for _, alias := range arch.GetNameAliases() {
			if strings.Contains(stdOut, alias) {
				tt.Architecture = arch
				break
			}
		}
	}

	for _, os := range []OS{Linux, Darwin, Win} {
		if strings.Contains(stdOut, string(os)) {
			tt.OS = os
			break
		}
	}

	if (tt.Architecture == "") || (tt.OS == "") {
		return tt, message.New(message.EngineConductorMachineTypeIdentifyInfoFromOutput).WithMetadata(map[string]string{"stdOut": stdOut})
	}

	return tt, nil
}

func identifyLocalTarget() (PlatformConfiguration, error) {
	platform := PlatformConfiguration{}

	switch runtime.GOOS {
	case "linux":
		platform.OS = Linux
	case "darwin":
		platform.OS = Darwin
	case "windows":
		platform.OS = Win
	}

	switch runtime.GOARCH {
	case "amd64":
		platform.Architecture = X86_64
	case "arm64":
		platform.Architecture = AArch64
	}

	if platform.OS == "" || platform.Architecture == "" {
		return PlatformConfiguration{}, message.New(message.EngineConductorMachineTypeIdentifyInfoFromOutput).WithMetadata(map[string]string{
			"stdOut": fmt.Sprintf("GOOS=%s GOARCH=%s", runtime.GOOS, runtime.GOARCH),
		})
	}

	return platform, nil
}

// unsupportedPlatformForGate returns the appropriate error for an unsupported platform configuration and platform gate.
func unsupportedPlatformForGate(platformConfiguration PlatformConfiguration, gate PlatformGate) error {
	if gate == HostSupported {
		return message.New(message.EngineCommonHostPlatformFunctionalityMissing).WithMetadata(map[string]string{
			"arch": string(platformConfiguration.Architecture),
			"os":   string(platformConfiguration.OS),
		})
	}

	return message.New(message.EngineCommonUnsupportedTargetPlatform).WithMetadata(map[string]string{
		"arch": string(platformConfiguration.Architecture),
		"os":   string(platformConfiguration.OS),
	})
}

// buildTargetPlatform constructs a TargetPlatform for the provided platform configuration.
func buildTargetPlatform(platformConfiguration PlatformConfiguration, gate PlatformGate, cmdRunner CommandRunner, fs TargetFilesystem) (*TargetPlatform, error) {
	if err := ValidatePlatformGateSupport(platformConfiguration, gate); err != nil {
		return nil, err
	}

	switch platformConfiguration.OS {
	case Linux:
		return NewLinuxTargetPlatform(cmdRunner, fs, platformConfiguration.Architecture), nil
	case Darwin:
		return NewDarwinTargetPlatform(cmdRunner, fs, platformConfiguration.Architecture), nil
	case Android:
		return NewAndroidTargetPlatform(cmdRunner, fs), nil
	case Win:
		return NewWindowsTargetPlatform(cmdRunner, fs, platformConfiguration.Architecture), nil
	default:
		return nil, unsupportedPlatformForGate(platformConfiguration, gate)
	}
}

// IsSupportedForPlatformGate reports whether the given platform configuration is supported for the specified platform gate.
func IsSupportedForPlatformGate(platformConfiguration PlatformConfiguration, gate PlatformGate) bool {
	switch gate {
	case TargetSupported:
		return isTargetSupportedPlatform(platformConfiguration)
	case HostSupported:
		return isHostSupportedPlatform(platformConfiguration)
	default:
		return false
	}
}

// isTargetSupportedPlatform reports whether the given platform configuration is a supported target platform.
func isTargetSupportedPlatform(platformConfiguration PlatformConfiguration) bool {
	switch platformConfiguration.OS {
	case Linux:
		return platformConfiguration.Architecture == AArch64 || platformConfiguration.Architecture == X86_64
	case Android, Win:
		return platformConfiguration.Architecture == AArch64
	default:
		return false
	}
}

// isHostSupportedPlatform reports whether the given platform configuration is a supported host platform.
func isHostSupportedPlatform(platformConfiguration PlatformConfiguration) bool {
	switch platformConfiguration.OS {
	case Linux, Darwin, Win:
		return platformConfiguration.Architecture == AArch64 || platformConfiguration.Architecture == X86_64
	default:
		return false
	}
}

// ValidatePlatformGateSupport returns the appropriate error if the given platform configuration is not supported for the specified platform gate.
func ValidatePlatformGateSupport(platformConfiguration PlatformConfiguration, gate PlatformGate) error {
	if IsSupportedForPlatformGate(platformConfiguration, gate) {
		return nil
	}
	return unsupportedPlatformForGate(platformConfiguration, gate)
}

// GetTargetArchitecture identifies the architecture the client is connected to and constructs the appropriate target platform
// It returns an error if the target platform is not supported by the requested platform gate.
func GetTargetArchitecture(cmdRunner CommandRunner, fs TargetFilesystem, targetType string, gate PlatformGate) (PlatformConfiguration, *TargetPlatform, error) {
	platformConfiguration, err := IdentifyTarget(cmdRunner, targetType)
	if err != nil {
		return platformConfiguration, nil, err
	}

	targetPlatform, err := buildTargetPlatform(platformConfiguration, gate, cmdRunner, fs)
	return platformConfiguration, targetPlatform, err
}
