// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package systeminfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveCPUName(t *testing.T) {
	t.Run("uses MIDR-derived cpuid when available", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{
			modelName:      "Generic CPU",
			cpuImplementer: "0x41",
			cpuPart:        "0xd0c",
		}, "0x413fd4f1")
		assert.Equal(t, "Neoverse-V2", name)
	})

	t.Run("falls back to implementer and part before model name", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{
			modelName:      "Generic CPU",
			cpuImplementer: "0x41",
			cpuPart:        "0xd4f",
		}, "")
		assert.Equal(t, "Neoverse-V2", name)
	})

	t.Run("maps the Microsoft Cobalt 100 MIDR to Neoverse-N2", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{}, "0x000000006d0fd490")
		assert.Equal(t, "Neoverse-N2", name)
	})

	t.Run("maps the Microsoft Cobalt 100 cpuinfo fields to Neoverse-N2", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{
			modelName:      "Generic CPU",
			cpuImplementer: "0x6d",
			cpuPart:        "0xd49",
		}, "")
		assert.Equal(t, "Neoverse-N2", name)
	})

	t.Run("falls back to model name when cpuid lookup data is unavailable", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{
			modelName: "Vendor CPU String",
		}, "")
		assert.Equal(t, "Vendor CPU String", name)
	})

	t.Run("falls back to unknown cpu when no name source is available", func(t *testing.T) {
		name := resolveCPUName(cpuInfo{}, "")
		assert.Equal(t, "Unknown CPU", name)
	})
}
