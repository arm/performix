// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func TestNeoprofStagesAndroidGPUTimelineLayer(t *testing.T) {
	for _, tc := range []struct {
		name             string
		enabled, missing bool
		copyRC           int32
	}{
		{name: "enabled", enabled: true},
		{name: "disabled"},
		{name: "missing layer", enabled: true, missing: true},
		{name: "copy failure", enabled: true, copyRC: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			h := jstest.LoadJSModule(t, "tool-integrations/neoprof_gpu_timeline.js")
			ctx := jstest.EmptyToolContext()
			ctx.Params["collect_gpu_timeline"] = tc.enabled
			ctx.Workload = tool_goja.NewWorkloadAndroidLaunch("com.example.ml", ".MainActivity", "")
			engine := &mocks.MockToolEngine{}
			t.Cleanup(func() { engine.AssertExpectations(t) })
			if tc.enabled {
				source := "/tools/gpulayers/9.8.0.ngp_b7193aa2_3/bin/libVkLayerGPUTimeline.so"
				engine.On("ToolsRoot").Return("/tools", nil).Once()
				engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Android}, nil).Once()
				rc := int32(0)
				if tc.missing {
					rc = 1
					engine.On("GetLocality").Return("target", nil).Once()
				}
				engine.On("ExecCommand", []string{"stat", source}, tool_goja.ExecOptions{}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: rc}, nil)).Once()
				if !tc.missing {
					engine.On("ExecCommand", []string{"cp", source, "/data/local/tmp/libVkLayerGPUTimeline.so"}, tool_goja.ExecOptions{AsPrivileged: false}).Return(h.ToJSValPromise(t, process.CommandResult{Rc: tc.copyRC, Stderr: "copy failed"}, nil)).Once()
				}
			}
			// When
			_, err := h.CallAwait[any](t, "prepare", engine, ctx)
			// Then
			if tc.missing || tc.copyRC != 0 {
				var msg message.Message
				require.ErrorAs(t, err, &msg)
				if tc.missing {
					require.Equal(t, message.ToolIntegrationsCommonToolNotDeployed, msg.Code())
					require.Equal(t, "gpulayers", msg.Metadata()["tool"])
				} else {
					require.Equal(t, message.ToolIntegrationsNeoprofNeoprofFailed, msg.Code())
					require.ErrorContains(t, msg.Unwrap(), "copy failed")
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
