// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool_goja

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/locality"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	run_mocks "github.com/Arm-Debug/apap-cli/apap-engine/run/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

const ToolSourceBegin = `
let tool = {
	name: "testTool",
	version: "0.1",
	supportsWorkloadLaunch: true,
	deployments: [{
		appliesTo: [
			{architecture: "x86_64", os: "Linux"}
		],
		dependencies: [],
	}],
	description: {
		short: "Short description",
		long: "Long description",
	},
`

const ToolSourceRender = `
		run: (engine, ctx) => {},
`

const ToolSourceEnd = `
		reformat: (engine, ctx) => {},
		onCancel: (engine, ctx) => {},
		onStop: (engine, ctx) => {},
		probe: (engine, ctx) => {return {available: true, capabilities: {}, advice: []}},
	};
`

const ToolSource = ToolSourceBegin + ToolSourceRender + ToolSourceEnd

const parameterisedToolSuffix = `
tool.parameters = [
	{
		id: "mode",
		label: "Mode",
		config: {
			type: "single_select",
			defaultValue: "samples",
			options: ["samples", "metrics"]
		}
	},
	{
		id: "toggle",
		label: "Toggle",
		config: {
			type: "checkbox",
			defaultValue: true
		}
	},
	{
		id: "note",
		label: "Note",
		config: {
			type: "input"
		}
	}
];
`

const ParameterisedToolSource = ToolSource + parameterisedToolSuffix

// LoadFromSourceNoError is a helper for tests that loads a tool source and fails the test if there is an error
func LoadFromSourceNoError(t *testing.T, src string) *ScriptedToolSource {
	sts, err := LoadFromSource(src, "dummy")
	require.NoError(t, err)
	return sts
}

func integrationContext(engine tool.Engine, ic *tool.IntegrationContext) *tool.IntegrationContext {
	if ic == nil {
		ic = &tool.IntegrationContext{}
	}
	ic.Ctx = context.Background()
	ic.DefaultEngineLocality = tool.EngineLocality{Engine: engine, ToolsRoot: "/target/tools"}
	ic.ResolveLocality = func(name string) (tool.EngineLocality, error) {
		if name != locality.Target {
			return tool.EngineLocality{}, errors.New("unsupported locality")
		}
		return tool.EngineLocality{Engine: engine, FileCollector: ic.DefaultEngineLocality.FileCollector, ToolsRoot: "/target/tools"}, nil
	}
	return ic
}

func TestScriptedToolInstance(t *testing.T) {
	t.Run("ToolInstance can use engine version metadata for bundle dependencies", func(t *testing.T) {
		toolSource := `
const bundleVersion = performix.engineVersion;
let tool = {
	name: "testTool",
	version: "0.1",
	supportsWorkloadLaunch: true,
	deployments: [{
		appliesTo: [{ architecture: "x86_64", os: "Linux" }],
		dependencies: [{
			type: "tool_bundle",
			name: "test-bundle",
			version: bundleVersion,
			requiredWhen: { type: "always" },
		}],
	}],
	description: {
		short: "Short description",
		long: "Long description",
	},
	run: (engine, ctx) => {},
	reformat: (engine, ctx) => {},
	onCancel: (engine, ctx) => {},
	onStop: (engine, ctx) => {},
	probe: (engine, ctx) => {return {available: true, capabilities: {}, advice: []}},
};
`
		sts := LoadFromSourceNoError(t, toolSource)
		assert.Equal(t, "0.1", sts.ToolVersion)
		require.Len(t, sts.ToolDeployments, 1)
		require.Len(t, sts.ToolDeployments[0].Dependencies, 1)
		assert.Equal(t, versions.GetVersion(), sts.ToolDeployments[0].Dependencies[0].Version)

		ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
		require.NoError(t, err)
		assert.Equal(t, "0.1", ti.Properties().Version)
		assert.Equal(t, versions.GetVersion(), ti.Properties().Deployments[0].Dependencies[0].Version)
	})

	t.Run("ToolInstance reports properties", func(t *testing.T) {
		sts := LoadFromSourceNoError(t, ToolSource)
		ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
		require.NoError(t, err)

		x86Arch := conductor.Architecture("x86_64")
		linuxOS := conductor.OS("Linux")
		expectedDeployments := []deploymentsupport.DeploymentDeclaration{
			{AppliesTo: []deploymentsupport.PlatformConfigurationFilter{{Architecture: &x86Arch, OS: &linuxOS}},
				Dependencies: []deploymentsupport.Dependency{}},
		}
		assert.Equal(t, ti.Properties(), tool.IntegrationProperties{
			Name:                   "testTool",
			Version:                "0.1",
			SupportsWorkloadLaunch: true,
			ShortDescription:       "Short description",
			LongDescription:        "Long description",
			Deployments:            expectedDeployments,
		})
	})

	t.Run("ToolInstance rejects parameter options function", func(t *testing.T) {
		modeOptionsDecl := `
			const modeOptions = () => ["a", "b"];
		`
		source := `
			parameters: [{
				id: "mode",
				config: {
					type: "single_select",
					defaultValue: "a",
					options: modeOptions,
				},
			}],
		` + ToolSourceRender

		toolSource := modeOptionsDecl + ToolSourceBegin + source + ToolSourceEnd
		mec := &tool_mocks.MockEngineContext{}
		_, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(mec, nil))
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineIntegrationParametersDynamicOptionsUnsupported, msgErr.Code())
		assert.Equal(t, map[string]string{"tool": "testTool", "version": "0.1"}, msgErr.Metadata())
	})

	t.Run("ToolInstance engineContext ExecCommand array ags is bound correctly", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {
			   await engine.execCommand(["/bin/ls", "-lh"], {
					asPrivileged: true,
					affinity: ["0"],
					workingDirectory: "/tmp",
					environment: {Key: "22", Dev: "true"}
				})
			},
		`
		toolSource := ToolSourceBegin + runSource + ToolSourceEnd
		mec := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(mec, nil))
		require.NoError(t, err)

		execOptions := process.LaunchCommand{
			Command:          []string{"/bin/ls", "-lh"},
			AsPrivileged:     true,
			Affinity:         []string{"0"},
			WorkingDirectory: "/tmp",
			Environment:      map[string]string{"Key": "22", "Dev": "true"},
		}
		commandResult := process.CommandResult{Rc: 52, Stdout: "some stdOut", Stderr: "some stdErr"}
		mec.On("ExecCommand", &execOptions).Return(&commandResult, nil)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		require.NoError(t, ti.Run())
		ti.(*GojaToolInstance).asyncHelper.StopLoop()
		mec.AssertExpectations(t)
	})

	t.Run("ToolInstance engineContext ExecCommand is bound correctly", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {
			  cmdRes = await engine.execCommand("/bin/ls", {
					asPrivileged: true,
					affinity: ["0"],
					workingDirectory: "/tmp",
					environment: {Key: "22", Dev: "true"}
				})
				rc = cmdRes.rc
				stdout = cmdRes.stdout
				stderr = cmdRes.stderr
			},
		`
		toolSource := ToolSourceBegin + runSource + ToolSourceEnd
		mec := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(mec, nil))
		require.NoError(t, err)

		execOptions := process.LaunchCommand{
			Command:          []string{"/bin/ls"},
			AsPrivileged:     true,
			Affinity:         []string{"0"},
			WorkingDirectory: "/tmp",
			Environment:      map[string]string{"Key": "22", "Dev": "true"},
		}
		commandResult := process.CommandResult{
			Rc:     52,
			Stdout: "some stdOut",
			Stderr: "some stdErr",
		}
		mec.On("ExecCommand", &execOptions).Return(&commandResult, nil)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		require.NoError(t, ti.Run())
		mec.AssertExpectations(t)
		ti.(*GojaToolInstance).asyncHelper.StopLoop()

		cmdRes := ti.(*GojaToolInstance).asyncHelper.Vm.Get("cmdRes").ToObject(ti.(*GojaToolInstance).asyncHelper.Vm)
		require.NotNil(t, cmdRes)
		assert.Equal(t, int64(52), cmdRes.Get("rc").ToInteger())
		assert.Equal(t, "some stdOut", cmdRes.Get("stdout").String())
		assert.Equal(t, "some stdErr", cmdRes.Get("stderr").String())
	})

	t.Run("ToolInstance run, stop, cancel and reformat can be called concurrently", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {
			  runCalled = true
				await engine.execCommand("/bin/sleep", {asPrivileged: false, affinity: [], workingDirectory: "/tmp", environment: {}})
			},
			onStop: async(engine, ctx) => {
			  stopCalled = true
			},
			onCancel: async (engine, ctx) => {
			  cancelCalled = true
				throw "boom"
			},
			reformat: (engine, ctx) => {
			  reformatCalled = true
			},
			probe: (engine, ctx) => {return {available: true, capabilities: {}, advice: []}},
	};
		`
		execOptions := process.LaunchCommand{
			Command:          []string{"/bin/sleep"},
			AsPrivileged:     false,
			Affinity:         []string{},
			WorkingDirectory: "/tmp",
			Environment:      map[string]string{},
		}
		commandResult := process.CommandResult{
			Rc:     52,
			Stdout: "some stdOut",
			Stderr: "some stdErr",
		}

		wg := sync.WaitGroup{}
		wg.Add(1)

		mec := &tool_mocks.MockEngineContext{}
		mec.
			On("ExecCommand", &execOptions).
			Run(func(args mock.Arguments) {
				wg.Wait()
			}).
			Return(&commandResult, nil)

		toolSource := ToolSourceBegin + runSource
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(mec, nil))
		require.NoError(t, err)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()

		// Start run, it won't complete until we call wg.Done()
		go func() {
			require.NoError(t, ti.Run())
		}()
		vm := ti.(*GojaToolInstance).asyncHelper.Vm

		// Wait for run to be active, poll the VM to achieve this.
		for {
			err = ti.(*GojaToolInstance).asyncHelper.RunOnLoopBlock(func(vm *goja.Runtime) error {
				runCalledState := vm.Get("runCalled")
				if runCalledState != nil && runCalledState.ToBoolean() {
					return nil
				}
				return errors.New("not yet")
			})
			if err == nil {
				break
			}
			time.Sleep(time.Millisecond * 1)
		}
		require.NoError(t, ti.Stop())
		require.ErrorContains(t, ti.Cancel(), "boom")
		wg.Done() // Let the run complete

		require.NoError(t, ti.Reformat())
		ti.(*GojaToolInstance).asyncHelper.StopLoop()

		assert.True(t, vm.Get("runCalled").ToBoolean())
		assert.True(t, vm.Get("stopCalled").ToBoolean())
		assert.True(t, vm.Get("cancelCalled").ToBoolean())
		assert.True(t, vm.Get("reformatCalled").ToBoolean())
	})

	t.Run("ToolInstance probe can be called", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {},
			onStop: async(engine, ctx) => {},
			onCancel: async (engine, ctx) => {},
			reformat: (engine, ctx) => {},
			probe: (engine, ctx) => {return {available: true, capabilities: {bound: "yes"}, advice: [{level: "info", messageCode: "some.CODE"}]}},
	};
		`

		toolSource := ToolSourceBegin + runSource
		engine := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(engine, nil))
		require.NoError(t, err)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		res, err := ti.Probe()
		require.NoError(t, err)
		ti.(*GojaToolInstance).asyncHelper.StopLoop()
		assert.Equal(t, tool.ProbeResult{Available: true, Capabilities: map[string]any{"bound": "yes"}, Advice: []tool.ProbeAdvice{{MessageCode: "some.CODE", Level: "info"}}}, res)
	})

	t.Run("metadata and cause fields in probe response are optionally accepted", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {},
			onStop: async(engine, ctx) => {},
			onCancel: async (engine, ctx) => {},
			reformat: (engine, ctx) => {},
			probe: (engine, ctx) => {return {available: true, capabilities: {bound: "yes"}, advice: [{level: "info", messageCode: "some.CODE", metadata: {myKey: "myValue"}, cause: "a cause"}]}},
	};
		`

		toolSource := ToolSourceBegin + runSource
		engine := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(engine, nil))
		require.NoError(t, err)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		res, err := ti.Probe()
		require.NoError(t, err)
		ti.(*GojaToolInstance).asyncHelper.StopLoop()
		expectedAdvice := tool.ProbeAdvice{MessageCode: "some.CODE", Level: "info", Metadata: map[string]string{"myKey": "myValue"}, Cause: "a cause"}
		assert.Equal(t, tool.ProbeResult{Available: true, Capabilities: map[string]any{"bound": "yes"}, Advice: []tool.ProbeAdvice{expectedAdvice}}, res)
	})

	t.Run("messageCode field in probe response is required", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {},
			onStop: async(engine, ctx) => {},
			onCancel: async (engine, ctx) => {},
			reformat: (engine, ctx) => {},
			probe: (engine, ctx) => {return {available: true, capabilities: {bound: "yes"}, advice: [{level: "info"}]}},
	};
		`

		toolSource := ToolSourceBegin + runSource
		engine := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(engine, nil))
		require.NoError(t, err)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		_, err = ti.Probe()
		require.ErrorContains(t, err, "has unset fields: advice[0].messageCode")
		ti.(*GojaToolInstance).asyncHelper.StopLoop()
	})

	t.Run("ToolInstance engineContext StartProcess is bound correctly", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {
			  processHandle = await engine.startProcess(["/bin/wait", "5"], {
					asPrivileged: true,
					affinity: [],
					workingDirectory: "/tmp",
					environment: {Key: "22", Dev: "true"},
					stdinOpen: true,
					stdout: {redirect: "file", path: "stdout.txt"},
					stderr: {redirect: "file", path: "stderr.txt"}
				})
				pid = processHandle.pid()
				await processHandle.kill()
				await processHandle.interrupt()
				waitRes = await processHandle.wait()
				await processHandle.writeStdin("stdinContents")
			},
		`
		toolSource := ToolSourceBegin + runSource + ToolSourceEnd
		mec := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(mec, nil))
		require.NoError(t, err)

		processOptions := process.StartProcess{
			LaunchCommand: process.LaunchCommand{
				Command:          []string{"/bin/wait", "5"},
				AsPrivileged:     true,
				Affinity:         []string{},
				WorkingDirectory: "/tmp",
				Environment:      map[string]string{"Key": "22", "Dev": "true"},
			},
			Stdin:  process.StdinBuffer,
			Stdout: process.StreamRedirect{Mode: process.File, FilePath: "stdout.txt"},
			Stderr: process.StreamRedirect{Mode: process.File, FilePath: "stderr.txt"},
			Ctx:    context.Background(),
		}

		mph := tool_mocks.MockProcessHandle{}
		mph.On("PID").Return(1234)
		mph.On("Kill").Return(nil)
		mph.On("Interrupt").Return(nil)
		mph.On("Wait").Return(10, nil)
		mph.On("Stdout").Return(bytes.NewReader([]byte("stdoutContents")))
		mph.On("Stderr").Return(bytes.NewReader([]byte("stderrContents")))
		mph.On("WriteStdin", "stdinContents").Return(nil)

		mec.
			On("StartProcess", &processOptions).
			Return(&mph, nil)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		require.NoError(t, ti.Run())
		ti.(*GojaToolInstance).asyncHelper.StopLoop()
		mec.AssertExpectations(t)

		vm := ti.(*GojaToolInstance).asyncHelper.Vm
		assert.Equal(t, int64(1234), vm.Get("pid").ToInteger())

		wait := vm.Get("waitRes").ToObject(vm)
		assert.Equal(t, int64(10), wait.Get("exitCode").ToInteger())

		mph.AssertExpectations(t)
	})

	t.Run("Process streams (stdout/stderr) can be read", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {
			  processHandle = await engine.startProcess("/bin/ls", {
					asPrivileged: true,
					affinity: [],
					workingDirectory: "/tmp",
					environment: {Key: "22", Dev: "true"},
					stdinOpen: false,
					stdout: {redirect: "stream"},
					stderr: {redirect: "stream"}
				})
				stdout = []
				stderr = []
				const p1 = forAwait(processHandle.stdout, (v) => {stdout.push(v)})
				const p2 = forAwait(processHandle.stderr, (v) => {stderr.push(v)})
				await Promise.all([p1, p2])
			},
		`
		toolSource := ToolSourceBegin + runSource + ToolSourceEnd
		mec := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(mec, nil))
		require.NoError(t, err)

		processOptions := process.StartProcess{
			LaunchCommand: process.LaunchCommand{
				Command:          []string{"/bin/ls"},
				AsPrivileged:     true,
				Affinity:         []string{},
				WorkingDirectory: "/tmp",
				Environment:      map[string]string{"Key": "22", "Dev": "true"},
			},
			Stdout: process.StreamRedirect{Mode: process.Stream},
			Stderr: process.StreamRedirect{Mode: process.Stream},
			Ctx:    context.Background(),
		}

		mph := tool_mocks.MockProcessHandle{}
		mph.On("Stdout").Return(io.MultiReader(bytes.NewReader([]byte("A")), bytes.NewReader([]byte("B")), bytes.NewReader([]byte("C"))))
		mph.On("Stderr").Return(io.MultiReader(bytes.NewReader([]byte("D")), bytes.NewReader([]byte("E")), bytes.NewReader([]byte("F"))))

		mec.On("StartProcess", &processOptions).Return(&mph, nil)

		asyncH := &ti.(*GojaToolInstance).asyncHelper
		asyncH.StartLoop()
		require.NoError(t, ti.Run())
		asyncH.StopLoop()
		mec.AssertExpectations(t)

		vm := asyncH.Vm
		assert.NotNil(t, vm.Get("stdout"))
		assert.NotNil(t, vm.Get("stderr"))
		assert.Equal(t, []interface{}{"A", "B", "C"}, vm.Get("stdout").Export())
		assert.Equal(t, []interface{}{"D", "E", "F"}, vm.Get("stderr").Export())

		mph.AssertExpectations(t)
	})

	t.Run("ToolInstance engineContext createTempDir is bound correctly", func(t *testing.T) {
		runWithCreateTempDir := `
			run: async (engine, ctx) => {
				generatedDir = await engine.createTempDir()
			},
		`
		toolSource := ToolSourceBegin + runWithCreateTempDir + ToolSourceEnd
		mec := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(mec, nil))
		require.NoError(t, err)

		mec.On("CreateTempDir").Return("/tmp/tt", nil)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()
		mec.AssertExpectations(t)

		createdDir := ti.(*GojaToolInstance).asyncHelper.Vm.Get("generatedDir").String()
		assert.Equal(t, "/tmp/tt", createdDir)
	})

	t.Run("ToolInstance can switch between host locality and target locality", func(t *testing.T) {
		toolSource := ToolSourceBegin + `
				run: async (engine, ctx) => {
				const hostEngine = engine.withLocality("host");
				hostToolsRoot = hostEngine.toolsRoot();
				hostLocality = hostEngine.getLocality();
				await hostEngine.execCommand(["/bin/true"], {});

				const targetEngine = hostEngine.withLocality("target");
				targetToolsRoot = targetEngine.toolsRoot();
				targetLocality = targetEngine.getLocality();
				await targetEngine.execCommand(["/bin/echo", "target"], {});
			},
		` + ToolSourceEnd

		targetEngine := &tool_mocks.MockEngineContext{}
		hostEngine := &tool_mocks.MockEngineContext{}
		hostEngine.On("ExecCommand", &process.LaunchCommand{
			Command: []string{"/bin/true"},
		}).Return(&process.CommandResult{Rc: 0}, nil).Once()
		targetEngine.On("ExecCommand", &process.LaunchCommand{
			Command: []string{"/bin/echo", "target"},
		}).Return(&process.CommandResult{Rc: 0}, nil).Once()

		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(&tool.IntegrationContext{
			Ctx:                   context.Background(),
			DefaultEngineLocality: tool.EngineLocality{Name: "target", Engine: targetEngine},
			ResolveLocality: func(name string) (tool.EngineLocality, error) {
				switch name {
				case "target":
					return tool.EngineLocality{Name: "target", Engine: targetEngine, ToolsRoot: "/target/tools"}, nil
				case "host":
					return tool.EngineLocality{Name: "host", Engine: hostEngine, ToolsRoot: "/host/tools"}, nil
				default:
					return tool.EngineLocality{}, errors.New("unsupported locality")
				}
			},
		})
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper
		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		assert.Equal(t, "/host/tools", ti.(*GojaToolInstance).asyncHelper.Vm.Get("hostToolsRoot").String())
		assert.Equal(t, "/target/tools", ti.(*GojaToolInstance).asyncHelper.Vm.Get("targetToolsRoot").String())
		assert.Equal(t, "host", ti.(*GojaToolInstance).asyncHelper.Vm.Get("hostLocality").String())
		assert.Equal(t, "target", ti.(*GojaToolInstance).asyncHelper.Vm.Get("targetLocality").String())
		targetEngine.AssertExpectations(t)
		hostEngine.AssertExpectations(t)
	})

	t.Run("ToolInstance returns error when locality resolution fails", func(t *testing.T) {
		toolSource := ToolSourceBegin + `
			run: async (engine, ctx) => {
				engine.withLocality("host");
			},
		` + ToolSourceEnd

		engine := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(engine, nil))
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper
		ah.StartLoop()
		err = ti.Run()
		ah.StopLoop()

		require.Error(t, err)
		require.ErrorContains(t, err, "unsupported locality")
	})

	t.Run("ToolInstance keeps ctx.toolsRoot aligned with the default engine locality", func(t *testing.T) {
		toolSource := ToolSourceBegin + `
			run: async (engine, ctx) => {
				legacyToolsRoot = ctx.toolsRoot;
				defaultToolsRoot = engine.toolsRoot();
			},
		` + ToolSourceEnd

		engine := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(engine, nil))
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper
		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		assert.Equal(t, "/target/tools", ah.Vm.Get("legacyToolsRoot").String())
		assert.Equal(t, "/target/tools", ah.Vm.Get("defaultToolsRoot").String())
	})

	t.Run("ToolInstance engineContext exposes isFullCaptureSupportEnabled", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {
				fullCaptureSupportEnabled = engine.isFullCaptureSupportEnabled();
			},
	`
		toolSource := ToolSourceBegin + runSource + ToolSourceEnd
		mec := &tool_mocks.MockEngineContext{}
		ic := integrationContext(mec, nil)
		ic.IsFullCaptureSupportEnabled = true
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper
		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		val := ti.(*GojaToolInstance).asyncHelper.Vm.Get("fullCaptureSupportEnabled").ToBoolean()
		assert.True(t, val)
	})

	t.Run("ToolInstance engineContext methods are bound correctly", func(t *testing.T) {
		const prefix, suffix = `
			run: async (engine, ctx) => {
		`, `
			},
		`
		type MEC = tool_mocks.MockEngineContext

		tests := []struct {
			name string
			body string
			mock func(*MEC)
		}{
			{"mkDir", `await engine.mkDir("targetDir")`,
				func(m *MEC) { m.On("Mkdir", "targetDir").Return(nil) }},
			{"rm", `await engine.rm("some/path", true, false)`,
				func(m *MEC) { m.On("Rm", "some/path", true, false).Return(nil) }},
			{"makeWritable", `await engine.makeWritable("some/file", true)`,
				func(m *MEC) { m.On("MakeWritable", "some/file", true).Return(nil) }},
			{"chown", `await engine.chown("target/file", "newOwner", false)`,
				func(m *MEC) { m.On("Chown", "target/file", "newOwner", false).Return(nil) }},
			{"log", `engine.log("info", "hello world")`,
				func(m *MEC) { m.On("Log", "info", "hello world").Return() }},
			{"writeUserMessage", `engine.writeUserMessage("warn", "action happened")`,
				func(m *MEC) { m.On("WriteUserMessage", "warn", "action happened").Return() }},
			{"startProgressTracker", `engine.startProgressTracker("build-1")`,
				func(m *MEC) { m.On("StartProgressTracker", "build-1").Return(nil) }},
			{"updateProgress", `engine.updateProgress("build-1", "Compiling...", 45.5)`,
				func(m *MEC) { m.On("UpdateProgress", "build-1", "Compiling...", 45.5).Return(nil) }},
			{"endProgress", `engine.endProgress("build-1")`,
				func(m *MEC) { m.On("EndProgress", "build-1").Return(nil) }},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				src := ToolSourceBegin + prefix + tt.body + suffix + ToolSourceEnd
				m := &MEC{}
				ti, err := LoadFromSourceNoError(t, src).NewIntegration(integrationContext(m, nil))
				require.NoError(t, err)

				tt.mock(m)

				ah := &ti.(*GojaToolInstance).asyncHelper

				ah.StartLoop()
				require.NoError(t, ti.Run())
				ah.StopLoop()

				m.AssertExpectations(t)
			})
		}
	})

	t.Run("ToolInstance emitOutput is bound correctly", func(t *testing.T) {
		const suffix = `
			run: async (engine, ctx) => {
				engine.emitOutput("out/file", "relPath", {name: "log", version: "0.1"}, {exclude: ["a/b", "c/d"], backgroundTransfer: true})
			},
		`
		transferOptions := tool.TransferOptions{
			ImmediateRetrieval: false,
			Exclude:            []string{"a/b", "c/d"},
			BackgroundTransfer: true,
		}
		mockFileCollector := &tool_mocks.MockFileCollector{}
		mockFileCollector.On("QueueFileRetrieval", "", "out/file", "relPath", cdf.ComponentType{Name: "log", SchemaVersion: "0.1"}, transferOptions).Return(nil)

		src := ToolSourceBegin + suffix + ToolSourceEnd
		m := &tool_mocks.MockEngineContext{}
		mrw := &run_mocks.MockRunWriter{}
		mrw.On("WriteManifest", mock.Anything).Return(nil)
		mrw.On("WriteEntityDirs", mock.Anything).Return(nil)
		ic := integrationContext(m, nil)
		ic.DefaultEngineLocality.FileCollector = mockFileCollector
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		m.AssertExpectations(t)
	})

	t.Run("ToolInstance emitOutput does not require optional config fields", func(t *testing.T) {
		const suffix = `
			run: async (engine, ctx) => {
				engine.emitOutput("out/file", "relPath", {name: "log", version: "0.1"})
			},
		`
		mockFileCollector := &tool_mocks.MockFileCollector{}
		mockFileCollector.On("QueueFileRetrieval", "", "out/file", "relPath", cdf.ComponentType{Name: "log", SchemaVersion: "0.1"}, tool.TransferOptions{}).Return(nil)

		src := ToolSourceBegin + suffix + ToolSourceEnd
		m := &tool_mocks.MockEngineContext{}
		mrw := &run_mocks.MockRunWriter{}
		mrw.On("WriteManifest", mock.Anything).Return(nil)
		mrw.On("WriteEntityDirs", mock.Anything).Return(nil)
		ic := integrationContext(m, nil)
		ic.DefaultEngineLocality.FileCollector = mockFileCollector
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		m.AssertExpectations(t)
	})

	t.Run("ToolInstance emitOutput uses locality-specific file collectors", func(t *testing.T) {
		const suffix = `
			run: async (engine, ctx) => {
				engine.emitOutput("target/file", "targetRelPath", {name: "log", version: "0.1"});
				const hostEngine = engine.withLocality("host");
				hostEngine.emitOutput("host/file", "hostRelPath", {name: "log", version: "0.1"});
			},
		`

		targetCollector := &tool_mocks.MockFileCollector{}
		targetCollector.
			On("QueueFileRetrieval", "", "target/file", "targetRelPath", cdf.ComponentType{Name: "log", SchemaVersion: "0.1"}, tool.TransferOptions{}).
			Return(nil).
			Once()
		hostCollector := &tool_mocks.MockFileCollector{}
		hostCollector.
			On("QueueFileRetrieval", "", "host/file", "hostRelPath", cdf.ComponentType{Name: "log", SchemaVersion: "0.1"}, tool.TransferOptions{}).
			Return(nil).
			Once()

		src := ToolSourceBegin + suffix + ToolSourceEnd
		targetEngine := &tool_mocks.MockEngineContext{}
		hostEngine := &tool_mocks.MockEngineContext{}
		ic := &tool.IntegrationContext{
			Ctx: context.Background(),
			DefaultEngineLocality: tool.EngineLocality{
				Engine:        targetEngine,
				FileCollector: targetCollector,
				ToolsRoot:     "/target/tools",
			},
			ResolveLocality: func(name string) (tool.EngineLocality, error) {
				switch name {
				case "target":
					return tool.EngineLocality{Engine: targetEngine, FileCollector: targetCollector, ToolsRoot: "/target/tools"}, nil
				case "host":
					return tool.EngineLocality{Engine: hostEngine, FileCollector: hostCollector, ToolsRoot: "/host/tools"}, nil
				default:
					return tool.EngineLocality{}, errors.New("unsupported locality")
				}
			},
		}
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper
		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		targetCollector.AssertExpectations(t)
		hostCollector.AssertExpectations(t)
	})

	t.Run("ToolInstance createRunFile streams data to host file. readHostFile reads it back.", func(t *testing.T) {
		tempDir := t.TempDir()
		hostFile := filepath.Join(tempDir, "capture_log_err.txt")

		const suffix = `
		run: async (engine, ctx) => {
			let handle = await engine.createRunFile("capture_log_err.txt", {name: "log-text", version: "1.0"})
			await handle.append("stderr ")
			await handle.append("data")
			await handle.close()
			readData = await engine.readHostFile(handle.path)
		},
	`

		mockFileCollector := &tool_mocks.MockFileCollector{}
		mockFileCollector.On("AddComponent", "", cdf.ComponentType{Name: "log-text", SchemaVersion: "1.0"}, "capture_log_err.txt").Return(hostFile, nil)

		type MEC = tool_mocks.MockEngineContext
		src := ToolSourceBegin + suffix + ToolSourceEnd
		m := &MEC{}
		hostFileHandle := &tool_mocks.MockFileHandle{PathValue: hostFile}

		closed := false
		appended := []string{}

		hostFileHandle.On("Append", "stderr ").Run(func(args mock.Arguments) {
			require.False(t, closed)
			appended = append(appended, args.String(0))
		}).Return(nil)
		hostFileHandle.On("Append", "data").Run(func(args mock.Arguments) {
			require.False(t, closed)
			appended = append(appended, args.String(0))
		}).Return(nil)
		hostFileHandle.On("Close").Run(func(mock.Arguments) {
			closed = true
		}).Return(nil)

		m.On("CreateRunFile", hostFile).Return(hostFileHandle, nil)
		m.On("ReadHostFile", hostFile).Return("stderr data", nil)
		ic := integrationContext(m, nil)
		ic.DefaultEngineLocality.FileCollector = mockFileCollector
		ic.WorkingDir = tempDir
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		m.AssertExpectations(t)
		hostFileHandle.AssertExpectations(t)
		require.True(t, closed)
		assert.Equal(t, []string{"stderr ", "data"}, appended)

		assert.Equal(t, "stderr data", ah.Vm.Get("readData").Export())
	})

	t.Run("ToolInstance createRunFile fails when AddComponent fails", func(t *testing.T) {
		const suffix = `
		run: async (engine, ctx) => {
			createRunFileSucceeded = false
			createRunFileError = undefined
			await engine.createRunFile("capture_log_err.txt", {name: "log-text", version: "1.0"})
			createRunFileSucceeded = true
		},
	`

		mockFileCollector := &tool_mocks.MockFileCollector{}
		mockFileCollector.On("AddComponent", "", cdf.ComponentType{Name: "log-text", SchemaVersion: "1.0"}, "capture_log_err.txt").
			Return("", errors.New("add component failed"))

		type MEC = tool_mocks.MockEngineContext
		src := ToolSourceBegin + suffix + ToolSourceEnd
		m := &MEC{}
		ic := integrationContext(m, nil)
		ic.DefaultEngineLocality.FileCollector = mockFileCollector
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.Error(t, ti.Run())
		require.ErrorContains(t, ti.Run(), "add component failed")
		ah.StopLoop()

		m.AssertExpectations(t)

		createSucceeded := ah.Vm.Get("createRunFileSucceeded").Export()
		assert.Equal(t, false, createSucceeded)
	})

	t.Run("ToolInstance readHostFile fails when file is missing", func(t *testing.T) {
		const suffix = `
		run: async (engine, ctx) => {
			readHostFileSucceeded = false
			await engine.readHostFile("/no/file/here/missing.txt")
			readHostFileSucceeded = true
		},
	`

		type MEC = tool_mocks.MockEngineContext
		src := ToolSourceBegin + suffix + ToolSourceEnd
		m := &MEC{}
		m.On("ReadHostFile", "/no/file/here/missing.txt").Return("", errors.New("error reading host file"))
		ic := integrationContext(m, nil)
		ic.DefaultEngineLocality.FileCollector = &tool_mocks.MockFileCollector{}
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.Error(t, ti.Run())
		require.ErrorContains(t, ti.Run(), "error reading host file")
		ah.StopLoop()

		m.AssertExpectations(t)

		readSucceeded := ah.Vm.Get("readHostFileSucceeded").Export()
		assert.Equal(t, false, readSucceeded)
	})

	t.Run("ToolInstance toolContext is bound correctly", func(t *testing.T) {
		const prefix, suffix = `
			parameters: [{
				id: "paramA",
				label: "Param A",
				config: {
					type: "input",
				},
			}],
			run: (engine, ctx) => {
		`, `
			},
		`

		tcd := tool.IntegrationContext{
			Params:     map[string]any{"paramA": "1234"},
			Workload:   &tool.WorkloadAttach{PID: 23},
			WorkingDir: "/tmp/workdir",
			Env:        map[string]string{"ENV_VAR": "value"},
			Timeout:    60,
		}

		tests := []struct {
			name    string
			body    string
			options func(ic *tool.IntegrationContext)
			test    func(*goja.Runtime)
		}{
			{"params", `a = ctx.params["paramA"]`, func(ic *tool.IntegrationContext) {},
				func(vm *goja.Runtime) { assert.Equal(t, "1234", vm.Get("a").String()) }},
			{"workloadAttach", `a = ctx.workload`, func(ic *tool.IntegrationContext) { ic.Workload = &tool.WorkloadAttach{PID: 23} },
				func(vm *goja.Runtime) {
					assert.Equal(t, map[string]interface{}{"pid": int64(23), "type": "attach"}, vm.Get("a").Export())
				}},
			{"workloadLaunch", `a = ctx.workload`, func(ic *tool.IntegrationContext) {
				ic.Workload = &tool.WorkloadLaunch{RawCommand: "ls", Command: []string{"ls"}, Environment: map[string]string{"FOO": "bar"}, WorkingDir: "/some/dir", UseShell: true}
			},
				func(vm *goja.Runtime) {
					assert.Equal(t, map[string]interface{}{"rawCommand": "ls", "command": []string{"ls"}, "environment": map[string]string{"FOO": "bar"}, "type": "launch", "workingDir": "/some/dir", "useShell": true}, vm.Get("a").Export())
				}},
			{"workloadAndroidLaunch", `a = ctx.workload`, func(ic *tool.IntegrationContext) {
				ic.Workload = &tool.WorkloadAndroidLaunch{PackageName: "com.example.app", ActivityName: ".MainActivity"}
			},
				func(vm *goja.Runtime) {
					assert.Equal(t, map[string]interface{}{"packageName": "com.example.app", "activityName": ".MainActivity", "type": "androidLaunch"}, vm.Get("a").Export())
				}},
			{"workloadSystemwide", `a = ctx.workload`, func(ic *tool.IntegrationContext) { ic.Workload = &tool.WorkloadSystemWide{} },
				func(vm *goja.Runtime) {
					assert.Equal(t, map[string]interface{}{"type": "systemWide"}, vm.Get("a").Export())
				}},
			{"workingDir", `a = ctx.workingDir`, func(ic *tool.IntegrationContext) {},
				func(vm *goja.Runtime) { assert.Equal(t, "/tmp/workdir", vm.Get("a").String()) }},
			{"env", `a = ctx.env["ENV_VAR"]`, func(ic *tool.IntegrationContext) {},
				func(vm *goja.Runtime) { assert.Equal(t, "value", vm.Get("a").String()) }},
			{"timeout", `a = ctx.timeout`, func(ic *tool.IntegrationContext) {},
				func(vm *goja.Runtime) { assert.Equal(t, int64(60), vm.Get("a").ToInteger()) }},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				src := ToolSourceBegin + prefix + tt.body + suffix + ToolSourceEnd
				ic := tcd
				tt.options(&ic)
				ti, err := LoadFromSourceNoError(t, src).NewIntegration(integrationContext(&tool.AgentEngine{}, &ic))
				require.NoError(t, err)
				ah := &ti.(*GojaToolInstance).asyncHelper
				ah.StartLoop()
				require.NoError(t, ti.Run())
				ah.StopLoop()
				tt.test(ti.(*GojaToolInstance).asyncHelper.Vm)
			})
		}
	})

	t.Run("ToolInstance binds parameters with defaults", func(t *testing.T) {
		sts := LoadFromSourceNoError(t, ParameterisedToolSource)
		ic := integrationContext(&tool.AgentEngine{}, &tool.IntegrationContext{
			Params: map[string]any{
				"mode": "metrics",
				"note": "provided",
			},
		})
		ti, err := sts.NewIntegration(ic)
		require.NoError(t, err)
		inst := ti.(*GojaToolInstance)
		collapsed := inst.boundParameters.CollapseToMap()
		assert.Equal(t, "metrics", collapsed["mode"])
		assert.Equal(t, true, collapsed["toggle"])
		assert.Equal(t, "provided", collapsed["note"])
	})

	t.Run("ToolInstance parameter validation rejects invalid input", func(t *testing.T) {
		sts := LoadFromSourceNoError(t, ParameterisedToolSource)
		ic := integrationContext(&tool.AgentEngine{}, &tool.IntegrationContext{
			Params: map[string]any{"mode": 42},
		})
		_, err := sts.NewIntegration(ic)
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineIntegrationParametersInvalidSingleSelectValue, msgErr.Code())
	})

	t.Run("ToolInstance parameter validation rejects unknown parameter", func(t *testing.T) {
		sts := LoadFromSourceNoError(t, ParameterisedToolSource)
		ic := integrationContext(&tool.AgentEngine{}, &tool.IntegrationContext{
			Params: map[string]any{"unexpected": "value"},
		})
		_, err := sts.NewIntegration(ic)
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineIntegrationParametersInvalidParam, msgErr.Code())
		assert.Equal(t, "testTool", msgErr.Metadata()["source"])
	})

	t.Run("ToolInstance parameter validation rejects unknown parameter when no parameters declared", func(t *testing.T) {
		sts := LoadFromSourceNoError(t, ToolSource)
		ic := integrationContext(&tool.AgentEngine{}, &tool.IntegrationContext{
			Params: map[string]any{"foo": "bar"},
		})
		_, err := sts.NewIntegration(ic)
		require.Error(t, err)
		var msgErr *message.MessageImpl
		require.ErrorAs(t, err, &msgErr)
		assert.Equal(t, message.EngineIntegrationParametersInvalidParam, msgErr.Code())
		assert.Equal(t, map[string]string{"paramName": "foo", "source": "testTool"}, msgErr.Metadata())
	})

	t.Run("Tool JS structured error preserves stack", func(t *testing.T) {
		// Stack is preserved if we throw a JS Error object, but not if we throw a plain object
		runSource := `
			run: async (engine, ctx) => {
				let err = new Error()
				err.code = "common.UNSUPPORTED_TARGET_TYPE"
				err.cause = "My error cause"
				err.metadata = { info: "hello world" }
				throw err
			},
			onStop: async(engine, ctx) => {},
			onCancel: async (engine, ctx) => {},
			reformat: (engine, ctx) => {},
			probe: (engine, ctx) => {return {available: true, capabilities: {bound: "yes"}, advice: [{level: "info", message: "fantastic result!"}]}},
		`

		toolSource := ToolSourceBegin + runSource + ToolSourceEnd
		engine := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(engine, nil))
		require.NoError(t, err)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		err = ti.Run()
		assert.ErrorContains(t, err, "common.UNSUPPORTED_TARGET_TYPE: My error cause; dummy:run:")

		errMessage, ok := err.(*message.MessageImpl)
		assert.True(t, ok)
		assert.Equal(t, errMessage.Code(), message.CommonUnsupportedTargetType)

		// Check that the metadata contains the stage name
		metadata := errMessage.Metadata()
		val, ok := metadata["info"]
		assert.True(t, ok)
		assert.Equal(t, val, "hello world")

		ti.(*GojaToolInstance).asyncHelper.StopLoop()
	})
	t.Run("Tool JS generic error is wrapped in a structured message", func(t *testing.T) {
		runSource := `
			run: async (engine, ctx) => {
				throw Error("Something bad happened.")
			},
			onStop: async(engine, ctx) => {},
			onCancel: async (engine, ctx) => {},
			reformat: (engine, ctx) => {},
			probe: (engine, ctx) => {return {available: true, capabilities: {bound: "yes"}, advice: [{level: "info", message: "fantastic result!"}]}},
		`

		toolSource := ToolSourceBegin + runSource + ToolSourceEnd
		engine := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, toolSource).NewIntegration(integrationContext(engine, nil))
		require.NoError(t, err)

		ti.(*GojaToolInstance).asyncHelper.StartLoop()
		err = ti.Run()
		assert.ErrorContains(t, err, "engine.recipe.stages.SCRIPTED_STAGE_ERROR: Something bad happened.; dummy:run:")

		errMessage, ok := err.(*message.MessageImpl)
		assert.True(t, ok)
		assert.Equal(t, errMessage.Code(), message.EngineRecipeStagesScriptedStageError)

		// Check that the metadata contains the stage name
		metadata := errMessage.Metadata()
		val, ok := metadata["stage"]
		assert.True(t, ok)
		assert.Equal(t, val, "Run")

		ti.(*GojaToolInstance).asyncHelper.StopLoop()
	})
	t.Run("ToolInstance isNeoprofTimelineEnabled is bound correctly", func(t *testing.T) {
		const suffix = `
			run: async (engine, ctx) => {
				enabled = engine.isNeoprofTimelineEnabled()
			},
		`

		src := ToolSourceBegin + suffix + ToolSourceEnd
		m := &tool_mocks.MockEngineContext{}
		ic := integrationContext(m, nil)
		ic.IsNeoprofTimelineEnabled = true
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(ic)
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()
		assert.Equal(t, true, ah.Vm.Get("enabled").Export())

		m.AssertExpectations(t)
	})

	t.Run("ToolInstance copyFrom forwards source and destination localities", func(t *testing.T) {
		const suffix = `
			run: async (engine, ctx) => {
				await engine.withLocality("host").copyFrom("target", "/remote/file", "/local/file")
			},
		`
		var sourceLocality string
		var destinationLocality string
		var sourcePath string
		var destinationPath string

		src := ToolSourceBegin + suffix + ToolSourceEnd
		m := &tool_mocks.MockEngineContext{}
		ti, err := LoadFromSourceNoError(t, src).NewIntegration(&tool.IntegrationContext{
			Ctx:                   context.Background(),
			DefaultEngineLocality: tool.EngineLocality{Engine: m, ToolsRoot: "/target/tools"},
			ResolveLocality: func(name string) (tool.EngineLocality, error) {
				switch name {
				case "target":
					return tool.EngineLocality{Engine: m, ToolsRoot: "/target/tools"}, nil
				case "host":
					return tool.EngineLocality{
						Engine:    m,
						ToolsRoot: "/host/tools",
						CopyFrom: func(source string, sourceFile string, destinationFile string) error {
							sourceLocality = source
							destinationLocality = name
							sourcePath = sourceFile
							destinationPath = destinationFile
							return nil
						},
					}, nil
				default:
					return tool.EngineLocality{}, errors.New("unsupported locality")
				}
			},
		})
		require.NoError(t, err)

		ah := &ti.(*GojaToolInstance).asyncHelper

		ah.StartLoop()
		require.NoError(t, ti.Run())
		ah.StopLoop()

		assert.Equal(t, "target", sourceLocality)
		assert.Equal(t, "host", destinationLocality)
		assert.Equal(t, "/remote/file", sourcePath)
		assert.Equal(t, "/local/file", destinationPath)
	})
}

func TestLoadFromSourceSupportsRequire(t *testing.T) {
	dir := t.TempDir()
	helperPath := filepath.Join(dir, "utils.js")
	helperSource := `module.exports = {
	buildDescriptions: function() {
		return { short: "Helper short", long: "Helper long" };
	}
};`
	require.NoError(t, os.WriteFile(helperPath, []byte(helperSource), 0o600))

	toolSource := `const helper = require("./utils");
let tool = {
	name: "require-tool",
	version: "0.1.0",
	supportsWorkloadLaunch: false,
	deployments: [{
		appliesTo: [
			{architecture: "x86_64", os: "Linux"}
		],
		dependencies: [],
	}],
	description: helper.buildDescriptions(),
	probe: (engine, ctx) => ({available: true, capabilities: {}, advice: []}),
	run: (engine, ctx) => {},
	reformat: (engine, ctx) => {},
	onCancel: (engine, ctx) => {},
	onStop: (engine, ctx) => {},
};`

	toolPath := filepath.Join(dir, "require-tool.js")
	sts, err := LoadFromSource(toolSource, toolPath)
	require.NoError(t, err)

	mec := &tool_mocks.MockEngineContext{}
	ti, err := sts.NewIntegration(integrationContext(mec, nil))
	require.NoError(t, err)

	props := ti.Properties()
	assert.Equal(t, "Helper short", props.ShortDescription)
	assert.Equal(t, "Helper long", props.LongDescription)
}

func TestLoadFromSourceSupportsRequireViaSymlink(t *testing.T) {
	baseDir := t.TempDir()
	realDir := filepath.Join(baseDir, "real")
	linkDir := filepath.Join(baseDir, "links")
	require.NoError(t, os.Mkdir(realDir, 0o700))
	require.NoError(t, os.Mkdir(linkDir, 0o700))

	helperPath := filepath.Join(realDir, "utils.js")
	helperSource := `module.exports = {
	buildDescriptions: function() {
		return { short: "Symlink short", long: "Symlink long" };
	}
};`
	require.NoError(t, os.WriteFile(helperPath, []byte(helperSource), 0o600))

	toolSource := `const helper = require("./utils");
let tool = {
	name: "require-tool",
	version: "0.1.0",
	supportsWorkloadLaunch: false,
	deployments: [{
		appliesTo: [
			{architecture: "x86_64", os: "Linux"}
		],
		dependencies: [],
	}],
	description: helper.buildDescriptions(),
	probe: (engine, ctx) => ({available: true, capabilities: {}, advice: []}),
	run: (engine, ctx) => {},
	reformat: (engine, ctx) => {},
	onCancel: (engine, ctx) => {},
	onStop: (engine, ctx) => {},
};`

	toolPath := filepath.Join(realDir, "require-tool.js")
	toolLinkPath := filepath.Join(linkDir, "require-tool.js")
	require.NoError(t, os.WriteFile(toolPath, []byte(toolSource), 0o600))
	require.NoError(t, os.Symlink(toolPath, toolLinkPath))

	sts, err := LoadFromSource(toolSource, toolLinkPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool.AgentEngine{}, nil))
	require.NoError(t, err)

	props := ti.Properties()
	assert.Equal(t, "Symlink short", props.ShortDescription)
	assert.Equal(t, "Symlink long", props.LongDescription)
}

func TestLoadSysutilTimelineIntegrationUsesEngineVersionedBundle(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "sysutil-timeline.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)
	assert.Equal(t, "sysutil-timeline", sts.ToolName)
	assert.Equal(t, "1.0.0", sts.ToolVersion)

	require.NotEmpty(t, sts.ToolDeployments)
	for _, deployment := range sts.ToolDeployments {
		require.Len(t, deployment.Dependencies, 1)
		dep := deployment.Dependencies[0]
		assert.Equal(t, deploymentsupport.DependencyTypeToolBundle, dep.Type)
		assert.Equal(t, "sysutil-timeline", dep.Name)
		assert.Equal(t, versions.GetVersion(), dep.Version)
	}
}

func TestNeoprofAndroidWorkloadUsesSeparateSlRecordArguments(t *testing.T) {
	toolPath := filepath.Clean(filepath.Join("..", "..", "..", "apap-cli", "tool-integrations", "neoprof.js"))
	data, err := os.ReadFile(toolPath)
	require.NoError(t, err)

	sts, err := LoadFromSource(string(data), toolPath)
	require.NoError(t, err)

	ti, err := sts.NewIntegration(integrationContext(&tool_mocks.MockEngineContext{}, nil))
	require.NoError(t, err)

	vm := ti.(*GojaToolInstance).asyncHelper.Vm
	workloadToSLArgs, ok := goja.AssertFunction(vm.Get("workloadToSLArgs"))
	require.True(t, ok)

	workload := vm.NewObject()
	require.NoError(t, workload.Set("type", "androidLaunch"))
	require.NoError(t, workload.Set("packageName", "com.example.app"))
	require.NoError(t, workload.Set("activityName", ".MainActivity"))

	args, err := workloadToSLArgs(goja.Undefined(), workload)
	require.NoError(t, err)
	assert.Equal(t, []any{
		"--android-pkg",
		"com.example.app",
		"--android-activity",
		".MainActivity",
	}, args.Export())
}

func TestLoadFromSourceParsesDependencyLocality(t *testing.T) {
	toolSource := `
let tool = {
	name: "testTool",
	version: "0.1",
	supportsWorkloadLaunch: true,
	deployments: [{
		appliesTo: [{ architecture: "x86_64", os: "Linux" }],
		dependencies: [{
			type: "tool_bundle",
			name: "test-bundle",
			version: "1.0",
			locality: "host",
			requiredWhen: { type: "always" },
		}],
	}],
	description: {
		short: "Short description",
		long: "Long description",
	},
	run: (engine, ctx) => {},
	reformat: (engine, ctx) => {},
	onCancel: (engine, ctx) => {},
	onStop: (engine, ctx) => {},
	probe: (engine, ctx) => {return {available: true, capabilities: {}, advice: []}},
};
`

	sts, err := LoadFromSource(toolSource, "dummy")
	require.NoError(t, err)
	require.Len(t, sts.ToolDeployments, 1)
	require.Len(t, sts.ToolDeployments[0].Dependencies, 1)
	assert.Equal(t, deploymentsupport.DeploymentLocalityHost, sts.ToolDeployments[0].Dependencies[0].Locality)
}

func TestLoadFromSource(t *testing.T) {
	t.Run("unset version produces error", func(t *testing.T) {
		toolSource := `
		tool = {
		  Name: "myTool",
			V: "1.0",
		}
		`
		_, err := LoadFromSource(toolSource, "dummy")
		require.ErrorContains(t, err, "unset")
		require.ErrorContains(t, err, "Version")
	})

	t.Run("invalid js produces error", func(t *testing.T) {
		toolSource := `
		tool = {{
		  Name: "myTool",
			V: "1.0",
		}
		`
		_, err := LoadFromSource(toolSource, "dummy")
		require.ErrorContains(t, err, "compiling tool integration source")
	})

	t.Run("missing tool produces error", func(t *testing.T) {
		toolSource := `
		too = {
		  Name: "myTool",
			V: "1.0",
		}
		`
		_, err := LoadFromSource(toolSource, "dummy")
		require.ErrorContains(t, err, "tool integration must define a global 'tool' object")
	})

	t.Run("valid tool reports correct name and version", func(t *testing.T) {
		sts, err := LoadFromSource(ToolSource, "dummy")
		require.NoError(t, err)
		assert.Equal(t, "testTool", sts.ToolName)
		assert.Equal(t, "0.1", sts.ToolVersion)
	})
	t.Run("tool is parsed with no errors when deployments are missing (implies always supported)", func(t *testing.T) {
		var testTool = `
			let tool = {
				name: "testTool",
				version: "0.1",
				supportsWorkloadLaunch: true,
				// parameters: [{
				// 	name: "sampling_mode",
				// 	type: "enum",
				// 	choices: ["periodic", "event_based"],
				// 	default: "periodic",
				// 	description: "desc",
				// 	optional: true,
				// }],
				description: {
					short: "Short description",
					long: "Long description",
				},
			`
		sts, err := LoadFromSource(testTool+ToolSourceRender+ToolSourceEnd, "dummy")
		require.NoError(t, err)
		assert.Equal(t, "testTool", sts.ToolName)
		assert.Equal(t, "0.1", sts.ToolVersion)
	})
}
