// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
)

var (
	idA = "renderer-a"
	idB = "renderer-b"
	idC = "renderer-c"
	idD = "renderer-d"
	idE = "renderer-e"

	configs = RendererConfigList{
		{Name: "A", ID: &idA},
		{Name: "B", ID: &idB},
		{Name: "C", ID: &idC},
		{Name: "D", ID: &idD},
		{Name: "E", ID: &idE},
	}
)

func TestNewRendererSpec(t *testing.T) {
	t.Run("creates graph from data sources correctly", func(t *testing.T) {
		// Flip ordering of indexes 0 and 1 to test that order doesn't matter
		localConfigs := RendererConfigList{configs[1], configs[0], configs[2]}
		dataSources := []map[string][]DataSource{
			{
				"tables": {
					&OutputTableRef{RendererID: idA, Output: "out-a", ContentIndex: 0},
				},
			},
			{
				"tables": {
					&TableRefSource{Name: "independent_table"},
				},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: idA, Output: "out-a", ContentIndex: 0},
					&OutputTableRef{RendererID: idB, Output: "out-b", ContentIndex: 0},
				},
			},
		}

		spec, errs, err := NewRendererSpec(localConfigs, dataSources)
		assert.NoError(t, err)

		expectedGraph := Digraph{
			nodes: NodeSet{
				0: {},
				1: {},
				2: {},
			},
			// We can think of this as saying renderer index 1 (renderer A) is depended on by indices 0 and 2 (renderers B and C)
			successors: map[NodeID]NodeSet{
				1: {
					0: {},
					2: {},
				},
				0: {
					2: {},
				},
			},
		}

		assert.Equal(t, expectedGraph, spec.Graph)
		assert.Equal(t, localConfigs, spec.Configs)
		assert.Equal(t, dataSources, spec.DataSources)
		assert.Equal(t, make([]error, len(localConfigs)), errs)
	})
	t.Run("handles invalid renderer id referenced in data source", func(t *testing.T) {
		localConfigs := RendererConfigList{configs[0], configs[1], configs[2], configs[3]}
		dataSources := []map[string][]DataSource{
			{
				"tables": {
					&TableRefSource{Name: "independent_table"},
				},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: idA, Output: "out-a", ContentIndex: 0},
				},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: "missing", Output: "out-a", ContentIndex: 0},
					&OutputTableRef{RendererID: idB, Output: "out-b", ContentIndex: 0},
				},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: idA, Output: "out-a", ContentIndex: 0},
					&OutputTableRef{RendererID: idC, Output: "out-c", ContentIndex: 0},
				},
			},
		}

		spec, errs, err := NewRendererSpec(localConfigs, dataSources)
		assert.NoError(t, err)

		expectedGraph := Digraph{
			nodes: NodeSet{
				0: {},
				1: {},
				2: {},
				3: {},
			},
			successors: map[NodeID]NodeSet{
				0: {
					1: {},
					// "missing" -> 2 doesn't exist
					3: {},
				},
				1: {
					2: {},
				},
				2: {
					3: {},
				},
			},
		}
		assert.Equal(t, expectedGraph, spec.Graph)
		assert.Equal(t, localConfigs, spec.Configs)
		assert.Equal(t, dataSources, spec.DataSources)
		// Configuration error should have been added for renderer with invalid data sources
		assert.Error(t, errs[2])
	})
	t.Run("sets config errs for all renderers that form part of a cycle", func(t *testing.T) {
		localConfigs := RendererConfigList{configs[0], configs[1], configs[2], configs[3], configs[4]}
		// A -> B -> C -> D -> B, E -> E (2 discrete cycles)
		dataSources := []map[string][]DataSource{
			{
				"tables": {},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: idA, Output: "out-a", ContentIndex: 0},
					&OutputTableRef{RendererID: idD, Output: "out-d", ContentIndex: 0},
				},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: idB, Output: "out-b", ContentIndex: 0},
				},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: idC, Output: "out-c", ContentIndex: 0},
				},
			},
			{
				"tables": {
					&OutputTableRef{RendererID: idE, Output: "out-e", ContentIndex: 0},
				},
			},
		}

		spec, errs, err := NewRendererSpec(localConfigs, dataSources)
		assert.NoError(t, err)

		expectedGraph := Digraph{
			nodes: NodeSet{
				0: {},
				1: {},
				2: {},
				3: {},
				4: {},
			},
			successors: map[NodeID]NodeSet{
				0: {
					1: {},
				},
				1: {
					2: {},
				},
				2: {
					3: {},
				},
				3: {
					1: {},
				},
				4: {
					4: {},
				},
			},
		}

		expectedCycle1 := "B (id: `renderer-b`) -> C (id: `renderer-c`) -> D (id: `renderer-d`) -> B (id: `renderer-b`)"
		expectedCycle2 := "E (id: `renderer-e`) -> E (id: `renderer-e`)"
		expectedErrB := message.New(message.EngineRenderRendererspecRendererDependencyCycle).WithMetadata(map[string]string{
			"type":  "B",
			"id":    "renderer-b",
			"cycle": expectedCycle1,
		})
		expectedErrC := message.New(message.EngineRenderRendererspecRendererDependencyCycle).WithMetadata(map[string]string{
			"type":  "C",
			"id":    "renderer-c",
			"cycle": expectedCycle1,
		})
		expectedErrD := message.New(message.EngineRenderRendererspecRendererDependencyCycle).WithMetadata(map[string]string{
			"type":  "D",
			"id":    "renderer-d",
			"cycle": expectedCycle1,
		})
		expectedErrE := message.New(message.EngineRenderRendererspecRendererDependencyCycle).WithMetadata(map[string]string{
			"type":  "E",
			"id":    "renderer-e",
			"cycle": expectedCycle2,
		})

		assert.Equal(t, expectedGraph, spec.Graph)
		assert.Equal(t, localConfigs, spec.Configs)
		assert.Equal(t, dataSources, spec.DataSources)
		assert.NoError(t, errs[0])
		assert.Equal(t, expectedErrB, errs[1])
		assert.Equal(t, expectedErrC, errs[2])
		assert.Equal(t, expectedErrD, errs[3])
		assert.Equal(t, expectedErrE, errs[4])
		assert.NoError(t, message.ValidateMetadataPlaceholders(errs[1]))
		assert.NoError(t, message.ValidateMetadataPlaceholders(errs[2]))
		assert.NoError(t, message.ValidateMetadataPlaceholders(errs[3]))
		assert.NoError(t, message.ValidateMetadataPlaceholders(errs[4]))
	})
}

func TestMerge(t *testing.T) {
	existing0 := errors.New("existing 0")
	existing1 := errors.New("existing 1")
	existing2 := errors.New("existing 2")
	new0 := errors.New("new 0")
	new1 := errors.New("new 1")
	new2 := errors.New("new 2")

	tests := []struct {
		name     string
		existing []error
		new      []error
		expected []error
	}{
		{
			name:     "keeps existing errors and fills gaps from new errors",
			existing: []error{existing0, nil, existing2, nil},
			new:      []error{new0, new1, new2, nil},
			expected: []error{existing0, new1, existing2, nil},
		},
		{
			name:     "preserves trailing errors from longer existing slice",
			existing: []error{nil, existing1, existing2},
			new:      []error{new0},
			expected: []error{new0, existing1, existing2},
		},
		{
			name:     "preserves trailing errors from longer new slice",
			existing: []error{existing0},
			new:      []error{new0, new1, new2},
			expected: []error{existing0, new1, new2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, merge(tt.existing, tt.new))
		})
	}
}
