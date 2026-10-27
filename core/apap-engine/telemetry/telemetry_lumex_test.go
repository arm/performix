// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"embed"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/systeminfo/armcpus"
)

func TestLumexSpecificationsMatchDetectionAndConsumers(t *testing.T) {
	for _, model := range []string{"C1-Nano", "C1-Pro", "C1-Premium", "C1-Ultra"} {
		t.Run(model, func(t *testing.T) {
			specification, ok := GetSpecification(model)
			require.True(t, ok)
			payload, err := GetTelemetryData(model)
			require.NoError(t, err)

			var document struct {
				Product struct {
					Name        string  `json:"product_name"`
					Implementer string  `json:"implementer"`
					Part        string  `json:"part_num"`
					Slots       float64 `json:"num_slots"`
					BusSlots    float64 `json:"num_bus_slots"`
				} `json:"product_configuration"`
				Methodologies struct {
					Topdown struct {
						Grouping map[string][]string `json:"metric_grouping"`
						Tree     struct {
							Roots []string `json:"root_nodes"`
							Nodes []struct {
								Name     string   `json:"name"`
								Group    string   `json:"group"`
								Children []string `json:"next_items"`
							} `json:"metrics"`
						} `json:"decision_tree"`
					} `json:"topdown_methodology"`
				} `json:"methodologies"`
			}
			require.NoError(t, json.Unmarshal([]byte(specification.JSON), &document))
			assert.Equal(t, model, document.Product.Name)
			implementer, err := strconv.ParseUint(document.Product.Implementer, 0, 32)
			require.NoError(t, err)
			part, err := strconv.ParseUint(document.Product.Part, 0, 32)
			require.NoError(t, err)
			cpuid := implementer<<12 | part
			require.LessOrEqual(t, cpuid, uint64(^uint32(0)))
			lookupID := uint32(cpuid) // #nosec G115 -- range checked above
			assert.Equal(t, model, armcpus.CpuidToName[lookupID])

			// Evaluate with synthetic counters to check syntax and variable binding,
			// not hardware accuracy. Only declared events and constants are bound.
			for id, metric := range payload.Metrics {
				t.Run(id, func(t *testing.T) {
					vm := goja.New()
					require.NoError(t, vm.Set("num_slots", document.Product.Slots))
					require.NoError(t, vm.Set("num_bus_slots", document.Product.BusSlots))
					for _, event := range metric.Events {
						require.Contains(t, payload.Events, event)
						require.NoError(t, vm.Set(event, 100))
					}
					for _, event := range metric.Samples {
						require.Contains(t, payload.Events, event)
					}
					_, err := vm.RunString(metric.Formula)
					require.NoError(t, err, "invalid formula %q", metric.Formula)
				})
			}

			for _, group := range payload.Groups.Metrics {
				for _, metric := range group.Metrics {
					require.Contains(t, payload.Metrics, metric)
				}
			}
			topdown := document.Methodologies.Topdown
			require.NotEmpty(t, topdown.Grouping["stage_1"])
			require.NotEmpty(t, topdown.Grouping["stage_2"])
			for _, groups := range topdown.Grouping {
				for _, group := range groups {
					require.Contains(t, payload.Groups.Metrics, group)
				}
			}
			children := map[string][]string{}
			for _, node := range topdown.Tree.Nodes {
				require.NotContains(t, children, node.Name, "duplicate decision node")
				require.Contains(t, payload.Metrics, node.Name)
				require.Contains(t, payload.Groups.Metrics, node.Group)
				assert.Contains(t, payload.Groups.Metrics[node.Group].Metrics, node.Name)
				children[node.Name] = node.Children
			}
			// The GUI recursively expands decision nodes and treats other children
			// as metric groups. Missing references or cycles would break the graph.
			var visit func(string, map[string]bool)
			visit = func(name string, ancestors map[string]bool) {
				require.False(t, ancestors[name], "methodology cycle at %s", name)
				next, decision := children[name]
				if !decision {
					require.Contains(t, payload.Groups.Metrics, name)
					return
				}
				ancestors[name] = true
				for _, child := range next {
					visit(child, ancestors)
				}
				delete(ancestors, name)
			}
			require.NotEmpty(t, topdown.Tree.Roots)
			for _, name := range topdown.Tree.Roots {
				require.Contains(t, children, name)
			}
			for name := range children {
				visit(name, map[string]bool{})
			}
		})
	}
}

//go:embed "data/public/c1-*.json"
var lumexSpecificationFiles embed.FS

func TestAllVendoredLumexSpecificationsArePublic(t *testing.T) {
	entries, err := lumexSpecificationFiles.ReadDir("data/public")
	require.NoError(t, err)
	require.Len(t, entries, 10)
	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			contents, err := lumexSpecificationFiles.ReadFile("data/public/" + entry.Name())
			require.NoError(t, err)
			var metadata struct {
				Document struct {
					Confidential bool `json:"confidential"`
				} `json:"document"`
			}
			require.NoError(t, json.Unmarshal(contents, &metadata))
			assert.False(t, metadata.Document.Confidential)
			assertSpecificationContainsTelemetryData(t, string(contents))
		})
	}
}

func TestVendoredSME2IsNotRegisteredAsACPU(t *testing.T) {
	_, ok := GetSpecification("C1-SME2")
	assert.False(t, ok)
	assert.NotContains(t, SupportedCPUModels(), "C1-SME2")
}
