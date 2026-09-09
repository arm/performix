// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package jstest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/gojautils"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

func loadTestToolIntegration(t *testing.T, source string) *ToolIntegrationJSHarness {
	t.Helper()

	toolPath := filepath.Join(t.TempDir(), "test.js")
	require.NoError(t, os.WriteFile(toolPath, []byte(source), 0o600))

	originalResolveJSFilePath := resolveJSFilePath
	resolveJSFilePath = func(_ *testing.T, _ string) string {
		return toolPath
	}
	t.Cleanup(func() {
		resolveJSFilePath = originalResolveJSFilePath
	})

	return LoadToolIntegration(t, "test")
}

func TestToolIntegrationJSHarness(t *testing.T) {
	t.Run("loads properties and executes lifecycle stages", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				supportsWorkloadLaunch: true,
				deployments: [],
				migrations: [{
					type: "rename",
					from: "a",
					to: "b",
					version: "1",
					oldSuffix: "x",
					newSuffix: "y",
				}],
				description: {
					short: "short",
					long: "long",
				},
				probe: async (engine, ctx) => {
					await engine.log("probe", ctx.params.value);
					return {
						available: true,
						capabilities: { mode: "a" },
						advice: [{ level: "info", messageCode: "a" }],
					};
				},
				run: async (engine, ctx) => engine.log("run", ctx.params.value),
				reformat: async (engine, ctx) => engine.log("reformat", ctx.params.value),
				onStop: async (engine, ctx) => engine.log("stop", ctx.params.value),
				onCancel: async (engine, ctx) => engine.log("cancel", ctx.params.value),
			};
		`)
		engine := &mocks.MockToolEngine{}
		toolContext := tool_goja.ToolContext{Params: map[string]any{"value": "a"}}
		for _, stage := range []string{"probe", "run", "reformat", "stop", "cancel"} {
			engine.On("Log", stage, "a").Return(nil).Once()
		}

		require.Equal(t, tool.IntegrationProperties{
			Name:                   "a",
			Version:                "1",
			Deployments:            []deploymentsupport.DeploymentDeclaration{},
			SupportsWorkloadLaunch: true,
			ShortDescription:       "short",
			LongDescription:        "long",
			Migrations: []tool.Migration{{
				Type:      "rename",
				From:      "a",
				To:        "b",
				Version:   "1",
				OldSuffix: "x",
				NewSuffix: "y",
			}},
		}, harness.ToolProperties())

		probeResult, err := harness.ToolProbe(t, engine, toolContext)
		require.NoError(t, err)
		require.Equal(t, tool.ProbeResult{
			Available:    true,
			Capabilities: map[string]any{"mode": "a"},
			Advice: []tool.ProbeAdvice{{
				Level:       "info",
				MessageCode: "a",
			}},
		}, probeResult)
		require.NoError(t, harness.ToolRun(t, engine, toolContext))
		require.NoError(t, harness.ToolReformat(t, engine, toolContext))
		require.NoError(t, harness.ToolStop(t, engine, toolContext))
		require.NoError(t, harness.ToolCancel(t, engine, toolContext))
		engine.AssertExpectations(t)
	})

	t.Run("composes engines returned by withLocality", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: async (engine) => {
					const hostEngine = engine.withLocality("host");
					const locality = hostEngine.getLocality();
					await hostEngine.log("info", locality);
				},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
		engine := &mocks.MockToolEngine{}
		hostEngine := &mocks.MockToolEngine{}
		engine.On("WithLocality", "host").Return(hostEngine, nil).Once()
		hostEngine.On("GetLocality").Return("host", nil).Once()
		hostEngine.On("Log", "info", "host").Return(nil).Once()

		err := harness.ToolRun(t, engine, tool_goja.ToolContext{})

		require.NoError(t, err)
		engine.AssertExpectations(t)
		hostEngine.AssertExpectations(t)
	})

	t.Run("binds asynchronous engine methods as promises", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: async (engine) => {
					const result = engine.createTempDir();
					if (!(result instanceof Promise)) {
						throw new Error("createTempDir did not return a promise");
					}
					if ((await result) !== "/tmp/mock") {
						throw new Error("promise resolved to the wrong value");
					}
				},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
		engine := &mocks.MockToolEngine{}
		engine.On("CreateTempDir").Return(harness.ToJSValPromise(t, "/tmp/mock", nil)).Once()

		require.NoError(t, harness.ToolRun(t, engine, tool_goja.ToolContext{}))
		engine.AssertExpectations(t)
	})

	t.Run("returns run errors", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: () => { throw new Error("boom"); },
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)

		err := harness.ToolRun(t, &mocks.MockToolEngine{}, EmptyToolContext())

		require.Error(t, err)
		var scriptErr *gojautils.ScriptError
		require.ErrorAs(t, err, &scriptErr)
		require.Equal(t, "boom", scriptErr.Message)
	})

	t.Run("passes complete tool context to JavaScript", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: (_engine, ctx) => {
					if (ctx.params.value !== "a") throw new Error("unexpected params");
					if (ctx.workload.type !== "launch") throw new Error("unexpected workload type");
					if (ctx.workload.rawCommand !== "a") throw new Error("unexpected raw command");
					if (ctx.workload.command.join(",") !== "a,b") throw new Error("unexpected command");
					if (ctx.workload.environment.A !== "a") throw new Error("unexpected workload environment");
					if (ctx.workload.workingDir !== "/a") throw new Error("unexpected workload working directory");
					if (ctx.workload.useShell !== true) throw new Error("unexpected use-shell value");
					if (ctx.workingDir !== "/b") throw new Error("unexpected working directory");
					if (ctx.env.B !== "b") throw new Error("unexpected environment");
					if (ctx.timeout !== 1) throw new Error("unexpected timeout");
					if (ctx.toolsRoot !== "/c") throw new Error("unexpected tools root");
					if (ctx.metadata.value !== "b") throw new Error("unexpected metadata");
				},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
		toolContext := tool_goja.ToolContext{
			Params: map[string]any{"value": "a"},
			Workload: tool_goja.NewWorkloadLaunch(
				"a",
				[]string{"a", "b"},
				map[string]string{"A": "a"},
				"/a",
				true,
			),
			WorkingDir: "/b",
			Env:        map[string]string{"B": "b"},
			Timeout:    1,
			ToolsRoot:  "/c",
			Metadata:   map[string]any{"value": "b"},
		}

		require.NoError(t, harness.ToolRun(t, &mocks.MockToolEngine{}, toolContext))
	})

	t.Run("converts complete probe result", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({
					available: true,
					capabilities: { mode: "a" },
					advice: [{
						level: "warning",
						messageCode: "a",
						metadata: { value: "a" },
						cause: "because",
					}],
				}),
				run: () => {},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)

		result, err := harness.ToolProbe(t, &mocks.MockToolEngine{}, EmptyToolContext())

		require.NoError(t, err)
		require.Equal(t, tool.ProbeResult{
			Available:    true,
			Capabilities: map[string]any{"mode": "a"},
			Advice: []tool.ProbeAdvice{{
				Level:       "warning",
				MessageCode: "a",
				Metadata:    map[string]string{"value": "a"},
				Cause:       "because",
			}},
		}, result)
	})

	t.Run("returns rejected asynchronous engine errors", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: async (engine) => { await engine.createTempDir(); },
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
		engine := &mocks.MockToolEngine{}
		engine.On("CreateTempDir").Return(harness.ToJSValPromise(t, nil, errors.New("boom"))).Once()

		err := harness.ToolRun(t, engine, EmptyToolContext())

		require.Error(t, err)
		var scriptErr *gojautils.ScriptError
		require.ErrorAs(t, err, &scriptErr)
		require.Equal(t, "boom", scriptErr.Message)
		engine.AssertExpectations(t)
	})

	t.Run("selects timeout before delayed engine promise", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: async (engine) => {
					const winner = await Promise.race([
						engine.createTempDir().then(() => "promise"),
						new Promise((resolve) => setTimeout(() => resolve("timeout"), 5)),
					]);
					await engine.log("warn", winner);
				},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
		release := make(chan struct{})
		var releaseOnce sync.Once
		releasePromise := func() {
			releaseOnce.Do(func() { close(release) })
		}
		t.Cleanup(releasePromise)
		engine := &mocks.MockToolEngine{}
		engine.On("CreateTempDir").
			Return(harness.ToJSCustomPromise(t, func() (any, error) {
				<-release
				return "/a", nil
			})).
			Once()
		engine.On("Log", mock.Anything, "timeout").Return(nil).Once()

		err := harness.ToolRun(t, engine, EmptyToolContext())
		releasePromise()

		require.NoError(t, err)
		engine.AssertExpectations(t)
	})
}

func TestToJSProcessHandle(t *testing.T) {
	t.Run("exposes configured process handle features", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			async function inspectProcessHandle(handle) {
				const stdout = [];
				const stderr = [];
				await Promise.all([
					forAwait(handle.stdout, (value) => stdout.push(value)),
					forAwait(handle.stderr, (value) => stderr.push(value)),
				]);
				await handle.writeStdin("a");
				await handle.kill();
				await handle.interrupt();
				const waitResult = await handle.wait();
				return { pid: handle.pid(), stdout, stderr, exitCode: waitResult.exitCode };
			}

			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: () => {},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
		handle := &tool_mocks.MockProcessHandle{}
		handle.On("Stdout").Return(bytes.NewBufferString("out")).Once()
		handle.On("Stderr").Return(bytes.NewBufferString("err")).Once()
		handle.On("WriteStdin", "a").Return(nil).Once()
		handle.On("Kill").Return(nil).Once()
		handle.On("Interrupt").Return(nil).Once()
		handle.On("Wait").Return(2, nil).Once()
		handle.On("PID").Return(1).Once()
		jsHandle := harness.ToJSProcessHandle(t, handle, tool_goja.ProcessHandleBindingOptions{
			StdinOpen:          true,
			StdoutRedirectMode: process.Stream,
			StderrRedirectMode: process.Stream,
		})
		type processResult struct {
			PID      int      `json:"pid"`
			Stdout   []string `json:"stdout"`
			Stderr   []string `json:"stderr"`
			ExitCode int      `json:"exitCode"`
		}

		result, err := harness.CallAwait[processResult](t, "inspectProcessHandle", jsHandle)

		require.NoError(t, err)
		require.Equal(t, 1, result.PID)
		require.Equal(t, []string{"out"}, result.Stdout)
		require.Equal(t, []string{"err"}, result.Stderr)
		require.Equal(t, 2, result.ExitCode)
		handle.AssertExpectations(t)
	})
}

func TestToJSProcessHandleNoOptions(t *testing.T) {
	t.Run("omits streams and rejects stdin writes", func(t *testing.T) {
		harness := loadTestToolIntegration(t, `
			async function inspectProcessHandle(handle) {
				if (handle.stdout !== null) throw new Error("stdout is not null");
				if (handle.stderr !== null) throw new Error("stderr is not null");
				try {
					await handle.writeStdin("a");
				} catch (error) {
					return error.message;
				}
				throw new Error("writeStdin resolved");
			}

			let tool = {
				name: "a",
				version: "1",
				description: { short: "short", long: "long" },
				probe: () => ({ available: true, capabilities: {}, advice: [] }),
				run: () => {},
				reformat: () => {},
				onStop: () => {},
				onCancel: () => {},
			};
		`)
		handle := &tool_mocks.MockProcessHandle{}
		handle.On("Stdout").Return(nil).Once()
		handle.On("Stderr").Return(nil).Once()
		jsHandle := harness.ToJSProcessHandleNoOptions(t, handle)

		result, err := harness.CallAwait[string](t, "inspectProcessHandle", jsHandle)

		require.NoError(t, err)
		require.Equal(t, "stdin is not open for this process", result)
		handle.AssertNotCalled(t, "WriteStdin", mock.Anything)
		handle.AssertExpectations(t)
	})
}
