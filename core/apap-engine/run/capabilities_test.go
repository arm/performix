// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bmatcuk/doublestar"
	"github.com/klauspost/compress/zstd"
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

func writeCompressedCapabilityTestFile(t *testing.T, dir string, name string, contents string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	require.NoError(t, err)
	compressed := encoder.EncodeAll([]byte(contents), nil)
	encoder.Close()
	require.NoError(t, os.WriteFile(path, compressed, perms.LocalFilePerm))
	return path
}

func TestLoadRunCapabilities(t *testing.T) {
	t.Run("loads a compressed capability component", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "one.json.zst")
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		require.NoError(t, err)
		compressed := encoder.EncodeAll([]byte(`{"state":"available","payload":{"enabled":true}}`), nil)
		encoder.Close()
		require.NoError(t, os.WriteFile(path, compressed, perms.LocalFilePerm))

		model := &capabilitiesModelView{components: []cdf.Component{{
			Type:         cdf.ComponentType{Name: "one-data", SchemaVersion: "1.0"},
			RelativePath: "tool/a/0/capabilities/one.json",
			AbsolutePath: path,
			Compressed:   true,
		}}}

		capabilities, err := LoadRunCapabilities([]cdf.ModelView{model})

		require.NoError(t, err)
		require.Equal(t, true, capabilities[0].CapabilitiesPerTool["tool/a/0"]["one"].Payload["enabled"])
	})

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

func TestCPUBindingsLoad(t *testing.T) {
	t.Run("compressed CPU outcome", func(t *testing.T) {
		dir := t.TempDir()
		model := &capabilitiesModelView{components: []cdf.Component{
			{Type: cdf.ComponentType{Name: "tool_capabilities/cpu_counter", SchemaVersion: "1.0"}, RelativePath: "tool/a/0/capabilities/counter.cpu_cycles.json", AbsolutePath: writeCompressedCapabilityTestFile(t, dir, "cpu.json.zst", `{"state":"collected","payload":{"title":"CPU Cycles","units":"cycles","bindings":[{"seriesCapabilityId":"series"}]}}`), Compressed: true},
			{Type: cdf.ComponentType{Name: "tool_capabilities/counter", SchemaVersion: "1.0"}, RelativePath: "tool/a/0/capabilities/series.json", AbsolutePath: writeCapabilityTestFile(t, dir, "series.json", `{"state":"collected","payload":{"key_type":0,"series_id":0}}`)},
		}}

		loaded, err := LoadRunCapabilities([]cdf.ModelView{model})
		require.NoError(t, err)
		require.Equal(t, "collected", loaded[0].CapabilitiesPerTool["tool/a/0"]["counter.cpu_cycles"].State)
	})

	for _, tc := range []struct {
		name, invocation, typ, state, payload string
		valid                                 bool
	}{
		{"valid", "a", "tool_capabilities/counter", "collected", `{"key_type":0,"series_id":0}`, true},
		{"other invocation", "b", "tool_capabilities/counter", "collected", `{"key_type":0,"series_id":0}`, false},
		{"wrong type", "a", "another-type", "collected", `{"key_type":0,"series_id":0}`, false},
		{"uncollected", "a", "tool_capabilities/counter", "available", `{"key_type":0,"series_id":0}`, false},
		{"missing series", "a", "tool_capabilities/counter", "collected", `{"key_type":0}`, false},
		{"key_type_string", "a", "tool_capabilities/counter", "collected", `{"key_type":"8","series_id":0}`, false},
		{"key_type_boolean", "a", "tool_capabilities/counter", "collected", `{"key_type":true,"series_id":0}`, false},
		{"key_type_fraction", "a", "tool_capabilities/counter", "collected", `{"key_type":1.5,"series_id":0}`, false},
		{"key_type_negative", "a", "tool_capabilities/counter", "collected", `{"key_type":-1,"series_id":0}`, false},
		{"key_type_unsafe", "a", "tool_capabilities/counter", "collected", `{"key_type":9007199254740992,"series_id":0}`, false},
		{"series_id_string", "a", "tool_capabilities/counter", "collected", `{"key_type":0,"series_id":"8"}`, false},
		{"series_id_boolean", "a", "tool_capabilities/counter", "collected", `{"key_type":0,"series_id":true}`, false},
		{"series_id_fraction", "a", "tool_capabilities/counter", "collected", `{"key_type":0,"series_id":1.5}`, false},
		{"series_id_negative", "a", "tool_capabilities/counter", "collected", `{"key_type":0,"series_id":-1}`, false},
		{"series_id_unsafe", "a", "tool_capabilities/counter", "collected", `{"key_type":0,"series_id":9007199254740992}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			model := &capabilitiesModelView{components: []cdf.Component{
				{Type: cdf.ComponentType{Name: "tool_capabilities/cpu_counter", SchemaVersion: "1.0"}, RelativePath: "tool/a/0/capabilities/counter.cpu_cycles.json", AbsolutePath: writeCapabilityTestFile(t, dir, "cpu.json", `{"state":"collected","payload":{"title":"CPU Cycles","units":"cycles","bindings":[{"seriesCapabilityId":"series"}]}}`)},
				{Type: cdf.ComponentType{Name: tc.typ, SchemaVersion: "1.0"}, RelativePath: "tool/" + tc.invocation + "/0/capabilities/series.json", AbsolutePath: writeCapabilityTestFile(t, dir, "series.json", `{"state":"`+tc.state+`","payload":`+tc.payload+`}`)},
			}}
			_, err := LoadRunCapabilities([]cdf.ModelView{model})
			require.Equal(t, tc.valid, err == nil, "%v", err)
		})
	}
}

func TestNativeCPUBindingsLoad(t *testing.T) {
	for _, tc := range []struct {
		name, binding, seriesState, invocation string
		valid                                  bool
	}{
		{"collected", `,"binding":{"seriesCapabilityId":"series"}`, "collected", "a", true},
		{"missing binding", "", "collected", "a", false},
		{"missing series", `,"binding":{"seriesCapabilityId":"missing"}`, "collected", "a", false},
		{"uncollected series", `,"binding":{"seriesCapabilityId":"series"}`, "incomplete", "a", false},
		{"other invocation", `,"binding":{"seriesCapabilityId":"series"}`, "collected", "b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a saved native-event outcome and a concrete series capability.
			dir := t.TempDir()
			model := &capabilitiesModelView{components: []cdf.Component{
				{Type: cdf.ComponentType{Name: "tool_capabilities/native_cpu_counter", SchemaVersion: "1.0"}, RelativePath: "tool/a/0/capabilities/nativecounter.pmu.8.json", AbsolutePath: writeCapabilityTestFile(t, dir, "event.json", `{"state":"collected","payload":{"title":"Instructions","pmu":{"id":"pmu","name":"PMU"},"eventCode":"0x8"`+tc.binding+`}}`)},
				{Type: cdf.ComponentType{Name: "tool_capabilities/counter", SchemaVersion: "1.0"}, RelativePath: "tool/" + tc.invocation + "/0/capabilities/series.json", AbsolutePath: writeCapabilityTestFile(t, dir, "series.json", `{"state":"`+tc.seriesState+`","payload":{"key_type":8,"series_id":10}}`)},
			}}

			// When loading capabilities for rendering.
			loaded, err := LoadRunCapabilities([]cdf.ModelView{model})

			// Then only bindings to a collected series in this invocation are accepted.
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "collected", loaded[0].CapabilitiesPerTool["tool/a/0"]["nativecounter.pmu.8"].State)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestUnknownCapabilityValidation(t *testing.T) {
	for _, tc := range []struct {
		name, contents string
		duplicate      bool
		wantError      string
	}{
		{name: "custom schema and state", contents: `{"state":"custom-state","payload":{"bindings":"custom-format","enabled":true}}`},
		{name: "unknown fields", contents: `{"state":"collected","payload":{},"unknown-field":true}`},
		{name: "empty file", contents: ""},
		{name: "empty object", contents: `{}`},
		{name: "null object", contents: `null`},
		{name: "omitted payload", contents: `{"state":"collected"}`},
		{name: "omitted state", contents: `{"payload":{"enabled":true}}`},
		{name: "null fields", contents: `{"state":null,"payload":null}`},
		{name: "malformed JSON", contents: `{`, wantError: "unexpected end of JSON input"},
		{name: "non-object", contents: `[]`, wantError: "cannot unmarshal array"},
		{name: "invalid state type", contents: `{"state":5,"payload":{}}`, wantError: "cannot unmarshal number"},
		{name: "invalid payload type", contents: `{"state":"collected","payload":[]}`, wantError: "cannot unmarshal array"},
		{name: "duplicate ID", contents: `{"state":"collected","payload":{}}`, duplicate: true, wantError: "duplicate capability"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a capability whose type and version have no Performix validator.
			dir := t.TempDir()
			componentType := cdf.ComponentType{Name: "example/custom", SchemaVersion: "2.3"}
			component := cdf.Component{
				Type:         componentType,
				RelativePath: "tool/example/0/capabilities/custom.json",
				AbsolutePath: writeCapabilityTestFile(t, dir, "custom.json", tc.contents),
			}
			model := &capabilitiesModelView{components: []cdf.Component{component}}
			if tc.duplicate {
				model.components = append(model.components, component)
			}

			// When loading the capability outcome.
			loaded, err := LoadRunCapabilities([]cdf.ModelView{model})

			// Then common checks apply.
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Nil(t, loaded)
				return
			}
			require.NoError(t, err)
			var want ToolCapability
			if tc.contents != "" {
				require.NoError(t, json.Unmarshal([]byte(tc.contents), &want))
			}
			want.ComponentType = componentType
			require.Equal(t, want, loaded[0].CapabilitiesPerTool["tool/example/0"]["custom"])
		})
	}
}

func TestMixedCapabilityValidation(t *testing.T) {
	const logical = `{"state":"collected","payload":{"title":"CPU Cycles","units":"cycles","bindings":[{"seriesCapabilityId":"series"}]}}`
	const native = `{"state":"collected","payload":{"title":"Instructions","pmu":{"id":"pmu","name":"PMU"},"eventCode":"0x8","binding":{"seriesCapabilityId":"series"}}}`
	for _, tc := range []struct {
		name, logical, native, wantError string
	}{
		{name: "valid", logical: logical, native: native},
		{name: "invalid logical CPU", logical: `{"extra":true,` + logical[1:], native: native, wantError: "unknown field"},
		{name: "invalid native CPU", logical: logical, native: `{"extra":true,` + native[1:], wantError: "unknown field"},
		{name: "null logical CPU", logical: "null", native: native, wantError: `CPU capability "counter.cpu_cycles" outcome must be a JSON object`},
		{name: "null native CPU", logical: logical, native: "null", wantError: `CPU capability "nativecounter.pmu.8" outcome must be a JSON object`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given logical and native CPU outcomes and an unknown capability type sharing one invocation.
			dir := t.TempDir()
			model := &capabilitiesModelView{components: []cdf.Component{
				{Type: cdf.ComponentType{Name: "tool_capabilities/cpu_counter", SchemaVersion: "1.0"}, RelativePath: "tool/a/0/capabilities/counter.cpu_cycles.json", AbsolutePath: writeCapabilityTestFile(t, dir, "logical.json", tc.logical)},
				{Type: cdf.ComponentType{Name: "tool_capabilities/native_cpu_counter", SchemaVersion: "1.0"}, RelativePath: "tool/a/0/capabilities/nativecounter.pmu.8.json", AbsolutePath: writeCapabilityTestFile(t, dir, "native.json", tc.native)},
				{Type: cdf.ComponentType{Name: "example/custom", SchemaVersion: "2.3"}, RelativePath: "tool/a/0/capabilities/custom.json", AbsolutePath: writeCapabilityTestFile(t, dir, "custom.json", `{"state":"custom-state","payload":{"binding":"custom-format"}}`)},
				{Type: cdf.ComponentType{Name: "tool_capabilities/counter", SchemaVersion: "1.0"}, RelativePath: "tool/a/0/capabilities/series.json", AbsolutePath: writeCapabilityTestFile(t, dir, "series.json", `{"state":"collected","payload":{"key_type":0,"series_id":0}}`)},
			}}

			// When loading the capability outcomes.
			loaded, err := LoadRunCapabilities([]cdf.ModelView{model})

			// Then known capability types are checked while unknown capability types are accepted.
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Nil(t, loaded)
				return
			}
			require.NoError(t, err)
			require.Len(t, loaded[0].CapabilitiesPerTool["tool/a/0"], 4)
			require.Equal(t, "custom-state", loaded[0].CapabilitiesPerTool["tool/a/0"]["custom"].State)
		})
	}
}
