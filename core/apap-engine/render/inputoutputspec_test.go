// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestValidatePortSpecs(t *testing.T) {
	t.Run("fails with duplicate input spec names", func(t *testing.T) {
		renderer := &MockRenderer{}
		inputs := InputSpec{}
		inputs.Ports = []PortSpec{
			{Name: "measurements", Cardinality: CardinalityOne, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: "0.2.2"}},
			{Name: "measurements", Cardinality: CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: "0.2.2"}},
		}

		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		renderer.On("Name").Return("test_renderer")
		renderer.On("GetInputSpec").Return(inputs)
		renderer.On("GetOutputSpec").Return(OutputSpec{})

		err := ValidatePortSpecs(renderer)
		assert.EqualError(t, err, "renderer test_renderer has invalid input spec: duplicate port name: measurements")
	})
	t.Run("fails with duplicate output spec names", func(t *testing.T) {
		renderer := &MockRenderer{}
		outputs := OutputSpec{}
		outputs.Ports = []PortSpec{
			{Name: "measurements", Cardinality: CardinalityOne, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: "0.2.2"}},
			{Name: "measurements", Cardinality: CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: "0.2.2"}},
		}

		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		renderer.On("Name").Return("test_renderer")
		renderer.On("GetInputSpec").Return(InputSpec{})
		renderer.On("GetOutputSpec").Return(outputs)

		err := ValidatePortSpecs(renderer)
		assert.EqualError(t, err, "renderer test_renderer has invalid output spec: duplicate port name: measurements")
	})
	t.Run("fails with empty component", func(t *testing.T) {
		renderer := &MockRenderer{}
		inputs := InputSpec{}
		inputs.Ports = []PortSpec{
			{Name: "drilldown", Cardinality: CardinalityOne, ComponentType: cdf.ComponentType{SchemaVersion: "0.2.2"}},
			{Name: "measurements", Cardinality: CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: "0.2.2"}},
		}

		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		renderer.On("Name").Return("test_renderer")
		renderer.On("GetInputSpec").Return(inputs)
		renderer.On("GetOutputSpec").Return(OutputSpec{})

		err := ValidatePortSpecs(renderer)
		assert.EqualError(t, err, "renderer test_renderer has invalid input spec: port drilldown has empty component type name")
	})
	t.Run("fails with empty port name", func(t *testing.T) {
		renderer := &MockRenderer{}
		inputs := InputSpec{}
		inputs.Ports = []PortSpec{
			{Name: "", Cardinality: CardinalityOne, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: "0.2.2"}},
			{Name: "measurements", Cardinality: CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: "0.2.2"}},
		}

		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		renderer.On("Name").Return("test_renderer")
		renderer.On("GetInputSpec").Return(inputs)
		renderer.On("GetOutputSpec").Return(OutputSpec{})

		err := ValidatePortSpecs(renderer)
		assert.EqualError(t, err, "renderer test_renderer has invalid input spec: empty port name")
	})
	t.Run("fails with non-identifier port name", func(t *testing.T) {
		renderer := &MockRenderer{}
		inputs := InputSpec{}
		inputs.Ports = []PortSpec{
			{Name: "", Cardinality: CardinalityOne, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: "0.2.2"}},
			{Name: "measurements-stuff", Cardinality: CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: "0.2.2"}},
		}

		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		renderer.On("Name").Return("test_renderer")
		renderer.On("GetInputSpec").Return(inputs)
		renderer.On("GetOutputSpec").Return(OutputSpec{})

		err := ValidatePortSpecs(renderer)
		assert.EqualError(t, err, "renderer test_renderer has invalid input spec: empty port name")
	})
	t.Run("succeeds with valid ports", func(t *testing.T) {
		renderer := &MockRenderer{}
		portSpec := []PortSpec{
			{Name: "drilldown", Cardinality: CardinalityOne, ComponentType: cdf.ComponentType{Name: "drilldown_measurements", SchemaVersion: "0.2.2"}},
			{Name: "measurements", Cardinality: CardinalityPerRun, ComponentType: cdf.ComponentType{Name: "drilldown", SchemaVersion: "0.2.2"}},
		}
		inputSpec, outputSpec := InputSpec{}, OutputSpec{}
		inputSpec.Ports = portSpec
		outputSpec.Ports = portSpec

		renderer.On("Configure", mock.Anything).Return(nil)
		renderer.On("Initialize", mock.Anything, mock.Anything).Return(nil)
		renderer.On("Name").Return("test_renderer")
		renderer.On("GetInputSpec").Return(inputSpec)
		renderer.On("GetOutputSpec").Return(outputSpec)

		err := ValidatePortSpecs(renderer)
		assert.NoError(t, err)
	})
}

func TestEmitPendingRendererOutputSpecSkipsExistingMatchingPerRunOutputs(t *testing.T) {
	session := newFakeSession([]string{"runA", "runB"})
	rendererID := "renderer-id"
	identity := RendererIdentity{Index: 1, ID: &rendererID, Name: "test_renderer"}
	component := cdf.ComponentType{Name: "symbols", SchemaVersion: "1.0.0"}

	session.Manifest().AddEntry(NewManifestEntryInfo(
		component,
		identity,
		[]run.RunID{{Value: "runA"}},
	))

	outputSpec := OutputSpec{
		PortList: PortList{
			Ports: []PortSpec{
				{Name: "symbols", Cardinality: CardinalityPerRun, ComponentType: component},
			},
		},
	}

	emitPendingRendererOutputSpec(session, outputSpec, identity)
	emitPendingRendererOutputSpec(session, outputSpec, identity)

	entries := session.Manifest().Entries()
	require.Len(t, entries, 2)

	pendingByRunID := map[string]bool{}
	for _, entry := range entries {
		associatedContent := entry.Info().AssociatedContent()
		require.Len(t, associatedContent, 1)
		pendingByRunID[associatedContent[0].Value] = entry.Info().Pending()
	}

	require.Contains(t, pendingByRunID, "runA")
	require.Contains(t, pendingByRunID, "runB")
	assert.False(t, pendingByRunID["runA"])
	assert.True(t, pendingByRunID["runB"])
}
