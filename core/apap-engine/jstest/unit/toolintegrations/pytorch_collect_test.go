// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/deploymentsupport"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	tool_mocks "github.com/Arm-Debug/apap-cli/apap-engine/tool/mocks"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-version/versions"
)

const parsePythonScript = "import ast, pathlib, sys; ast.parse(pathlib.Path(sys.argv[1]).read_bytes(), filename=sys.argv[1])"
const readPythonPathScript = "import os, sys; sys.stdout.write(os.environ.get(\"PYTHONPATH\", \"\"))"

func TestPytorchCollectProperties(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "pytorch-collect")
	properties := h.ToolProperties()

	assert.Equal(t, "pytorch-collect", properties.Name)
	assert.Equal(t, "1.0.0", properties.Version)
	assert.True(t, properties.SupportsWorkloadLaunch)
	require.Len(t, properties.Deployments, 1)
	require.Len(t, properties.Deployments[0].Dependencies, 2)
	for index, name := range []string{"pytorch-collect", "parquet-writer"} {
		dependency := properties.Deployments[0].Dependencies[index]
		assert.Equal(t, deploymentsupport.DependencyTypeToolBundle, dependency.Type)
		assert.Equal(t, name, dependency.Name)
		assert.Equal(t, versions.GetVersion(), dependency.Version)
	}
}

func TestPytorchCollectProbe(t *testing.T) {
	t.Run("checks Python modules, deployments, and workload", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "pytorch-collect")
		engine := &mocks.MockToolEngine{}
		expectLinux(engine)
		ctx := pytorchCollectContext()

		expectSuccessfulPytorchCollectProbe(t, h, engine, ctx, ".venv/bin/python3")

		result, err := h.ToolProbe(t, engine, ctx)

		require.NoError(t, err)
		assert.True(t, result.Available)
		assert.Empty(t, result.Advice)
		engine.AssertExpectations(t)
	})

	t.Run("uses a configured python environment", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "pytorch-collect")
		engine := &mocks.MockToolEngine{}
		expectLinux(engine)
		ctx := pytorchCollectContext()
		ctx.Params["python"] = "/opt/pytorch/bin/python3"

		expectSuccessfulPytorchCollectProbe(t, h, engine, ctx, "/opt/pytorch/bin/python3")

		result, err := h.ToolProbe(t, engine, ctx)

		require.NoError(t, err)
		assert.True(t, result.Available)
		engine.AssertExpectations(t)
	})

	t.Run("prepends deployment paths to the target PYTHONPATH", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "pytorch-collect")
		engine := &mocks.MockToolEngine{}
		expectLinux(engine)
		ctx := pytorchCollectContext()
		targetPythonPath := "/target/site-packages:/opt/shared/python"
		ctx.Env = map[string]string{"PATH": "/tool/bin", "SHARED": "tool"}
		ctx.Workload = tool_goja.NewWorkloadLaunch(
			"model.py --iterations 2",
			[]string{"model.py", "--iterations", "2"},
			map[string]string{"LD_LIBRARY_PATH": "/workload/lib", "SHARED": "workload"},
			"/work",
			false,
		)
		baseOptions := tool_goja.ExecOptions{
			WorkingDirectory: "/work",
			Environment: map[string]string{
				"PATH":            "/tool/bin",
				"LD_LIBRARY_PATH": "/workload/lib",
				"SHARED":          "workload",
			},
		}
		execOptions := tool_goja.ExecOptions{
			WorkingDirectory: "/work",
			Environment: map[string]string{
				"PATH":            "/tool/bin",
				"LD_LIBRARY_PATH": "/workload/lib",
				"SHARED":          "workload",
				"PYTHONPATH": pytorchCollectRoot() + ":" + parquetWriterRoot() +
					":" + targetPythonPath,
			},
		}
		expectPythonPathRead(t, h, engine, ".venv/bin/python3", baseOptions, targetPythonPath, 0)
		expectPythonProbe(t, h, engine, ".venv/bin/python3", execOptions)
		expectCommand(t, h, engine, []string{"stat", pytorchCollectPackagePath()}, tool_goja.ExecOptions{}, 0)
		expectCommand(t, h, engine, []string{"stat", parquetWriterModulePath(), parquetWriterBinaryPath()}, tool_goja.ExecOptions{}, 0)
		expectWorkloadProbe(t, h, engine, ".venv/bin/python3", execOptions, "model.py", 0)

		result, err := h.ToolProbe(t, engine, ctx)

		require.NoError(t, err)
		assert.True(t, result.Available)
		engine.AssertExpectations(t)
	})

	t.Run("uses Python from PATH for an empty python intrepreter", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "pytorch-collect")
		engine := &mocks.MockToolEngine{}
		expectLinux(engine)
		ctx := pytorchCollectContext()
		ctx.Params["python"] = ""

		expectSuccessfulPytorchCollectProbe(t, h, engine, ctx, "python3")

		result, err := h.ToolProbe(t, engine, ctx)

		require.NoError(t, err)
		assert.True(t, result.Available)
		engine.AssertExpectations(t)
	})

	t.Run("reports normal readiness advice when PYTHONPATH cannot be read", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "pytorch-collect")
		engine := &mocks.MockToolEngine{}
		expectLinux(engine)
		ctx := pytorchCollectContext()
		python := ".venv/bin/python3"
		execOptions := baseExecOptions("/work")

		expectPythonPathRead(t, h, engine, python, execOptions, "", 1)
		expectCommand(t, h, engine, []string{python, "--version"}, execOptions, 1)
		expectCommand(t, h, engine, []string{"stat", pytorchCollectPackagePath()}, tool_goja.ExecOptions{}, 0)
		expectCommand(t, h, engine, []string{"stat", parquetWriterModulePath(), parquetWriterBinaryPath()}, tool_goja.ExecOptions{}, 0)
		expectCommand(t, h, engine, []string{"test", "-f", "model.py"}, execOptions, 0)

		result, err := h.ToolProbe(t, engine, ctx)

		require.NoError(t, err)
		assert.False(t, result.Available)
		require.Len(t, result.Advice, 1)
		assert.Equal(t, "engine.recipeparser.js_recipe_stage.READINESS_MESSAGE", result.Advice[0].MessageCode)
		engine.AssertExpectations(t)
	})

	t.Run("reports a missing Parquet writer bundle", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "pytorch-collect")
		engine := &mocks.MockToolEngine{}
		expectLinux(engine)
		ctx := pytorchCollectContext()
		python := ".venv/bin/python3"
		baseOptions := baseExecOptions("/work")
		execOptions := withPythonPath(baseOptions, "")

		expectPythonPathRead(t, h, engine, python, baseOptions, "", 0)
		expectPythonProbe(t, h, engine, python, execOptions)
		expectCommand(t, h, engine, []string{"stat", pytorchCollectPackagePath()}, tool_goja.ExecOptions{}, 0)
		expectCommand(t, h, engine, []string{"stat", parquetWriterModulePath(), parquetWriterBinaryPath()}, tool_goja.ExecOptions{}, 1)
		expectWorkloadProbe(t, h, engine, python, execOptions, "model.py", 0)
		engine.On("GetLocality").Return("target", nil).Once()

		result, err := h.ToolProbe(t, engine, ctx)

		require.NoError(t, err)
		assert.False(t, result.Available)
		require.Len(t, result.Advice, 1)
		assert.Equal(t, "tool_integrations.common.TOOL_NOT_DEPLOYED", result.Advice[0].MessageCode)
		assert.Equal(t, "parquet-writer", result.Advice[0].Metadata["tool"])
		assert.Equal(t, parquetWriterRoot(), result.Advice[0].Metadata["deployPath"])
		engine.AssertExpectations(t)
	})

	t.Run("rejects a Python interpreter as the workload script", func(t *testing.T) {
		h := jstest.LoadToolIntegration(t, "pytorch-collect")
		engine := &mocks.MockToolEngine{}
		expectLinux(engine)
		ctx := pytorchCollectContext()
		ctx.Workload = tool_goja.NewWorkloadLaunch(
			"/usr/bin/python3 model.py",
			[]string{"/usr/bin/python3", "model.py"},
			nil,
			"/work",
			false,
		)
		python := ".venv/bin/python3"
		baseOptions := baseExecOptions("/work")
		execOptions := withPythonPath(baseOptions, "")

		expectPythonPathRead(t, h, engine, python, baseOptions, "", 0)
		expectPythonProbe(t, h, engine, python, execOptions)
		expectCommand(t, h, engine, []string{"stat", pytorchCollectPackagePath()}, tool_goja.ExecOptions{}, 0)
		expectCommand(t, h, engine, []string{"stat", parquetWriterModulePath(), parquetWriterBinaryPath()}, tool_goja.ExecOptions{}, 0)
		expectWorkloadProbe(t, h, engine, python, execOptions, "/usr/bin/python3", 1)

		result, err := h.ToolProbe(t, engine, ctx)

		require.NoError(t, err)
		assert.False(t, result.Available)
		require.Len(t, result.Advice, 1)
		assert.Equal(t, "tool_integrations.pytorch_collect.MODULE_PARSE_FAILED", result.Advice[0].MessageCode)
		assert.Equal(t, map[string]string{"module": "/usr/bin/python3"}, result.Advice[0].Metadata)
		engine.AssertExpectations(t)
	})
}

func TestPytorchCollectRun(t *testing.T) {
	t.Run("emits finalized partial output after a requested stop", func(t *testing.T) {
		err := runPytorchCollect(t, 130, true, true, nil)
		require.NoError(t, err)
	})

	t.Run("succeeds without output when stopped before finalization", func(t *testing.T) {
		err := runPytorchCollect(t, -1, true, false, nil)
		require.NoError(t, err)
	})

	t.Run("ignores an interrupt failure for a stop requested before startup", func(t *testing.T) {
		err := runPytorchCollect(t, -1, true, false, errors.New("process already exited"))
		require.NoError(t, err)
	})

	t.Run("emits output after normal completion", func(t *testing.T) {
		err := runPytorchCollect(t, 0, false, true, nil)
		require.NoError(t, err)
	})

	t.Run("does not hide an unexpected exit", func(t *testing.T) {
		err := runPytorchCollect(t, 7, false, false, nil)
		require.Error(t, err)
	})

	t.Run("requires both outputs", func(t *testing.T) {
		err := runPytorchCollect(t, 130, true, true, nil, "operator_calls.parquet")
		require.Error(t, err)
	})

	t.Run("exit 130 without a stop request fails", func(t *testing.T) {
		err := runPytorchCollect(t, 130, false, false, nil)
		require.Error(t, err)
	})

	t.Run("normal exit without a completion marker fails", func(t *testing.T) {
		err := runPytorchCollect(t, 0, false, false, nil)
		require.Error(t, err)
	})
}

func TestPytorchCollectStop(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "pytorch-collect")
	handle := &tool_mocks.MockProcessHandle{}
	handle.On("Stdout").Return(nil).Once()
	handle.On("Stderr").Return(nil).Once()
	handle.On("Interrupt").Return(nil).Once()
	ctx := pytorchCollectContext()
	ctx.Metadata["processHandle"] = h.ToJSProcessHandleNoOptions(t, handle)

	err := h.ToolStop(t, nil, ctx)

	require.NoError(t, err)
	assert.Equal(t, true, ctx.Metadata["requestStop"])
	handle.AssertExpectations(t)
}

func TestPytorchCollectStopIgnoresInterruptFailure(t *testing.T) {
	h := jstest.LoadToolIntegration(t, "pytorch-collect")
	engine := &mocks.MockToolEngine{}
	handle := &tool_mocks.MockProcessHandle{}
	handle.On("Stdout").Return(nil).Once()
	handle.On("Stderr").Return(nil).Once()
	handle.On("Interrupt").Return(errors.New("process already exited")).Once()
	engine.On("Log", "warn", "Failed to interrupt PyTorch collector: process already exited").Return(nil).Once()
	ctx := pytorchCollectContext()
	ctx.Metadata["processHandle"] = h.ToJSProcessHandleNoOptions(t, handle)

	err := h.ToolStop(t, engine, ctx)

	require.NoError(t, err)
	assert.Equal(t, true, ctx.Metadata["requestStop"])
	engine.AssertExpectations(t)
	handle.AssertExpectations(t)
}

func expectSuccessfulPytorchCollectProbe(
	t *testing.T,
	h *jstest.ToolIntegrationJSHarness,
	engine *mocks.MockToolEngine,
	ctx tool_goja.ToolContext,
	python string,
) {
	t.Helper()
	baseOptions := baseExecOptions(ctx.Workload.(*tool_goja.WorkloadLaunch).WorkingDir)
	execOptions := withPythonPath(baseOptions, "")
	expectPythonPathRead(t, h, engine, python, baseOptions, "", 0)
	expectPythonProbe(t, h, engine, python, execOptions)
	expectCommand(t, h, engine, []string{"stat", pytorchCollectPackagePath()}, tool_goja.ExecOptions{}, 0)
	expectCommand(t, h, engine, []string{"stat", parquetWriterModulePath(), parquetWriterBinaryPath()}, tool_goja.ExecOptions{}, 0)
	expectWorkloadProbe(t, h, engine, python, execOptions, "model.py", 0)
}

func baseExecOptions(workingDirectory string) tool_goja.ExecOptions {
	return tool_goja.ExecOptions{
		WorkingDirectory: workingDirectory,
		Environment:      map[string]string{},
	}
}

func withPythonPath(options tool_goja.ExecOptions, existingPythonPath string) tool_goja.ExecOptions {
	environment := make(map[string]string, len(options.Environment)+1)
	for name, value := range options.Environment {
		environment[name] = value
	}
	pythonPath := pytorchCollectRoot() + ":" + parquetWriterRoot()
	if existingPythonPath != "" {
		pythonPath += ":" + existingPythonPath
	}
	options.Environment = environment
	options.Environment["PYTHONPATH"] = pythonPath
	return options
}

func expectPythonPathRead(
	t *testing.T,
	h *jstest.ToolIntegrationJSHarness,
	engine *mocks.MockToolEngine,
	python string,
	options tool_goja.ExecOptions,
	pythonPath string,
	rc int32,
) {
	t.Helper()
	engine.On("ExecCommand", []string{python, "-c", readPythonPathScript}, options).
		Return(h.ToJSValPromise(t, process.CommandResult{Rc: rc, Stdout: pythonPath}, nil)).Once()
}

func expectPythonProbe(
	t *testing.T,
	h *jstest.ToolIntegrationJSHarness,
	engine *mocks.MockToolEngine,
	python string,
	options tool_goja.ExecOptions,
) {
	t.Helper()
	expectCommand(t, h, engine, []string{python, "--version"}, options, 0)
	expectCommand(t, h, engine, []string{python, "-c", "import sys; sys.exit(sys.version_info < (3, 8))"}, options, 0)
	expectCommand(t, h, engine, []string{python, "-c", "import importlib, sys; importlib.import_module(sys.argv[1])", "torch"}, options, 0)
}

func expectWorkloadProbe(
	t *testing.T,
	h *jstest.ToolIntegrationJSHarness,
	engine *mocks.MockToolEngine,
	python string,
	options tool_goja.ExecOptions,
	module string,
	parseRC int32,
) {
	t.Helper()
	expectCommand(t, h, engine, []string{"test", "-f", module}, options, 0)
	expectCommand(t, h, engine, []string{python, "-c", parsePythonScript, module}, options, parseRC)
}

func expectCommand(
	t *testing.T,
	h *jstest.ToolIntegrationJSHarness,
	engine *mocks.MockToolEngine,
	command []string,
	options tool_goja.ExecOptions,
	rc int32,
) {
	t.Helper()
	engine.On("ExecCommand", command, options).
		Return(h.ToJSValPromise(t, process.CommandResult{Rc: rc}, nil)).Once()
}

func runPytorchCollect(t *testing.T, exitCode int, requestStop bool, completionMarker bool, interruptError error, missingOutputs ...string) error {
	t.Helper()
	h := jstest.LoadToolIntegration(t, "pytorch-collect")
	engine := &mocks.MockToolEngine{}
	expectLinux(engine)
	handle := &tool_mocks.MockProcessHandle{}
	handle.On("Stdout").Return(nil).Once()
	handle.On("Stderr").Return(nil).Once()
	ctx := pytorchCollectContext()
	if requestStop {
		require.NoError(t, h.ToolStop(t, nil, ctx))
		assert.Equal(t, true, ctx.Metadata["requestStop"])
		handle.On("Interrupt").Return(interruptError).Once()
		if interruptError != nil {
			engine.On("Log", "warn", "Failed to interrupt PyTorch collector: "+interruptError.Error()).Return(nil).Once()
		}
	}

	baseOptions := baseExecOptions("/work")
	execOptions := withPythonPath(baseOptions, "")
	expectPythonPathRead(t, h, engine, ".venv/bin/python3", baseOptions, "", 0)
	expectWorkloadProbe(t, h, engine, ".venv/bin/python3", execOptions, "model.py", 0)
	expectCommand(t, h, engine, []string{"stat", pytorchCollectPackagePath()}, tool_goja.ExecOptions{}, 0)
	expectCommand(t, h, engine, []string{"stat", parquetWriterModulePath()}, tool_goja.ExecOptions{}, 0)
	expectCommand(t, h, engine, []string{"stat", parquetWriterBinaryPath()}, tool_goja.ExecOptions{}, 0)
	engine.On("CreateTempDir").Return(h.ToJSValPromise(t, "/output", nil)).Once()
	engine.On("StartProgressTracker", "Collecting PyTorch call data").Return(nil).Once()
	engine.On("EndProgress", "Collecting PyTorch call data").Return(nil).Once()

	jsHandle := h.ToJSProcessHandleNoOptions(t, handle)
	handle.On("Wait").Return(exitCode, nil).Once()
	engine.On(
		"StartProcess",
		[]string{".venv/bin/python3", "-m", "pytorch_collect.cli", "--output", "/output", "--writer", "parquet", "--completion-marker", "/output/pytorch_collect_complete", "--", "model.py", "--iterations", "2"},
		tool_goja.ProcessOptions{
			WorkingDirectory: "/work",
			Environment:      execOptions.Environment,
			Stdout:           tool_goja.StreamRedirect{Redirect: "file", Path: "/output/pytorch_collect_stdout.txt"},
			Stderr:           tool_goja.StreamRedirect{Redirect: "file", Path: "/output/pytorch_collect_stderr.txt"},
		},
	).Return(h.ToJSValPromise(t, jsHandle, nil)).Once()

	expectCommand(t, h, engine, []string{"stat", "/output/pytorch_collect_stdout.txt"}, tool_goja.ExecOptions{}, 1)
	expectCommand(t, h, engine, []string{"stat", "/output/pytorch_collect_stderr.txt"}, tool_goja.ExecOptions{}, 1)
	if exitCode == 0 || requestStop && (exitCode == 130 || exitCode == -1) {
		var markerRC int32 = 1
		if completionMarker {
			markerRC = 0
		}
		expectCommand(t, h, engine, []string{"stat", "/output/pytorch_collect_complete"}, tool_goja.ExecOptions{}, markerRC)
	}
	if completionMarker {
		missing := make(map[string]bool, len(missingOutputs))
		for _, output := range missingOutputs {
			missing[output] = true
		}
		for _, output := range []struct {
			name      string
			component tool_goja.OutputMetadata
		}{
			{name: "api_calls.parquet", component: tool_goja.OutputMetadata{ComponentType: "pytorch-api-calls-parquet", Version: "1.0"}},
			{name: "operator_calls.parquet", component: tool_goja.OutputMetadata{ComponentType: "pytorch-operator-calls-parquet", Version: "1.0"}},
		} {
			var rc int32
			if missing[output.name] {
				rc = 1
			}
			expectCommand(t, h, engine, []string{"stat", "/output/" + output.name}, tool_goja.ExecOptions{}, rc)
			if rc == 0 {
				engine.On("EmitOutput", "/output/"+output.name, output.name, output.component, tool.TransferOptions{}).Return(nil).Once()
			}
		}
	}

	err := h.ToolRun(t, engine, ctx)
	engine.AssertExpectations(t)
	handle.AssertExpectations(t)
	return err
}

func pytorchCollectContext() tool_goja.ToolContext {
	ctx := jstest.EmptyToolContext()
	ctx.Params["python"] = ".venv/bin/python3"
	ctx.ToolsRoot = "/target/tools"
	ctx.Workload = tool_goja.NewWorkloadLaunch(
		"model.py --iterations 2",
		[]string{"model.py", "--iterations", "2"},
		nil,
		"/work",
		false,
	)
	return ctx
}

func expectLinux(engine *mocks.MockToolEngine) {
	engine.On("GetPlatform").Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil).Maybe()
}

func pytorchCollectRoot() string {
	return "/target/tools/pytorch-collect/" + versions.GetVersion()
}

func pytorchCollectPackagePath() string {
	return pytorchCollectRoot() + "/pytorch_collect"
}

func parquetWriterRoot() string {
	return "/target/tools/parquet-writer/" + versions.GetVersion()
}

func parquetWriterModulePath() string {
	return parquetWriterRoot() + "/apx_parquet_writer.py"
}

func parquetWriterBinaryPath() string {
	return parquetWriterRoot() + "/parquet-writer"
}
