// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
)

const cpuCounterID = "counter.cpu_cycles"

func cpuCatalogue(name, event string, cluster int) map[string]any {
	return map[string]any{
		"counters": fmt.Sprintf(`<counters><counter name="%s_ccnt"/><cluster id="%d" name="%s"/><cpu id="%d" cluster="%d"/></counters>`, name, cluster, name, cluster, cluster),
		"events":   fmt.Sprintf(`<events><category per_cpu="yes" counter_set="%s_cnt"><event counter="%s_ccnt" event="%s" units="cycles"/></category></events>`, name, name, event),
	}
}

func cpuRequest(policy string, catalogue map[string]any) map[string]any {
	required, optional := []string{}, []string{}
	switch policy {
	case "required":
		required = []string{cpuCounterID}
	case "optional":
		optional = []string{cpuCounterID}
	}
	return map[string]any{
		"params":   map[string]any{"mode": "samples"},
		"metadata": map[string]any{},
		"discovery": map[string]any{"capabilities": map[string]any{
			"neoprof_cpu_counter_catalogue": catalogue,
		}},
		"capabilityRequest": map[string]any{"required": required, "optional": optional},
	}
}

func TestNeoprofCPUCounterSelectionAndValidation(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/neoprof_cpu_counters.js", jstest.WithXMLModule)
	empty := map[string]any{"required": []string{}, "optional": []string{}}
	selected := cpuRequest("required", cpuCatalogue("ARMv9_Neoverse_V2", "0x11", 0))

	got, err := h.Call[bool](t, "selected", selected)
	require.NoError(t, err)
	assert.True(t, got)
	got, err = h.Call[bool](t, "discoveryRequested", map[string]any{"capabilityRequest": empty})
	require.NoError(t, err)
	assert.False(t, got)
	initial, err := h.Call[map[string]any](t, "initialProbeCapabilities", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, initial, cpuCounterID)

	_, err = h.Call[any](t, "validateOptions", map[string]any{
		"params":            map[string]any{"mode": "metrics"},
		"capabilityRequest": empty,
	})
	assert.ErrorContains(t, err, "CPU_COUNTER_OPTIONS_INVALID")
	_, err = h.Call[any](t, "validateOptions", map[string]any{"params": map[string]any{"mode": "metrics"}})
	require.NoError(t, err)
}

func TestNeoprofCPUCounterCatalogueResolutionAndBinding(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/neoprof_cpu_counters.js", jstest.WithXMLModule)
	arm := cpuCatalogue("ARMv9_Neoverse_V2", "0x11", 0)
	resolved, err := h.Call[map[string]any](t, "resolveCatalogue", arm)
	require.NoError(t, err)
	assert.Equal(t, "available", resolved["capability"].(map[string]any)["state"])
	assert.Len(t, resolved["native"], 1)

	unavailable, err := h.Call[map[string]any](t, "resolveCatalogue", map[string]any{
		"counters": "<counters/>", "events": "<events/>",
	})
	require.NoError(t, err)
	assert.Equal(t, "not_supported", unavailable["capability"].(map[string]any)["payload"].(map[string]any)["reason"])

	bindings, err := h.Call[[]map[string]any](t, "bindSeries",
		resolved["native"],
		`<captured><counters><counter key="0xa" type="ARMv9_Neoverse_V2_ccnt" event="0x11"/></counters></captured>`,
		[]map[string]any{{"key_type": 8, "series_id": 10}},
		func(entry map[string]any) string { return fmt.Sprintf("series.%v", entry["series_id"]) },
	)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	assert.Equal(t, "series.10", bindings[0]["seriesCapabilityId"])
}

func TestNeoprofCPUCounterPrepareUsesDiscovery(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/neoprof_cpu_counters.js", jstest.WithXMLModule)
	arm := cpuCatalogue("ARMv9_Neoverse_V2", "0x11", 0)
	ctx := cpuRequest("required", arm)
	commands := [][]string{}
	engine := map[string]any{
		"createTempDir": func() string { return "/tmp/cpu" },
		"execCommand": func(args []string) map[string]any {
			commands = append(commands, args)
			stdout := ""
			if args[0] == "cat" {
				stdout = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<configurations revision=\"3\">\n</configurations>\n"
			}
			return map[string]any{"rc": 0, "stdout": stdout, "stderr": ""}
		},
		"addToolCapability":   func(...any) {},
		"writeCatalogMessage": func(...any) {},
	}

	args, err := h.CallAwait[[]string](t, "prepare", engine, ctx)
	require.NoError(t, err)
	assert.Contains(t, args, "ARMv9_Neoverse_V2_ccnt")
	assert.Len(t, commands, 2)

	emptyCtx := cpuRequest("empty", arm)
	args, err = h.CallAwait[[]string](t, "prepare", engine, emptyCtx)
	require.NoError(t, err)
	assert.Equal(t, []string{"-c", "/tmp/cpu/counter_collection.xml"}, args)
}

func TestNeoprofCPUCounterDiscoveryFailuresPropagate(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/neoprof_cpu_counters.js", jstest.WithXMLModule)
	ctx := cpuRequest("required", cpuCatalogue("ARMv9_Neoverse_V2", "0x11", 0))
	ctx["metadata"].(map[string]any)["neoprofAsPrivileged"] = true
	stream := func(text string) map[string]any {
		done := false
		return map[string]any{"next": func() map[string]any {
			if done {
				return map[string]any{"done": true}
			}
			done = true
			return map[string]any{"done": false, "value": text}
		}}
	}
	engine := map[string]any{
		"startProcess": func(args []string, opts map[string]any) map[string]any {
			assert.Equal(t, []string{"/tools/sl-record", "--print", "counters.xml"}, args)
			assert.Equal(t, true, opts["asPrivileged"])
			return map[string]any{
				"stdout": stream(""), "stderr": stream("permission denied"),
				"wait": func() map[string]any { return map[string]any{"exitCode": 1} },
			}
		},
	}

	_, err := h.CallAwait[map[string]any](t, "discover", engine, ctx, "/tools/sl-record")
	assert.ErrorContains(t, err, "common.UNKNOWN_ERROR")
	assert.ErrorContains(t, err, "permission denied")
}

func TestNeoprofCPUCounterRegistryLoadsRowsFromGroupedJSON(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/neoprof_cpu_counters.js", jstest.WithXMLModule)
	const registryPath = "/capture/report-new/apx/metadata/counter_series_files.json"
	const registryJSON = `{"counter_groups":[
		{"key_type":8,"counters":[{"id":7,"files":[
			{"bin_duration":0,"relative_path":"report-new/apx/timeline/key_type=8/series_id=7/bin_duration=0/counter.parquet"},
			{"bin_duration":10,"relative_path":"report-new/apx/timeline/key_type=8/series_id=7/bin_duration=10/counter.parquet"}
		]}]},
		{"key_type":7,"counters":[{"id":31,"files":[
			{"bin_duration":10,"relative_path":"report-new/apx/timeline/key_type=7/series_id=31/bin_duration=10/counter.parquet"}
		]}]}
	]}`
	expectedRows := []map[string]any{
		{
			"key_type": int64(8), "series_id": int64(7), "bin_duration": int64(0),
			"relative_path": "report-new/apx/timeline/key_type=8/series_id=7/bin_duration=0/counter.parquet",
		},
		{
			"key_type": int64(8), "series_id": int64(7), "bin_duration": int64(10),
			"relative_path": "report-new/apx/timeline/key_type=8/series_id=7/bin_duration=10/counter.parquet",
		},
		{
			"key_type": int64(7), "series_id": int64(31), "bin_duration": int64(10),
			"relative_path": "report-new/apx/timeline/key_type=7/series_id=31/bin_duration=10/counter.parquet",
		},
	}

	for _, locality := range []string{"host", "target"} {
		t.Run(locality, func(t *testing.T) {
			var hostReadPath string
			engine := map[string]any{
				"getLocality": func() string { return locality },
				"readHostFile": func(path string) string {
					hostReadPath = path
					return registryJSON
				},
				"execCommand": func(args []string, _ ...any) map[string]any {
					if len(args) == 2 && args[0] == "cat" && args[1] == registryPath {
						return map[string]any{"rc": 0, "stdout": registryJSON, "stderr": ""}
					}
					return map[string]any{"rc": 1, "stdout": "", "stderr": "No such file or directory"}
				},
			}
			ctx := map[string]any{"metadata": map[string]any{"neoprofAsPrivileged": true}}

			rows, err := h.CallAwait[[]map[string]any](t, "loadSeriesRegistry", engine, ctx,
				"/capture",
			)

			require.NoError(t, err)
			assert.Equal(t, expectedRows, rows)
			if locality == "host" {
				assert.Equal(t, registryPath, hostReadPath)
			}
		})
	}
}

func TestNeoprofCPUCounterVerifyRequiresEveryTimelineDuration(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/neoprof_cpu_counters.js", jstest.WithXMLModule)
	arm := cpuCatalogue("ARMv9_Neoverse_V2", "0x11", 0)
	ctx := cpuRequest("required", arm)
	resolved, err := h.Call[map[string]any](t, "resolveCatalogue", arm)
	require.NoError(t, err)
	ctx["metadata"].(map[string]any)["cpuCounterNative"] = resolved["native"]

	durations := []int64{10000000000, 5000000000, 1000000000, 500000000, 100000000, 50000000, 10000000}
	registry := make([]map[string]any, 0, len(durations))
	for _, duration := range durations {
		registry = append(registry, map[string]any{
			"bin_duration":  duration,
			"relative_path": fmt.Sprintf("report-new/apx/timeline/key_type=8/series_id=10/bin_duration=%d/counter.parquet", duration),
		})
	}
	registryJSON, err := json.Marshal(map[string]any{"counter_groups": []map[string]any{{
		"key_type": 8, "counters": []map[string]any{{"id": 10, "files": registry}},
	}}})
	require.NoError(t, err)
	rowsJSON, err := json.Marshal(map[string]any{"valid": true, "device_nos": []int{0}})
	require.NoError(t, err)
	saved := []map[string]any{}
	published := []map[string]any{}
	engine := map[string]any{
		"getLocality": func() string { return "target" },
		"execCommand": func(args []string, _ ...any) map[string]any {
			switch {
			case args[0] == "cat" && args[1] == "/capture/captured.xml":
				return map[string]any{"rc": 0, "stdout": `<captured><counters><counter key="0xa" type="ARMv9_Neoverse_V2_ccnt" event="0x11"/></counters></captured>`}
			case len(args) > 1 && args[1] == "/capture/report-new/apx/metadata/counter_series_files.json":
				return map[string]any{"rc": 0, "stdout": string(registryJSON)}
			default:
				return map[string]any{"rc": 0, "stdout": string(rowsJSON)}
			}
		},
		"addToolCapability":   func(_ string, _ map[string]any, data map[string]any) { saved = append(saved, data) },
		"writeCatalogMessage": func(...any) {},
	}

	_, err = h.CallAwait[any](t, "verify", engine, ctx, "/capture",
		[]map[string]any{{"key_type": 8, "series_id": 10}},
		func(map[string]any) string { return "series.10" }, "/converter",
		func(series map[string]any) { published = append(published, series) },
	)
	require.NoError(t, err)
	require.Len(t, saved, 1)
	assert.Equal(t, "collected", saved[0]["state"])
	assert.Len(t, published, 1)
}

func TestNeoprofCPUCounterSeriesRegistryJSON(t *testing.T) {
	const path = "/capture/report-new/apx/metadata/counter_series_files.json"
	for _, locality := range []string{"host", "target"} {
		t.Run(locality, func(t *testing.T) {
			for _, tc := range []struct {
				name, document string
				valid          bool
			}{
				{"grouped", `{"counter_groups":[{"key_type":8,"counters":[{"id":10,"files":[{"bin_duration":10000000,"relative_path":"a.parquet"},{"bin_duration":50000000,"relative_path":"b.parquet"}]},{"id":12,"files":[{"bin_duration":10000000,"relative_path":"c.parquet"}]}]},{"key_type":7,"counters":[{"id":20,"files":[{"bin_duration":0,"relative_path":"d.parquet"}]}]}]}`, true},
				{"empty", `{"counter_groups":[]}`, true},
				{"malformed", `{`, false},
				{"old_flat_array", `[]`, false},
				{"missing_groups", `{}`, false},
				{"invalid_group", `{"counter_groups":[null]}`, false},
				{"invalid_counter", `{"counter_groups":[{"key_type":8,"counters":[{"id":"10","files":[]}]}]}`, false},
				{"invalid_file", `{"counter_groups":[{"key_type":8,"counters":[{"id":10,"files":[null]}]}]}`, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					h := jstest.LoadJSModule(t, "tool-integrations/neoprof_cpu_counters.js", jstest.WithXMLModule)
					engine := map[string]any{
						"getLocality": func() string { return locality },
						"readHostFile": func(actual string) string {
							require.Equal(t, "host", locality)
							require.Equal(t, path, actual)
							return tc.document
						},
						"execCommand": func(args []string, options map[string]any) map[string]any {
							require.Equal(t, "target", locality)
							require.Equal(t, []string{"cat", path}, args)
							require.Equal(t, true, options["asPrivileged"])
							return map[string]any{"rc": 0, "stdout": tc.document}
						},
					}
					rows, err := h.CallAwait[[]map[string]any](t, "loadSeriesRegistry", engine, map[string]any{"metadata": map[string]any{"neoprofAsPrivileged": true}}, "/capture")
					require.NoError(t, err)
					if !tc.valid {
						require.Nil(t, rows)
						return
					}
					if tc.name == "empty" {
						require.Empty(t, rows)
						return
					}
					require.Len(t, rows, 4)
					assert.EqualValues(t, 8, rows[0]["key_type"])
					assert.EqualValues(t, 10, rows[1]["series_id"])
					assert.EqualValues(t, 50000000, rows[1]["bin_duration"])
					assert.EqualValues(t, 12, rows[2]["series_id"])
					assert.EqualValues(t, 7, rows[3]["key_type"])
					assert.EqualValues(t, 20, rows[3]["series_id"])
					assert.Equal(t, "d.parquet", rows[3]["relative_path"])
				})
			}
		})
	}
}
