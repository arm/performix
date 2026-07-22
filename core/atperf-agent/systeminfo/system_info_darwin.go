// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package systeminfo

import (
	"os/exec"
	"strings"
)

type DarwinSystemInfo struct{}

func NewSystemInfo() SystemInfo {
	return &DarwinSystemInfo{}
}

func (d *DarwinSystemInfo) ListProcesses() ([]ProcessInfo, error) {
	return nil, systemInfoUnsupportedPlatformError("ListProcesses")
}

func (d *DarwinSystemInfo) SupportsAdmin() (bool, error) {
	return false, systemInfoUnsupportedPlatformError("SupportsAdmin")
}

func (d *DarwinSystemInfo) GetKernelVersion() (string, error) {
	out, err := exec.Command("uname", "-r").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *DarwinSystemInfo) GetOSDescription() (string, error) {
	nameOut, err := exec.Command("sw_vers", "-productName").CombinedOutput()
	if err != nil {
		return "", err
	}
	versionOut, err := exec.Command("sw_vers", "-productVersion").CombinedOutput()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(nameOut))
	version := strings.TrimSpace(string(versionOut))
	return strings.TrimSpace(name + " " + version), nil
}

func (d *DarwinSystemInfo) GetCPUTopology() (CPUTopology, error) {
	return CPUTopology{}, systemInfoUnsupportedPlatformError("GetCPUTopology")
}
