// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipejstest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
)

func TestASCTCSVColumnLabelsAndValues(t *testing.T) {
	fixture := newASCTUnitsFixture(t, "legacy-mbps")
	checks := []struct {
		id, column string
		want       float64
	}{
		{"asct_csv_benchmark_bandwidth_sweep", "Bandwidth [GB/s]", 107.3741824},
		{"asct_csv_benchmark_peak_bandwidth", "Peak bandwidth [GB/s]", 107.3741824},
		{"asct_csv_benchmark_loaded_latency", "Bandwidth [GB/s]", 107.3741824},
		{"asct_csv_benchmark_loaded_latency", "% of Peak Theoretical", 50},
		{"asct_csv_benchmark_cross_numa_bandwidth", "Node 0 [GB/s]", 107.3741824},
		{"asct_csv_benchmark_idle_latency", "Node 0 [ns]", 90},
		{"asct_csv_benchmark_latency_sweep", "Lower bound [bytes]", 1024},
		{"asct_csv_benchmark_latency_sweep", "Upper bound [bytes]", 4096},
		{"asct_csv_benchmark_latency_sweep", "Optimum data size [bytes]", 2048},
		{"asct_csv_benchmark_bandwidth_sweep", "Data size used [bytes]", 2048},
		{"asct_csv_benchmark_peak_bandwidth", "Optional metric", 7},
		{"asct_csv_benchmark_latency_sweep", "User override [bytes]", 2048},
		{"asct_csv_benchmark_c2c_latency", "Latency [ns]", 12.5},
	}
	for _, check := range checks {
		t.Run(check.id+"/"+check.column, func(t *testing.T) {
			require.Contains(t, fixture.tables, check.id)
			var value float64
			query := fmt.Sprintf(`SELECT "%s" FROM "%s" LIMIT 1`, check.column, fixture.tables[check.id])
			require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&value))
			require.InDelta(t, check.want, value, 1e-9, check.column)
		})
	}
}

func TestASCTDetailedBandwidthUnits(t *testing.T) {
	// Only the detailed JSON metric name differs between these collector formats.
	for _, format := range []string{"legacy-mbps", "legacy-total-bandwidth"} {
		t.Run(format, func(t *testing.T) {
			fixture := newASCTUnitsFixture(t, format)
			var bandwidth float64
			query := fmt.Sprintf(`SELECT try_cast(value AS DOUBLE) FROM "%s" WHERE metric = 'total_bandwidth_mbps' AND row_key = '0'`, fixture.tables["asct_json_benchmark_bandwidth_sweep_bandwidthSweepLineChart"])
			require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&bandwidth))
			require.InDelta(t, 107374.1824, bandwidth, 1e-9)
			query = fmt.Sprintf(`SELECT try_cast(value AS DOUBLE) FROM "%s" WHERE metric = 'PMU_bw [MB/s]' AND row_key = '0'`, fixture.tables["asct_json_benchmark_bandwidth_sweep_bandwidthSweepLineChart"])
			require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&bandwidth))
			require.InDelta(t, 104.8576, bandwidth, 1e-9)
			var missing bool
			query = fmt.Sprintf(`SELECT try_cast(value AS DOUBLE) IS NULL FROM "%s" WHERE metric = 'total_bandwidth_mbps' AND row_key = '1'`, fixture.tables["asct_json_benchmark_bandwidth_sweep_bandwidthSweepLineChart"])
			require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&missing))
			require.True(t, missing)
		})
	}
}

func TestASCTSystemInformationUnits(t *testing.T) {
	fixture := newASCTUnitsFixture(t, "legacy-mbps")
	var cacheLineSize string
	var theoreticalBandwidth, memoryCapacity float64
	for key, count := range map[string]string{
		"sys_hw.caches.L1D 64KiB 4-way 64b-line":   "64",
		"sys_hw.caches.L2U 1MiB 8-way 64b-line":    "64",
		"sys_hw.caches.L3U 1.5GiB 16-way 64b-line": "2",
		"sys_hw.caches.L3U 1TiB":                   "1",
		"sys_hw.caches.L1D 32KiB 4-way 64b-line":   "32",
		"sys_hw.caches.L2U 2MiB 8-way 64b-line":    "16",
		"sys_hw.caches.L3U 2.5GiB 16-way 64b-line": "4",
		"sys_hw.caches.L3U 2TiB":                   "2",
		"sys_hw.caches.L1I 512MiB 8-way 64b-line":  "3",
		"sys_hw.caches.unknown":                    "4",
	} {
		var cacheCount string
		query := fmt.Sprintf(`SELECT value FROM "%s" WHERE key = ?`, fixture.tables["asct_key_value_system_info"])
		require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query, key).Scan(&cacheCount), key)
		require.Equal(t, count, cacheCount, key)
	}
	query := fmt.Sprintf(`SELECT value FROM "%s" WHERE key = 'sys_hw.cache_line_size'`, fixture.tables["asct_key_value_system_info"])
	require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&cacheLineSize))
	require.Equal(t, "64", cacheLineSize)
	query = fmt.Sprintf(`SELECT try_cast(value AS DOUBLE) FROM "%s" WHERE key = 'memory.peak_theoretical_bandwidth [bytes/s]'`, fixture.tables["asct_key_value_system_info"])
	require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&theoreticalBandwidth))
	require.Equal(t, 200000000000.0, theoreticalBandwidth)
	query = fmt.Sprintf(`SELECT try_cast(value AS DOUBLE) FROM "%s" WHERE key = 'memory.total_size [bytes]'`, fixture.tables["asct_key_value_system_info"])
	require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&memoryCapacity))
	require.Equal(t, float64(1<<30), memoryCapacity)
}

func TestASCTMCPQueryExamples(t *testing.T) {
	fixture := newASCTUnitsFixture(t, "legacy-mbps")
	guide, err := os.ReadFile(filepath.Join("..", "..", "apap-cli", "service", "mcpserver", "insights", "asct_run_query.md"))
	require.NoError(t, err)
	checkedMCPQueries := map[string]int{}
	for _, block := range strings.Split(strings.ReplaceAll(string(guide), "\r\n", "\n"), "```sql\n")[1:] {
		query, _, _ := strings.Cut(block, "```")
		matched := false
		for placeholder, id := range map[string]string{
			"<system_info_table>":             "asct_key_value_system_info",
			"<latency_sweep_summary_table>":   "asct_csv_benchmark_latency_sweep",
			"<bandwidth_sweep_summary_table>": "asct_csv_benchmark_bandwidth_sweep",
			"<peak_bandwidth_table>":          "asct_csv_benchmark_peak_bandwidth",
			"<loaded_latency_table>":          "asct_csv_benchmark_loaded_latency",
			"<c2c_latency_table>":             "asct_csv_benchmark_c2c_latency",
			"<numa_matrix_table>":             "asct_csv_benchmark_idle_latency",
			"<detailed_sweep_table>":          "asct_json_benchmark_bandwidth_sweep_bandwidthSweepLineChart",
		} {
			if !strings.Contains(query, placeholder) {
				continue
			}
			resolved := strings.ReplaceAll(query, placeholder, `"`+fixture.tables[id]+`"`)
			resolved = strings.ReplaceAll(resolved, "<value_metric>", "total_bandwidth_mbps")
			rows, err := fixture.session.Database().Conn.QueryContext(context.Background(), resolved)
			require.NoError(t, err, placeholder)
			require.True(t, rows.Next(), placeholder)
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			checkedMCPQueries[placeholder]++
			matched = true
		}
		if !strings.Contains(query, "<numa_bandwidth_table>") {
			require.True(t, matched, "unrecognized MCP SQL example: %s", query)
			continue
		}
		query = strings.ReplaceAll(query, "<numa_bandwidth_table>", `"`+fixture.tables["asct_csv_benchmark_cross_numa_bandwidth"]+`"`)
		var localBandwidth float64
		require.NoError(t, fixture.session.Database().Conn.QueryRowContext(context.Background(), query).Scan(&localBandwidth))
		require.InDelta(t, 214.7483648, localBandwidth, 1e-9)
		checkedMCPQueries["<numa_bandwidth_table>"]++
	}
	require.Equal(t, map[string]int{
		"<system_info_table>":             1,
		"<latency_sweep_summary_table>":   1,
		"<bandwidth_sweep_summary_table>": 1,
		"<detailed_sweep_table>":          1,
		"<numa_matrix_table>":             1,
		"<numa_bandwidth_table>":          1,
		"<peak_bandwidth_table>":          1,
		"<loaded_latency_table>":          1,
		"<c2c_latency_table>":             2,
	}, checkedMCPQueries, "execute every expected MCP SQL example")
}

func TestASCTVisualizationQueries(t *testing.T) {
	fixture := newASCTUnitsFixture(t, "legacy-mbps")
	// Require every expected visualization query so removing or breaking a
	// configuration cannot silently remove its coverage.
	checkedAnalysis := false
	for _, visualization := range fixture.output.Widgets {
		if visualization.Type != "asct_analysis" {
			continue
		}
		encoded, err := json.Marshal(visualization.Config)
		require.NoError(t, err)
		var config map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(encoded, &config))
		for _, key := range []string{
			"bandwidthSweepLineChart", "latencySweepLineChart", "loadedLatencyLineChart",
			"bandwidthSweepGrid", "latencySweepGrid", "loadedLatencyGrid", "peakBandwidthGrid",
			"numaBandwidthMatrix", "numaLatencyMatrix", "coreToCoreLatencyHeatmap", "cacheSizeMarkers",
		} {
			t.Run(key, func(t *testing.T) {
				require.Contains(t, config, key)
				var widget struct {
					XAxisValueFormat, YAxisValueFormat string
					YAxisMin                           json.RawMessage
					CustomQuery                        struct{ Query, TableNamePlaceholder string }
					DataSource                         struct {
						Tables map[string][]struct {
							RendererID string `json:"renderer_id"`
						}
					} `json:"data_source"`
				}
				require.NoError(t, json.Unmarshal(config[key], &widget), key)
				require.Len(t, widget.DataSource.Tables["table"], 1, key)
				id := widget.DataSource.Tables["table"][0].RendererID
				require.Contains(t, fixture.tables, id, key)
				// #nosec G201 -- the table name is generated by the render manifest in this test.
				query := fmt.Sprintf(`SELECT * FROM "%s"`, fixture.tables[id])
				if key == "numaBandwidthMatrix" || key == "numaLatencyMatrix" {
					// Matrices use the original columns; only the GUI shortens visible labels.
					require.Empty(t, widget.CustomQuery.Query, key)
				} else {
					require.NotEmpty(t, widget.CustomQuery.Query, key)
					require.NotEmpty(t, widget.CustomQuery.TableNamePlaceholder, key)
					query = strings.ReplaceAll(widget.CustomQuery.Query, widget.CustomQuery.TableNamePlaceholder, `"`+fixture.tables[id]+`"`)
				}
				rows, err := fixture.session.Database().Conn.QueryContext(context.Background(), query)
				require.NoError(t, err, key)
				defer rows.Close()
				columns, err := rows.Columns()
				require.NoError(t, err)
				require.True(t, rows.Next(), key)
				switch key {
				case "numaBandwidthMatrix":
					require.Equal(t, []string{"column0", "Node 0 [GB/s]", "Node 1 [GB/s]"}, columns)
				case "numaLatencyMatrix":
					require.Equal(t, []string{"column0", "Node 0 [ns]", "Node 1 [ns]"}, columns)
				case "bandwidthSweepLineChart":
					require.Nil(t, widget.YAxisMin, "use automatic logarithmic bounds")
					var size, bandwidth float64
					require.NoError(t, rows.Scan(&size, &bandwidth))
					require.Equal(t, 2048.0, size)
					require.InDelta(t, 107374.1824, bandwidth, 1e-9)
					require.Equal(t, "iec-bytes", widget.XAxisValueFormat)
					require.Equal(t, "raw", widget.YAxisValueFormat)
				case "latencySweepLineChart":
					require.Nil(t, widget.YAxisMin, "use automatic logarithmic bounds")
					var size, latency float64
					require.NoError(t, rows.Scan(&size, &latency))
					require.Equal(t, 2048.0, size)
					require.Equal(t, 2e-9, latency)
					require.Equal(t, "iec-bytes", widget.XAxisValueFormat)
					require.Equal(t, "seconds", widget.YAxisValueFormat)
				case "loadedLatencyLineChart":
					var percent, latency float64
					require.NoError(t, rows.Scan(&percent, &latency))
					require.Equal(t, 50.0, percent)
					require.Equal(t, 1e-7, latency)
					require.Equal(t, "seconds", widget.YAxisValueFormat)
				case "cacheSizeMarkers":
					for i, want := range []struct {
						name string
						size float64
					}{
						{"L1D cache", 32 * 1024},
						{"L1D cache", 64 * 1024},
						{"L2U cache", 1024 * 1024},
						{"L2U cache", 2 * 1024 * 1024},
						{"L3U cache", 1.5 * 1024 * 1024 * 1024},
						{"L3U cache", 2.5 * 1024 * 1024 * 1024},
						{"L3U cache", 1024 * 1024 * 1024 * 1024},
						{"L3U cache", 2 * 1024 * 1024 * 1024 * 1024},
					} {
						if i > 0 {
							require.True(t, rows.Next())
						}
						var name string
						var size float64
						require.NoError(t, rows.Scan(&name, &size))
						require.Equal(t, want.name, name)
						require.Equal(t, want.size, size)
					}
					require.False(t, rows.Next())
					require.NoError(t, rows.Err())
				}
				require.NoError(t, rows.Close())
			})
		}
		checkedAnalysis = true
	}
	require.True(t, checkedAnalysis, "ASCT analysis visualization must be present")
}

func TestASCTRenderingPreservesCapturedFiles(t *testing.T) {
	fixture := newASCTUnitsFixture(t, "legacy-mbps")
	// Rendering must never rewrite the persisted capture.
	for name, want := range fixture.files {
		data, err := os.ReadFile(filepath.Join(fixture.root, "tool/asct/0/output", name))
		require.NoError(t, err)
		require.Equal(t, want, string(data), name)
	}
}

type asctUnitsFixture struct {
	root    string
	files   map[string]string
	session render.Session
	output  recipe.RenderOutput
	tables  map[string]string
}

// Exercise the installed recipe and actual renderers: these tables are shared
// by the GUI, CLI and MCP server. Each test owns its session and temporary files.
func newASCTUnitsFixture(t *testing.T, format string) asctUnitsFixture {
	t.Helper()
	root := t.TempDir()
	files := asctUnitFixture(format)
	var entries []cdf.ManifestEntry
	for name, data := range files {
		path := "tool/asct/0/output/" + name
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(data), 0644))
		entries = append(entries, cdf.ManifestEntry{Path: path, ComponentType: cdf.ComponentType{Name: "file", SchemaVersion: "1.0"}})
	}
	model := cdf.NewOnDiskModel(root, &cdf.Manifest{Entries: entries}, cdf.Metadata{})
	output := executeRenderStageWithCapabilities(t, parseRecipeFile(t, "asct.js"),
		[]*run.RunDescription{{RecipeName: "asct", Parameters: map[string]any{
			"system_info_only": false, "default_benchmarks": false,
			"benchmark_idle_latency": true, "benchmark_peak_bandwidth": true,
			"benchmark_cross_numa_bandwidth": true, "benchmark_bandwidth_sweep": true,
			"benchmark_latency_sweep": true, "benchmark_loaded_latency": true,
			"benchmark_c2c_latency": true,
		}}}, []cdf.ModelView{model}, map[string]any{}, []run.RunCapabilities{{}}, false)
	session := newRenderSession(t, "asct-units", root, model)
	initializeRenderers(t, session, output, nil)

	tables := map[string]string{}
	for _, entry := range session.Manifest().Entries() {
		if id := entry.Info().RendererIdentity().ID; id != nil {
			tables[*id] = entry.TableName()
		}
	}
	return asctUnitsFixture{root: root, files: files, session: session, output: output, tables: tables}
}

func asctUnitFixture(format string) map[string]string {
	files := map[string]string{
		"bandwidth-sweep.csv":         ",Datasize Used,Level,Bandwidth [GB/s]\n0,2048,L1,107.3741824\n",
		"peak-bandwidth.csv":          ",Traffic type,Peak BW [GB/s],% of Peak Theoretical,Optional metric\n0,Read only,107.3741824,50,7\n",
		"loaded-latency.csv":          ",Injected NOPs,Loaded latency [ns],Bandwidth [GB/s],% of Peak Theoretical BW\n0,0,100,107.3741824,50\n",
		"cross-numa-bandwidth.csv":    ",Node 0,Node 1\nNode 0,107.3741824,53.6870912\nNode 1,53.6870912,107.3741824\n",
		"idle-latency.csv":            ",Node 0,Node 1\nNode 0,90,180\nNode 1,180,90\n",
		"latency-sweep.csv":           ",Lower Bound,Upper Bound,Optimum Datasize,User override,Latency [ns]\nL1,1024,4096,2048,2048,2\n",
		"c2c-latency.csv":             ",CPUA,CPUB,LATENCY,CPUA_NODE,MEMBIND_NODE\n0,0,1,12.5,0,0\n",
		"bandwidth-sweep.ubench.json": `{"sizes":{"0":2048,"1":4096},"total_bandwidth_mbps":{"0":107374.1824,"1":null},"PMU_bw [MB/s]":{"0":104.8576}}`,
		"latency-sweep.ubench.json":   `{"sizes":{"0":2048},"average_latency_ns":{"0":2}}`,
		"system-info.csv": strings.Join([]string{
			"sys_hw.caches.L1D 64K 4-way 64b-line,64",
			"sys_hw.caches.L2U 1M 8-way 64b-line,64",
			"sys_hw.caches.L3U 1.5G 16-way 64b-line,2",
			"sys_hw.caches.L3U 1T,1",
			"sys_hw.caches.L1D 32k 4-way 64b-line,32",
			"sys_hw.caches.L2U 2m 8-way 64b-line,16",
			"sys_hw.caches.L3U 2.5g 16-way 64b-line,4",
			"sys_hw.caches.L3U 2t,2",
			"sys_hw.caches.L1I 512MiB 8-way 64b-line,3",
			"sys_hw.caches.unknown,4",
			"sys_hw.cache_line_size,64",
			"memory.total_size,1073741824",
			"memory.peak_theoretical_bw,200000000000",
		}, "\n") + "\n",
	}
	if format == "legacy-total-bandwidth" {
		files["bandwidth-sweep.ubench.json"] = strings.ReplaceAll(files["bandwidth-sweep.ubench.json"], "total_bandwidth_mbps", "total_bandwidth")
	}
	return files
}
