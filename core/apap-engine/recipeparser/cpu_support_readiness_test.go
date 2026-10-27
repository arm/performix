// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/telemetry"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

func TestCPUCoreSupportReadiness(t *testing.T) {
	targets := []struct {
		name     string
		cpus     []target.CPUDescription
		support  string
		metadata map[string]string
	}{
		{
			name:    "all cores supported with one CPU type",
			cpus:    []target.CPUDescription{{CoreNumber: 0, Name: "Neoverse-N1"}, {CoreNumber: 1, Name: "Neoverse-N1"}},
			support: "full",
		},
		{
			name:    "all cores supported with different CPU types",
			cpus:    []target.CPUDescription{{CoreNumber: 0, Name: "Neoverse-N1"}, {CoreNumber: 1, Name: "Neoverse-V2"}},
			support: "full",
		},
		{
			name:     "unsupported core before supported core",
			cpus:     []target.CPUDescription{{CoreNumber: 0, Name: "Unsupported-A"}, {CoreNumber: 1, Name: "Neoverse-N1"}},
			support:  "partial",
			metadata: map[string]string{"cpuNames": "Unsupported-A", "coreNumbers": "0"},
		},
		{
			name:     "unsupported core after supported core",
			cpus:     []target.CPUDescription{{CoreNumber: 0, Name: "Neoverse-N1"}, {CoreNumber: 1, Name: "Unsupported-A"}},
			support:  "partial",
			metadata: map[string]string{"cpuNames": "Unsupported-A", "coreNumbers": "1"},
		},
		{
			name: "multiple unsupported types and repeated cores in unsorted topology",
			cpus: []target.CPUDescription{
				{CoreNumber: 4, Name: "Unsupported-B"},
				{CoreNumber: 3, Name: "Unsupported-A"},
				{CoreNumber: 2, Name: "Neoverse-V2"},
				{CoreNumber: 1, Name: "Neoverse-N1"},
				{CoreNumber: 0, Name: "Unsupported-A"},
			},
			support:  "partial",
			metadata: map[string]string{"cpuNames": "Unsupported-A, Unsupported-B", "coreNumbers": "0, 3, 4"},
		},
		{
			name:    "no supported cores",
			cpus:    []target.CPUDescription{{CoreNumber: 0, Name: "Unsupported-A"}, {CoreNumber: 1, Name: "Unsupported-B"}},
			support: "none",
		},
		{
			name:    "no core topology",
			cpus:    []target.CPUDescription{},
			support: "none",
		},
	}

	// Internal builds resolve this CPU to generic Cortex telemetry. It is still
	// supported for the readiness check, even without a CPU-specific spec.
	if _, supported := telemetry.GetSpecification("Cortex-A76"); supported {
		targets[1].cpus = append(targets[1].cpus, target.CPUDescription{CoreNumber: 2, Name: "Cortex-A76"})
	}

	for _, recipeName := range []string{"cpu_microarchitecture", "instruction_mix"} {
		modes := []string{""}
		if recipeName == "instruction_mix" {
			modes = []string{"dynamic", "both", "static"}
		}
		for _, mode := range modes {
			for _, tt := range targets {
				t.Run(recipeName+"/"+mode+"/"+tt.name, func(t *testing.T) {
					params := map[string]any{"metrics_group": []string{"topdown_l1"}}
					if recipeName == "instruction_mix" {
						params = map[string]any{"mode": mode}
					}
					readiness := executeRecipeReadiness(t, "core/apap-cli/recipes/"+recipeName+".js", params, &target.Description{
						Os:             target.OsInfo{OSFamily: "Linux"},
						PrimaryCPUName: "Unsupported-A",
						CPUs:           tt.cpus,
					}, recipeName != "cpu_microarchitecture" || tt.support != "none")

					if tt.support == "full" || mode == "static" {
						assert.Equal(t, recipe.ReadyStatusReady, readiness.Status)
						assert.Empty(t, readiness.Advice)
						return
					}

					expectedStatus := recipe.ReadyStatusWarning
					expectedCode := "recipes." + recipeName + ".PARTIAL_CPU_SUPPORT"
					metadata := tt.metadata
					if tt.support == "none" {
						expectedCode = "recipes." + recipeName + ".TELEMETRY_SPECIFICATION_UNAVAILABLE"
						metadata = map[string]string{"cpuName": "Unsupported-A"}
						expectedStatus = recipe.ReadyStatusError
					}
					assert.Equal(t, expectedStatus, readiness.Status)
					require.Len(t, readiness.Advice, 1)
					advice := readiness.Advice[0]
					assert.Equal(t, expectedStatus, advice.AdviceSeverity)
					assert.Equal(t, expectedCode, advice.AdviceMessage.Code())
					assert.Equal(t, metadata, advice.AdviceMessage.Metadata())
					assert.NoError(t, message.ValidateMetadataPlaceholders(advice.AdviceMessage))
				})
			}
		}
	}
}

func TestPartialCPUCoreSupportPreservesToolErrors(t *testing.T) {
	for _, mode := range []string{"cpu_microarchitecture", "dynamic", "both"} {
		t.Run(mode, func(t *testing.T) {
			recipeName := "instruction_mix"
			params := map[string]any{"mode": mode}
			if mode == "cpu_microarchitecture" {
				recipeName = mode
				params = map[string]any{"metrics_group": []string{"topdown_l1"}}
			}
			const toolErrorCode = "tool_integrations.common.TOOL_NOT_DEPLOYED"
			readiness := executeRecipeReadiness(t, "core/apap-cli/recipes/"+recipeName+".js", params, &target.Description{
				Os:             target.OsInfo{OSFamily: "Linux"},
				PrimaryCPUName: "Neoverse-N1",
				CPUs:           []target.CPUDescription{{CoreNumber: 0, Name: "Neoverse-N1"}, {CoreNumber: 1, Name: "Unsupported-A"}},
			}, true, tool.ProbeAdvice{
				Level:       recipe.AdviceSeverityError,
				MessageCode: toolErrorCode,
				Metadata:    map[string]string{"tool": "sl-record"},
			})

			assert.Equal(t, recipe.ReadyStatusError, readiness.Status)
			require.Len(t, readiness.Advice, 2)
			assert.Equal(t, toolErrorCode, readiness.Advice[0].AdviceMessage.Code())
			assert.Equal(t, recipe.AdviceSeverityError, readiness.Advice[0].AdviceSeverity)
			assert.Equal(t, "recipes."+recipeName+".PARTIAL_CPU_SUPPORT", readiness.Advice[1].AdviceMessage.Code())
			assert.Equal(t, recipe.AdviceSeverityWarning, readiness.Advice[1].AdviceSeverity)
		})
	}
}
