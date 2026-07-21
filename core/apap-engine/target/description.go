// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package target

type OsInfo struct {
	OSFamily      string
	OSDescription string
	KernelVersion string
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

type Description struct {
	Os             OsInfo
	PrimaryCPUName string
	CPUs           []CPUDescription
	ClusterInfo    []ClusterDescription
}
