// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package systeminfo

import (
	"fmt"
	"runtime"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/atperf-agent/privilege"
)

type CPUTopologyProvider interface {
	GetCPUTopology() (CPUTopology, error)
}

type SystemInfo interface {
	ListProcesses() ([]ProcessInfo, error)
	GetKernelVersion() (string, error)
	GetOSDescription() (string, error)

	CPUTopologyProvider
}

type ProcessInfo struct {
	Pid     int32
	User    string
	Name    string
	CmdLine string
}

type TargetInfo struct {
	PlatformType  PlatformType
	OSDescription string
	KernelVersion string
	IsRoot        bool
	CPUTopology   CPUTopology
}

type PlatformType struct {
	OS   OSType
	Arch ArchType
}

type CPUDescription struct {
	CoreNumber uint32
	ClusterID  uint32
	Midr       string
	Name       string
}

type ClusterDescription struct {
	ClusterID uint32
	Name      string
}

type CPUTopology struct {
	PrimaryCPUName string
	CPUs           []CPUDescription
	ClusterInfo    []ClusterDescription
}

// GetTargetInfo returns the target system information.
// Takes a platform-specific SystemInfo implementation
func GetTargetInfo(s SystemInfo, checker privilege.Checker) (*TargetInfo, error) {
	errMessage := fmt.Errorf("platform not supported")
	platform, err := GetPlatformType()
	if err != nil {
		return nil, fmt.Errorf("%v: %v", errMessage, err)
	}
	kernelVersion, err := s.GetKernelVersion()
	if err != nil {
		return nil, fmt.Errorf("%v: %v", errMessage, err)
	}
	privileged, err := checker.IsPrivileged()
	if err != nil {
		return nil, fmt.Errorf("%v: %v", errMessage, err)
	}
	osDesc, err := s.GetOSDescription()
	if err != nil {
		log.Warnf("Failed to get OS info: %v", err)
	}
	cpuTopology, err := s.GetCPUTopology()
	if err != nil {
		log.Warnf("Failed to get CPU topology: %v", err)
	}

	return &TargetInfo{
		PlatformType:  platform,
		OSDescription: osDesc,
		KernelVersion: kernelVersion,
		IsRoot:        privileged,
		CPUTopology:   cpuTopology,
	}, nil
}

// GetPlatformType returns the platform type based on the current runtime environment.
func GetPlatformType() (PlatformType, error) {
	arch, err := ToArchType(runtime.GOARCH)
	if err != nil {
		return PlatformType{}, err
	}
	osType, err := ToOSType(runtime.GOOS)
	if err != nil {
		return PlatformType{}, err
	}
	return PlatformType{OS: osType, Arch: arch}, nil
}

type ArchType string

const (
	ArchAmd64 ArchType = "amd64"
	ArchArm64 ArchType = "arm64"
)

type OSType string

const (
	AndroidType OSType = "android"
	LinuxType   OSType = "linux"
	MacOSType   OSType = "darwin"
	WindowsType OSType = "windows"
)

// ToArchType converts a Go architecture string to an ArchType.
func ToArchType(goarch string) (ArchType, error) {
	switch goarch {
	case "amd64":
		return ArchAmd64, nil
	case "arm64":
		return ArchArm64, nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}

// ToOSType converts a Go operating system string to an OSType.
func ToOSType(goos string) (OSType, error) {
	switch goos {
	case "android":
		return AndroidType, nil
	case "linux":
		return LinuxType, nil
	case "darwin":
		return MacOSType, nil
	case "windows":
		return WindowsType, nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}
}

// systemInfoUnsupportedPlatformError constructs a standardized unsupported platform error for the given operation name.
func systemInfoUnsupportedPlatformError(operation string) error {
	return message.New(message.AgentSystemInfoUnsupportedPlatform).
		WithMetadata(map[string]string{"platform": runtime.GOOS}).
		WithCause(fmt.Errorf("SystemInfo.%s is not supported on %s", operation, runtime.GOOS))
}

// CommonSystemInfo provides a default stubbed implementation of SystemInfo.
type UnsupportedSystemInfo struct{}

func (*UnsupportedSystemInfo) ListProcesses() ([]ProcessInfo, error) {
	// Stubbed method.
	return nil, systemInfoUnsupportedPlatformError("ListProcesses")
}

func (*UnsupportedSystemInfo) GetKernelVersion() (string, error) {
	// Stubbed method.
	return "", systemInfoUnsupportedPlatformError("GetKernelVersion")
}

func (*UnsupportedSystemInfo) GetOSDescription() (string, error) {
	// Stubbed method.
	return "", systemInfoUnsupportedPlatformError("GetOSInfo")
}

func (*UnsupportedSystemInfo) GetCPUTopology() (CPUTopology, error) {
	// Stubbed method.
	return CPUTopology{}, systemInfoUnsupportedPlatformError("GetCPUTopology")
}
