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
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing=%t", missing), func(t *testing.T) {
			h := jstest.LoadJSModule(t, "tool-integrations/jitdump.js")
			engine := &mocks.MockToolEngine{}
			engine.On("ExecCommand", []string{"cat", "/tools/jfr_schema.xml"}, tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Stdout: `<schema><event name="jdk.JVMInformation"/><event name="jdk.InitialSystemProperty"/><event name="jdk.GCHeapSummary"/><event name="jdk.GarbageCollection"/><event name="jdk.GCHeapMemoryPoolUsage"/></schema>`}, nil)).Once()
			for _, path := range []string{"metadata/jfr_recordings.parquet", "events/jfr_jvm_information.parquet", "events/jfr_initial_system_property.parquet", "events/jfr_gc_heap_summary.parquet", "events/jfr_garbage_collection.parquet", "events/jfr_gc_heap_memory_pool_usage.parquet"} {
				var rc int32
				if missing && path == "events/jfr_gc_heap_summary.parquet" {
					rc = 1
				}
				engine.On("ExecCommand", []string{"stat", "/parquet/" + path}, tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: rc}, nil)).Once()
			}
			_, err := h.CallAwait[any](t, "validateJfrParquetComponents", engine, "/parquet", "/tools/jitdump-jvm")
			if missing {
				require.ErrorContains(t, err, "incomplete")
			} else {
				require.NoError(t, err)
			}
			engine.AssertExpectations(t)
		})
	}
}
