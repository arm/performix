// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package systeminfo

import (
	"strconv"
	"strings"

	armcpus "github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo/armcpus"
)

type cpuInfo struct {
	modelName      string
	cpuImplementer string
	cpuPart        string
}

func convertImplementer(implementer uint32, part uint32) uint32 {
	// Bare metal Cobalt 100 platforms may show a MIDR with Microsoft's implemeter code (0x6d) for the N2 CPU part number (d49).
	// This is a special case to map the implementor code back to Arm's, so the CPUs are correctly identified.
	if (implementer<<12)|part == 0x6dd49 {
		implementer = 0x41
	}
	return implementer
}

func resolveCPUName(info cpuInfo, midr string) string {
	rawMIDR, ok := parseUintField(midr)
	if ok {
		implementer := (rawMIDR >> 24) & 0xff
		part := (rawMIDR >> 4) & 0xfff
		converted := convertImplementer(implementer, part)
		if name := armcpus.CpuidToName[(converted<<12)|part]; name != "" {
			return name
		}
	}
	implementer, ok := parseUintField(info.cpuImplementer)
	if ok {
		part, ok := parseUintField(info.cpuPart)
		if ok {
			converted := convertImplementer(implementer, part)
			if name := armcpus.CpuidToName[(converted<<12)|part]; name != "" {
				return name
			}
		}
	}
	if info.modelName != "" {
		return info.modelName
	}
	return "Unknown CPU"
}

func parseUintField(value string) (uint32, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 0, 32)
	if err != nil {
		return 0, false
	}
	return uint32(parsed), true
}
