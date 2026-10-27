// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
)

func TestNeoprofCPUCounterHostReadFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		cause    error
		internal bool
	}{
		{"missing", &fs.PathError{Op: "open", Path: "/capture/captured.xml", Err: fs.ErrNotExist}, false},
		{"permission_denied", &fs.PathError{Op: "open", Path: "/capture/captured.xml", Err: fs.ErrPermission}, false},
		{"internal", errors.New("unexpected host API failure"), true},
	} {
		for _, policy := range []string{"required", "optional"} {
			t.Run(test.name+"/"+policy, func(t *testing.T) {
				engine := &tool_mocks.MockEngineContext{}
				engine.On("ReadHostFile", "/capture/captured.xml").Return("", test.cause).Once()
				factory := loadNeoprofLifecycle(t, fmt.Sprintf("(engine) => scenarios.host_capture_read_failure(engine, %q)", policy))
				err := runCPUCounterScenario(t, factory, engine)
				if test.internal {
					require.ErrorContains(t, err, test.cause.Error())
				} else {
					require.NoError(t, err)
				}
				engine.AssertExpectations(t)
			})
		}
	}
}

func TestNeoprofCPUCounterHostRegistryReadFailures(t *testing.T) {
	const path = "/capture/report-new/apx/metadata/counter_series_files.json"
	for _, test := range []struct {
		name     string
		cause    error
		internal bool
	}{
		{"missing", &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}, false},
		{"permission_denied", &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission}, false},
		{"internal", errors.New("unexpected host API failure"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &tool_mocks.MockEngineContext{}
			engine.On("ReadHostFile", path).Return("", test.cause).Once()
			factory := loadNeoprofLifecycle(t, `async (engine) => {
    const rows = await cpuCounters.loadSeriesRegistry({
     getLocality: () => 'host', readHostFile: engine.readHostFile,
    }, { metadata: {} }, '/capture');
    if (rows !== undefined) throw new Error('Unreadable registry must not verify output');
   }`)
			err := runCPUCounterScenario(t, factory, engine)
			if test.internal {
				require.ErrorContains(t, err, test.cause.Error())
			} else {
				require.NoError(t, err)
			}
			engine.AssertExpectations(t)
		})
	}
}

// Helper tests exercise one responsibility at a time. Lifecycle tests load the
// production integration as well to cover orchestration and error handling.
func TestNeoprofCPUCounterDiscovery(t *testing.T) {
	runCPUCounterHelpers(t,
		"discovery_drains_both_streams",
		"large_streamed_catalogues",
		"streamed_discovery_failures",
		"heterogeneous_known_pmus_and_duplicate_selection",
		"unknown_and_ambiguous_pmus_do_not_collect_subset",
		"discovery_failures_are_not_unavailable",
		"stderr_diagnostics_do_not_block_cpu_discovery",
		"event_descriptions_are_not_discovery_errors",
		"invalid_scope_and_topology_do_not_resolve",
	)
}

func TestNeoprofCPUCounterConfiguration(t *testing.T) {
	runCPUCounterHelpers(t,
		"option_conflicts_and_legacy_defaults",
		"explicit_empty_writes_complete_configuration",
		"required_unavailable_stops_before_configuration",
		"optional_unavailable_uses_explicit_empty_and_warns",
		"configuration_write_or_readback_failure",
	)
}

func TestNeoprofCPUCounterVerification(t *testing.T) {
	runCPUCounterHelpers(t,
		"malformed_or_missing_series_output_is_unverified",
		"malformed_capture_and_registry_are_unverified",
		"malformed_registry_entries_reject_partial_rows",
		"verified_output_is_collected_and_published",
		"empty_series_requires_registered_schema_checked_output",
		"native_event_key_join_and_multiple_series",
		"required_partial_output_fails_and_optional_warns",
	)
}

func TestNeoprofCPUCounterErrorPropagation(t *testing.T) {
	runCPUCounterHelpers(t,
		"capability_id_errors_propagate",
		"output_registration_errors_propagate",
		"engine_command_errors_propagate",
		"host_file_api_errors_propagate",
		"outcome_publication_errors_propagate",
	)
}

func TestNeoprofCPUCounterLifecycle(t *testing.T) {
	for _, name := range []string{
		"selected_counters_require_timeline_export",
		"collector_arguments_apply_selection_before_workload",
		"android_launch_preserves_explicit_counter_selection",
		"timeline_metadata_failures_preserve_original_error_with_cpu_selection",
		"missing_timeline_output_is_not_published",
		"failed_optional_cpu_series_is_not_published_by_fallback",
	} {
		t.Run(name, func(t *testing.T) {
			factory := loadNeoprofLifecycle(t, fmt.Sprintf("scenarios[%q]", name))
			require.NoError(t, runCPUCounterScenario(t, factory, &tool_mocks.MockEngineContext{}))
		})
	}
}

func runCPUCounterHelpers(t *testing.T, names ...string) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "cpu_counters", "helpers.js"))
	require.NoError(t, err)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			source := ToolSourceBegin + fmt.Sprintf("run: async () => require(%q)[%q](),", path, name) + ToolSourceEnd
			factory, err := LoadFromSource(source, path)
			require.NoError(t, err)
			require.NoError(t, runCPUCounterScenario(t, factory, &tool_mocks.MockEngineContext{}))
		})
	}
}

func loadNeoprofLifecycle(t *testing.T, run string) *ScriptedToolSource {
	t.Helper()
	path := filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js")
	integrationSource, err := os.ReadFile(path)
	require.NoError(t, err)
	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "cpu_counters"))
	require.NoError(t, err)
	scenarios, err := os.ReadFile(filepath.Join(fixtureDir, "lifecycle.js"))
	require.NoError(t, err)
	source := string(integrationSource) + fmt.Sprintf("\nconst {createFixture} = require(%q);\n", filepath.Join(fixtureDir, "harness.js")) + string(scenarios) + "\ntool.run = " + run + ";"
	factory, err := LoadFromSource(source, path)
	require.NoError(t, err)
	require.Equal(t, "1.0", factory.CapabilityInterfaceVersion())
	return factory
}

func runCPUCounterScenario(t *testing.T, factory *ScriptedToolSource, engine *tool_mocks.MockEngineContext) error {
	t.Helper()
	integration, err := factory.NewIntegration(integrationContext(engine, nil))
	require.NoError(t, err)
	cleanup, err := integration.StartRuntime()
	require.NoError(t, err)
	defer cleanup()
	return integration.Run()
}

func TestNeoprofHardwareCatalogues(t *testing.T) {
	module, err := filepath.Abs(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof_cpu_counters.js"))
	require.NoError(t, err)
	for _, test := range []struct{ name, fixture string }{
		{"arm_linux", "arm.json"},
		{"x86_linux", "x86.json"},
		{"android", "pixel7.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, err := os.ReadFile(filepath.Join("testdata", "cpu_counters", "catalogues", test.fixture))
			require.NoError(t, err)
			source := ToolSourceBegin + fmt.Sprintf(`run: async () => {
   const cpu=require(%q), fixture=%s;
   const actual=cpu.resolveCatalogue(fixture);
   if(actual.capability.state!=='available') throw Error('Hardware catalogue must resolve');
   if(JSON.stringify(actual.native)!==JSON.stringify(fixture.expected)) throw Error('Wrong native selections: '+JSON.stringify(actual.native));
  },`, module, fixture) + ToolSourceEnd
			factory, err := LoadFromSource(source, "hardware_catalogue.js")
			require.NoError(t, err)
			require.NoError(t, runCPUCounterScenario(t, factory, &tool_mocks.MockEngineContext{}))
		})
	}
}
