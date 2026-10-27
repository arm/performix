// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestJfrCaptureArguments(t *testing.T) {
	h := jstest.LoadJSModule(t, "tool-integrations/jitdump.js")
	args, err := h.Call[[]string](t, "buildJfrCaptureArgs", "/path with spaces/jfr", "recording", "profile")
	require.NoError(t, err)
	assert.Equal(t, []string{"--jfr-output-dir", "/path with spaces/jfr", "--jfr-name", "recording", "--jfr-settings", "profile"}, args)
}

func TestJitdumpFiltersAttachedRuntime(t *testing.T) {
	for _, jvm := range []bool{false, true} {
		for _, dotnet := range []bool{false, true} {
			t.Run(fmt.Sprintf("JVM=%t/dotnet=%t", jvm, dotnet), func(t *testing.T) {
				h := jstest.LoadJSModule(t, "tool-integrations/jitdump.js")
				engine := &mocks.MockToolEngine{}
				jvmPath := ""
				if jvm {
					jvmPath = "/tmp/hsperfdata_user/42\n"
				}
				dotnetPath := ""
				if dotnet {
					dotnetPath = "/tmp/dotnet-diagnostic-42-0-socket\n"
				}
				// A matching path remains authoritative when find encounters unrelated permission errors.
				engine.On("ExecCommand", []string{"find", "/tmp", "-maxdepth", "2", "-path", "/tmp/hsperfdata_*/42", "-print", "-quit"}, tool_goja.ExecOptions{AsPrivileged: true}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: 1, Stdout: jvmPath}, nil)).Once()
				engine.On("ExecCommand", []string{"find", "/tmp", "-path", "/tmp/dotnet-diagnostic-42*", "-print", "-quit"}, tool_goja.ExecOptions{AsPrivileged: true}).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: dotnetPath}, nil)).Once()
				out, err := h.CallAwait[map[string]bool](t, "filterJitdumpAgentsForPid", engine, 42, true)
				require.NoError(t, err)
				assert.Equal(t, map[string]bool{"isJvmPid": jvm, "isDotnetPid": dotnet}, out)
				engine.AssertExpectations(t)
			})
		}
	}
}

func TestJfrRequiredComponents(t *testing.T) {
	for _, missingPath := range []string{"", "events/jfr_gc_heap_summary.parquet", "events/jvm_flags.parquet"} {
		t.Run(fmt.Sprintf("missing=%s", missingPath), func(t *testing.T) {
			h := jstest.LoadJSModule(t, "tool-integrations/jitdump.js")
			engine := &mocks.MockToolEngine{}
			engine.On("ExecCommand", []string{"cat", "/tools/jfr_schema.xml"}, tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<schema><event name="jdk.JVMInformation"/><event name="jdk.InitialSystemProperty"/><event name="jdk.GCHeapSummary"/><event name="jdk.GarbageCollection"/><event name="jdk.GCHeapMemoryPoolUsage"/><event name="jdk.BooleanFlag" rust_handler="jvm_flags"/><event name="jdk.LongFlag" rust_handler="jvm_flags"/></schema>`}, nil)).Once()
			for _, path := range []string{"metadata/jfr_recordings.parquet", "events/jfr_jvm_information.parquet", "events/jfr_initial_system_property.parquet", "events/jfr_gc_heap_summary.parquet", "events/jfr_garbage_collection.parquet", "events/jfr_gc_heap_memory_pool_usage.parquet", "events/jvm_flags.parquet"} {
				var rc int32
				if path == missingPath {
					rc = 1
				}
				engine.On("ExecCommand", []string{"stat", "/parquet/" + path}, tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: rc}, nil)).Once()
			}
			_, err := h.CallAwait[any](t, "validateJfrParquetComponents", engine, "/parquet", "/tools/jitdump-jvm")
			if missingPath != "" {
				require.ErrorContains(t, err, "incomplete")
			} else {
				require.NoError(t, err)
			}
			engine.AssertExpectations(t)
		})
	}
}

func TestJfrFlagsManifest(t *testing.T) {
	for _, tc := range []struct {
		name, schema, errorText string
		paths                   []string
	}{
		{name: "shared handler", schema: `<event name="jdk.BooleanFlag" rust_handler="jvm_flags"/><event rust_handler='jvm_flags' name='jdk.LongFlag'/><event name="jdk.JVMInformation" rust_handler="jvm_information"/>`, paths: []string{"metadata/jfr_recordings.parquet", "events/jvm_flags.parquet", "events/jfr_jvm_information.parquet"}},
		{name: "duplicate event", schema: `<event name="jdk.BooleanFlag" rust_handler="jvm_flags"/><event name="jdk.BooleanFlag" rust_handler="jvm_flags"/>`, errorText: "duplicate event"},
		{name: "output collision", schema: `<event name="one.Event"/><event name="two.Event"/>`, errorText: "duplicate output"},
		{name: "ordinary handler output collision", schema: `<event name="one.Event" rust_handler="jvm_information"/><event name="two.Event" rust_handler="jvm_information"/>`, errorText: "duplicate output"},
		{name: "flags without shared handler", schema: `<event name="jdk.BooleanFlag"/><event name="jdk.LongFlag"/>`, paths: []string{"metadata/jfr_recordings.parquet", "events/jfr_boolean_flag.parquet", "events/jfr_long_flag.parquet"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := jstest.LoadJSModule(t, "tool-integrations/jitdump.js")
			paths, err := h.Call[[]string](t, "parquetManifestFromJfrSchema", tc.schema)
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.paths, paths)
			}
		})
	}
}
