// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

//go:embed "data/public/neoverse-n1.json"
var NeoverseN1JSON string

//go:embed "data/public/neoverse-n2.json"
var NeoverseN2JSON string

//go:embed "data/public/neoverse-n3.json"
var NeoverseN3JSON string

//go:embed "data/public/neoverse-v1.json"
var NeoverseV1JSON string

//go:embed "data/public/neoverse-v2.json"
var NeoverseV2JSON string

//go:embed "data/public/neoverse-v3.json"
var NeoverseV3JSON string

var telemetryDataByCPUModel = map[string]string{
	"Neoverse-N1":   NeoverseN1JSON,
	"Neoverse-N2":   NeoverseN2JSON,
	"Neoverse-N3":   NeoverseN3JSON,
	"Neoverse-V1":   NeoverseV1JSON,
	"Neoverse-V2":   NeoverseV2JSON,
	"Neoverse-V3":   NeoverseV3JSON,
	"Neoverse-V3AE": NeoverseV3JSON,
}

var telemetryDataParsed = sync.Map{}

type Metric struct {
	Title       string   `json:"title"`
	Formula     string   `json:"formula"`
	Description string   `json:"description"`
	Units       string   `json:"units"`
	Events      []string `json:"events"`
	Samples     []string `json:"sample_events"`
}

type Event struct {
	Code          string `json:"code"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Common        bool   `json:"common"`
	Architectural bool   `json:"architectural"`
}

type Groups struct {
	Metrics map[string]MetricGroup `json:"metrics"`
}

type MetricGroup struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Metrics     []string `json:"metrics"`
}

// Payload is the telemetry JSON structure.
type Payload struct {
	Events  map[string]Event  `json:"events"`
	Metrics map[string]Metric `json:"metrics"`
	Groups  Groups            `json:"groups"`
}

// Specification contains a supported CPU model and its complete telemetry JSON.
type Specification struct {
	CPUModel string
	JSON     string
}

func (p *Payload) FindMetricByName(name string) (string, Metric, bool) {
	needle := strings.ToLower(name)
	for id, m := range p.Metrics {
		if strings.EqualFold(strings.TrimSpace(m.Title), needle) {
			return id, m, true
		}
	}
	return "", Metric{}, false
}

func (p *Payload) GetGroupNamesByMetricID(metricID string) []string {
	var groups []string
	for name, g := range p.Groups.Metrics {
		for _, m := range g.Metrics {
			if m == metricID {
				groups = append(groups, name)
				break
			}
		}
	}
	return groups
}

func ParseTelemetryJSON(jsonStr string) (*Payload, error) {
	var tp Payload
	if err := json.Unmarshal([]byte(jsonStr), &tp); err != nil {
		return nil, err
	}
	return &tp, nil
}

// SupportedCPUModels returns the CPU models with embedded telemetry.
func SupportedCPUModels() []string {
	cortexModels := cortexCPUModels()
	models := make([]string, 0, len(telemetryDataByCPUModel)+len(cortexModels))
	for model := range telemetryDataByCPUModel {
		models = append(models, model)
	}
	models = append(models, cortexModels...)
	sort.Strings(models)
	return models
}

// GetSpecification returns the complete telemetry specification for a supported CPU model.
func GetSpecification(cpuModel string) (Specification, bool) {
	jsonStr, ok := telemetryDataByCPUModel[cpuModel]
	if !ok {
		jsonStr, ok = resolveCortex(cpuModel)
	}
	if !ok {
		return Specification{}, false
	}

	return Specification{CPUModel: cpuModel, JSON: jsonStr}, true
}

func GetTelemetryData(cpuName string) (*Payload, error) {
	specification, ok := GetSpecification(cpuName)
	if !ok {
		return nil, nil
	}

	if val, ok := telemetryDataParsed.Load(specification.CPUModel); ok {
		return val.(*Payload), nil
	}
	tp, err := ParseTelemetryJSON(specification.JSON)
	if err != nil {
		return nil, err
	}
	telemetryDataParsed.Store(specification.CPUModel, tp)
	return tp, nil
}

// GetGroupDescriptionByName returns the description for a group by its name
func (p *Payload) GetGroupDescriptionByName(groupName string) (string, bool) {
	if group, exists := p.Groups.Metrics[groupName]; exists {
		return group.Description, true
	}
	return "", false
}
