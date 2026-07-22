// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

//go:build android

package systeminfo

import (
	"os/exec"
	"strings"

	"github.com/spf13/afero"
)

type AndroidSystemInfo struct {
	cpuTopology CPUTopologyProvider
}

func NewSystemInfo() SystemInfo {
	return &AndroidSystemInfo{cpuTopology: NewSysfsCPUTopologyProvider(afero.NewOsFs())}
}

func (d *AndroidSystemInfo) GetOSDescription() (string, error) {
	descriptionOut, err := exec.Command("getprop", "ro.build.description").Output()
	if err != nil {
		return "", err
	}
	description := strings.TrimSpace(string(descriptionOut))
	return description, nil
}

func (d *AndroidSystemInfo) ListProcesses() ([]ProcessInfo, error) {
	return nil, systemInfoUnsupportedPlatformError("ListProcesses")
}

func (d *AndroidSystemInfo) GetKernelVersion() (string, error) {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *AndroidSystemInfo) GetCPUTopology() (CPUTopology, error) {
	return d.cpuTopology.GetCPUTopology()
}
