// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bmatcuk/doublestar"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
)

type capabilitiesModelView struct {
	cdf.ModelView
	components []cdf.Component
	err        error
	globs      []string
	migrations []cdf.PathMigration
}

func (m *capabilitiesModelView) FindComponents(glob string) ([]cdf.Component, error) {
	m.globs = append(m.globs, glob)
	if m.err != nil {
		return nil, m.err
	}

	var components []cdf.Component
	for _, component := range m.components {
		matched, err := doublestar.Match(glob, component.RelativePath)
		if err != nil {
			return nil, err
		}
		if matched {
			components = append(components, component)
		}
	}
	return components, nil
}

func (m *capabilitiesModelView) Migrations() []cdf.PathMigration {
	return m.migrations
}

func writeCapabilityTestFile(t *testing.T, dir string, name string, contents string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(contents), perms.LocalFilePerm))
	return path
}

func TestLoadRunCapabilities(t *testing.T) {
	t.Run("loads and groups capabilities for each run", func(t *testing.T) {
		dir := t.TempDir()
		oneType := cdf.ComponentType{Name: "one-data", SchemaVersion: "1.0"}
		twoType := cdf.ComponentType{Name: "two-data", SchemaVersion: "2.0"}
		threeType := cdf.ComponentType{Name: "three-data", SchemaVersion: "3.0"}
		firstRun := &capabilitiesModelView{components: []cdf.Component{
			{
				Type:         oneType,
				RelativePath: "tool/a/0/capabilities/one.json",
				AbsolutePath: writeCapabilityTestFile(t, dir, "one.json", `{"state":"available","payload":{"enabled":true}}`),
			},
			{
				Type:         twoType,
				RelativePath: "tool/a/0/capabilities/two.json",
				AbsolutePath: writeCapabilityTestFile(t, dir, "two.json", `{"state":"unavailable","payload":{"reason":"off"}}`),
			},
			{
				Type:         threeType,
				RelativePath: "tool/b/1/capabilities/three.json",
				AbsolutePath: writeCapabilityTestFile(t, dir, "three.json", `{"state":"available","payload":{"mode":"x"}}`),
			},
		}}
		secondRun := &capabilitiesModelView{}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{firstRun, secondRun})

		require.NoError(t, err)
		require.Equal(t, []RunCapabilities{
			{
				CapabilitiesPerTool: map[string]ToolCapabilities{
					"tool/a/0": {
						"one": {
							State:         "available",
							Payload:       map[string]any{"enabled": true},
							ComponentType: oneType,
						},
						"two": {
							State:         "unavailable",
							Payload:       map[string]any{"reason": "off"},
							ComponentType: twoType,
						},
					},
					"tool/b/1": {
						"three": {
							State:         "available",
							Payload:       map[string]any{"mode": "x"},
							ComponentType: threeType,
						},
					},
				},
			},
			{CapabilitiesPerTool: map[string]ToolCapabilities{}},
		}, capabilities)
	})

	t.Run("loads capabilities without invocation indexes when an invocation migration exists", func(t *testing.T) {
		dir := t.TempDir()
		componentType := cdf.ComponentType{Name: "one-data", SchemaVersion: "1.0"}
		migrations := []cdf.PathMigration{
			&cdf.ToolNameMigration{Type: "renameTool", From: "old", To: "a"},
			&cdf.ToolInvocationMigration{Type: "missingInvocation", From: "a"},
		}
		model := &capabilitiesModelView{
			components: []cdf.Component{
				{
					Type:         componentType,
					RelativePath: "tool/a/capabilities/one.json",
					AbsolutePath: writeCapabilityTestFile(t, dir, "one.json", `{"state":"available","payload":{"enabled":true}}`),
				},
			},
			migrations: migrations,
		}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.NoError(t, err)
		require.Equal(t, []RunCapabilities{
			{
				CapabilitiesPerTool: map[string]ToolCapabilities{
					"tool/a": {
						"one": {
							State:         "available",
							Payload:       map[string]any{"enabled": true},
							ComponentType: componentType,
						},
					},
				},
				Migrations: migrations,
			},
		}, capabilities)
	})

	t.Run("keeps invocation indexes for unrelated migrations", func(t *testing.T) {
		dir := t.TempDir()
		componentType := cdf.ComponentType{Name: "one-data", SchemaVersion: "1.0"}
		migrations := []cdf.PathMigration{
			&cdf.ToolNameMigration{Type: "renameTool", From: "old", To: "a"},
		}
		model := &capabilitiesModelView{
			components: []cdf.Component{
				{
					Type:         componentType,
					RelativePath: "tool/a/0/capabilities/one.json",
					AbsolutePath: writeCapabilityTestFile(t, dir, "one.json", `{"state":"available","payload":{"enabled":true}}`),
				},
			},
			migrations: migrations,
		}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.NoError(t, err)
		require.Equal(t, []RunCapabilities{
			{
				CapabilitiesPerTool: map[string]ToolCapabilities{
					"tool/a/0": {
						"one": {
							State:         "available",
							Payload:       map[string]any{"enabled": true},
							ComponentType: componentType,
						},
					},
				},
				Migrations: migrations,
			},
		}, capabilities)
	})

	t.Run("loads indexed and non-indexed capabilities from the same run", func(t *testing.T) {
		dir := t.TempDir()
		oneType := cdf.ComponentType{Name: "one-data", SchemaVersion: "1.0"}
		twoType := cdf.ComponentType{Name: "two-data", SchemaVersion: "2.0"}
		migrations := []cdf.PathMigration{
			&cdf.ToolInvocationMigration{Type: "missingInvocation", From: "a"},
		}
		model := &capabilitiesModelView{
			components: []cdf.Component{
				{
					Type:         oneType,
					RelativePath: "tool/a/capabilities/one.json",
					AbsolutePath: writeCapabilityTestFile(t, dir, "one.json", `{"state":"available","payload":{"enabled":true}}`),
				},
				{
					Type:         twoType,
					RelativePath: "tool/b/0/capabilities/two.json",
					AbsolutePath: writeCapabilityTestFile(t, dir, "two.json", `{"state":"unavailable","payload":{"reason":"off"}}`),
				},
			},
			migrations: migrations,
		}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.NoError(t, err)
		require.Equal(t, []RunCapabilities{
			{
				CapabilitiesPerTool: map[string]ToolCapabilities{
					"tool/a": {
						"one": {
							State:         "available",
							Payload:       map[string]any{"enabled": true},
							ComponentType: oneType,
						},
					},
					"tool/b/0": {
						"two": {
							State:         "unavailable",
							Payload:       map[string]any{"reason": "off"},
							ComponentType: twoType,
						},
					},
				},
				Migrations: migrations,
			},
		}, capabilities)
	})

	t.Run("returns component discovery errors", func(t *testing.T) {
		discoveryErr := errors.New("find failed")
		model := &capabilitiesModelView{err: discoveryErr}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.Nil(t, capabilities)
		require.ErrorIs(t, err, discoveryErr)
	})

	t.Run("ignores malformed capability paths", func(t *testing.T) {
		model := &capabilitiesModelView{components: []cdf.Component{
			{RelativePath: "tool/a/0/capabilities/one.txt"},
		}}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.NoError(t, err)
		require.Empty(t, capabilities[0].CapabilitiesPerTool)
		require.Empty(t, capabilities[0].Migrations)
	})

	t.Run("returns JSON read errors", func(t *testing.T) {
		dir := t.TempDir()
		model := &capabilitiesModelView{components: []cdf.Component{
			{
				RelativePath: "tool/a/0/capabilities/one.json",
				AbsolutePath: writeCapabilityTestFile(t, dir, "one.json", "{"),
			},
		}}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.Nil(t, capabilities)
		require.ErrorContains(t, err, "unexpected end of JSON input")
	})

	t.Run("rejects duplicate capability IDs", func(t *testing.T) {
		dir := t.TempDir()
		model := &capabilitiesModelView{components: []cdf.Component{
			{
				RelativePath: "tool/a/0/capabilities/one.json",
				AbsolutePath: writeCapabilityTestFile(t, dir, "first.json", `{"state":"available"}`),
			},
			{
				RelativePath: "tool/a/0/capabilities/one.json",
				AbsolutePath: writeCapabilityTestFile(t, dir, "second.json", `{"state":"unavailable"}`),
			},
		}}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.Nil(t, capabilities)
		require.ErrorContains(t, err, "duplicate capability")
	})
}

func TestGetCapabilityDetails(t *testing.T) {
	tests := []struct {
		name            string
		relativePath    string
		invocationPath  string
		capabilityID    string
		expectedFailure bool
	}{
		{
			name:           "returns details for an indexed invocation",
			relativePath:   "tool/a/0/capabilities/one.json",
			invocationPath: "tool/a/0",
			capabilityID:   "one",
		},
		{
			name:           "returns details when the invocation index is missing",
			relativePath:   "tool/a/capabilities/one.json",
			invocationPath: "tool/a",
			capabilityID:   "one",
		},
		{
			name:           "preserves a dotted capability ID",
			relativePath:   "tool/a/0/capabilities/counter.cpu_cycles.json",
			invocationPath: "tool/a/0",
			capabilityID:   "counter.cpu_cycles",
		},
		{
			name:            "rejects a capability with the wrong extension",
			relativePath:    "tool/a/0/capabilities/one.txt",
			expectedFailure: true,
		},
		{
			name:            "rejects an unrelated path",
			relativePath:    "not/a/capability.json",
			expectedFailure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocationPath, capabilityID, parsed := getCapabilityDetails(tt.relativePath)

			if tt.expectedFailure {
				require.False(t, parsed)
				require.Empty(t, invocationPath)
				require.Empty(t, capabilityID)
				return
			}

			require.True(t, parsed)
			require.Equal(t, tt.invocationPath, invocationPath)
			require.Equal(t, tt.capabilityID, capabilityID)
		})
	}
}
