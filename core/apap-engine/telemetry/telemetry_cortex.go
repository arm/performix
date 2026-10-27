// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package telemetry

import "embed"

//go:embed "data/public/cortex-a725.json"
var cortexA725JSON string

//go:embed "data/public/cortex-x925.json"
var cortexX925JSON string

//go:embed "data/public/c1-nano.json"
var c1NanoJSON string

//go:embed "data/public/c1-pro.json"
var c1ProJSON string

//go:embed "data/public/c1-premium.json"
var c1PremiumJSON string

//go:embed "data/public/c1-ultra.json"
var c1UltraJSON string

// The wildcard also matches public specifications after the generic one is
// excluded from the open-source mirror, so both source trees compile.
//
//go:embed data/public/*cortex*.json
var publicTelemetryFiles embed.FS

var genericCortexJSON = func() string {
	contents, err := publicTelemetryFiles.ReadFile("data/public/generic-cortex.json")
	if err != nil {
		return ""
	}
	return string(contents)
}()

var genericCortexCPUModels = func() map[string]struct{} {
	if genericCortexJSON == "" {
		return nil
	}
	return map[string]struct{}{
		"Cortex-A55":    {},
		"Cortex-A76":    {},
		"Cortex-A76AE":  {},
		"Cortex-A77":    {},
		"Cortex-A78":    {},
		"Cortex-A78C":   {},
		"Cortex-A510":   {},
		"Cortex-A710":   {},
		"Cortex-A715":   {},
		"Cortex-A520":   {},
		"Cortex-A720":   {},
		"Cortex-A720AE": {},
		"Cortex-X1":     {},
		"Cortex-X1C":    {},
		"Cortex-X2":     {},
		"Cortex-X3":     {},
		"Cortex-X4":     {},
	}
}()

var cortexTelemetryDataByCPUModel = map[string]string{
	"C1-Nano":    c1NanoJSON,
	"C1-Pro":     c1ProJSON,
	"C1-Premium": c1PremiumJSON,
	"C1-Ultra":   c1UltraJSON,

	"Cortex-A725": cortexA725JSON,
	"Cortex-X925": cortexX925JSON,
}

func cortexCPUModels() []string {
	models := make([]string, 0, len(cortexTelemetryDataByCPUModel)+len(genericCortexCPUModels))
	for model := range cortexTelemetryDataByCPUModel {
		models = append(models, model)
	}
	for model := range genericCortexCPUModels {
		models = append(models, model)
	}
	return models
}

func resolveCortex(cpuModel string) (string, bool) {
	if specification, ok := cortexTelemetryDataByCPUModel[cpuModel]; ok {
		return specification, true
	}
	if _, ok := genericCortexCPUModels[cpuModel]; ok {
		return genericCortexJSON, true
	}
	return "", false
}
