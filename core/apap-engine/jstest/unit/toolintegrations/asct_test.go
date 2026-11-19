// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package toolintegrations

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest"
	"github.com/Arm-Debug/apap-cli/apap-engine/jstest/mocks"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
)

type deploymentPaths struct {
	DeploymentPath string   `json:"deploymentPath"`
	ArchivePath    string   `json:"archivePath"`
	InstallerPath  string   `json:"installerPath"`
	RequiredInputs []string `json:"requiredInputs"`
}

func TestASCTInstallerFailureRetainsDependencyLogs(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "asct")
	engine := mocks.MockToolEngineIgnoreLogs()
	context := jstest.EmptyToolContext()
	context.ToolsRoot = "/target/tools"

	installRoot, err := harness.Call[string](t, "resolvePersistentInstallRoot", context.ToolsRoot)
	require.NoError(t, err)
	deployment, err := harness.Call[deploymentPaths](t, "buildDeploymentPaths", context.ToolsRoot)
	require.NoError(t, err)
	require.Len(t, deployment.RequiredInputs, 2)
	bundlePath := deployment.ArchivePath
	installerPath := deployment.InstallerPath
	installerInputs := installRoot + "/installer-inputs"
	const setupLogs = "/tmp/asct-run/setup-logs"

	engine.On("CreateTempDir").
		Return(harness.ToJSValPromise(t, "/tmp/asct-run", nil)).Once()
	engine.On("MkDir", mock.AnythingOfType("string")).
		Return(harness.ToJSValPromise(t, nil, nil)).Times(4)
	engine.On("EmitOutput",
		installRoot+"/dependency-logs/**/*",
		"setup-logs/dependency-logs/**/*",
		tool_goja.OutputMetadata{ComponentType: "asct-setup-log", Version: "1.0"},
		tool.TransferOptions{},
	).Return(nil).Once()
	engine.On("EmitOutput", mock.MatchedBy(func(path string) bool {
		return path == setupLogs+"/install.stdout.log" || path == setupLogs+"/install.stderr.log"
	}), mock.Anything, tool_goja.OutputMetadata{
		ComponentType: "asct-setup-log",
		Version:       "1.0",
	}, tool.TransferOptions{}).Return(nil).Twice()

	engine.On("ExecCommand", []string{"stat", installRoot + "/asct_venv"}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
	engine.On("ExecCommand", []string{"stat", installRoot}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
	engine.On("ExecCommand", []string{"stat", bundlePath}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{"stat", installerPath}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{"cp", bundlePath, installerPath, installerInputs}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	const tracker = "Installing ASCT into the virtual environment"
	engine.On("StartProgressTracker", tracker).Return(nil).Once()
	engine.On("EndProgress", tracker).Return(nil).Once()
	// TODO: Add coverage for an installer process that starts but exits non-zero,
	// including the exit code and installer diagnostics in the resulting error.
	engine.On("StartProcess", mock.Anything, mock.Anything).
		Return(harness.ToJSValPromise(t, nil, errors.New("installer launch failed"))).Once()
	engine.On("ExecCommand", []string{"cat", setupLogs + "/install.stdout.log"}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{
			Stdout: "Could not use python3 (use --python to select a supported interpreter)",
		}, nil)).Once()
	engine.On("ExecCommand", []string{"cat", setupLogs + "/install.stderr.log"}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
	engine.On("Rm", installerInputs, true, true).
		Return(harness.ToJSValPromise(t, nil, nil)).Once()

	err = harness.ToolRun(t, engine, context)

	require.ErrorContains(t, err, "tool_integrations.asct.INSTALL_FAILED")
	engine.AssertExpectations(t)
}

func TestASCTRunRejectsIncompleteDeployment(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "asct")
	engine := mocks.MockToolEngineIgnoreLogs()
	context := jstest.EmptyToolContext()
	context.ToolsRoot = "/target/tools"

	installRoot, err := harness.Call[string](t, "resolvePersistentInstallRoot", context.ToolsRoot)
	require.NoError(t, err)
	deployment, err := harness.Call[deploymentPaths](t, "buildDeploymentPaths", context.ToolsRoot)
	require.NoError(t, err)

	engine.On("CreateTempDir").
		Return(harness.ToJSValPromise(t, "/tmp/asct-run", nil)).Once()
	engine.On("MkDir", mock.AnythingOfType("string")).
		Return(harness.ToJSValPromise(t, nil, nil)).Times(3)
	engine.On("EmitOutput", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()
	engine.On("ExecCommand", []string{"stat", installRoot + "/asct_venv"}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
	engine.On("ExecCommand", []string{"stat", installRoot}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
	engine.On("ExecCommand", []string{"stat", deployment.ArchivePath}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
	engine.On("ExecCommand", []string{"stat", deployment.InstallerPath}, tool_goja.ExecOptions{}).
		Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()

	err = harness.ToolRun(t, engine, context)

	require.ErrorContains(t, err, "tool_integrations.asct.INCOMPLETE_DEPLOYMENT")
	engine.AssertExpectations(t)
}

func TestASCTRecognizesInstallerCancellation(t *testing.T) {
	harness := jstest.LoadToolIntegration(t, "asct")

	interrupted, err := harness.Call[bool](t, "isInterruptionError",
		map[string]any{"metadata": map[string]any{"interruptType": "cancel"}},
		map[string]any{"code": "engine.common.USER_CANCELED"},
	)

	require.NoError(t, err)
	require.True(t, interrupted)
}

func TestASCTProbe(t *testing.T) {
	t.Run("reports error when Python venv creation fails", func(t *testing.T) {
		harness := jstest.LoadToolIntegration(t, "asct")
		engine := mocks.MockToolEngine{}

		engine.On("CreateTempDir").
			Return(harness.ToJSValPromise(t, "/tmp/asct-probe", nil)).
			Once()
		engine.On("ExecCommand", []string{"python3", "-m", "venv", "/tmp/asct-probe/probe-venv"}, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
		engine.On("ExecCommand", mock.Anything, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{}, nil))

		context := jstest.EmptyToolContext()
		context.ToolsRoot = "/target/tools"

		result, err := harness.ToolProbe(t, &engine, context)

		require.NoError(t, err)
		require.False(t, result.Available)
		require.Len(t, result.Advice, 1)
		require.Equal(t, "error", result.Advice[0].Level)
		require.Equal(t, message.EngineRecipeparserJsRecipeStageReadinessMessage, result.Advice[0].MessageCode)
		require.Contains(t, result.Advice[0].Metadata["message"], "python3-venv")
		engine.AssertExpectations(t)
	})

	t.Run("reports a missing deployment for a fresh install", func(t *testing.T) {
		harness := jstest.LoadToolIntegration(t, "asct")
		engine := mocks.MockToolEngine{}
		context := jstest.EmptyToolContext()
		context.ToolsRoot = "/target/tools"
		installRoot, err := harness.Call[string](t, "resolvePersistentInstallRoot", context.ToolsRoot)
		require.NoError(t, err)
		deployment, err := harness.Call[deploymentPaths](t, "buildDeploymentPaths", context.ToolsRoot)
		require.NoError(t, err)

		engine.On("CreateTempDir").
			Return(harness.ToJSValPromise(t, "/tmp/asct-probe", nil)).Once()
		engine.On("ExecCommand", []string{"stat", installRoot + "/asct_venv"}, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
		engine.On("ExecCommand", []string{"stat", deployment.DeploymentPath}, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
		engine.On("ExecCommand", mock.Anything, tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{}, nil))
		engine.On("GetPlatform").
			Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil)
		engine.On("GetLocality").Return("target", nil).Once()

		result, err := harness.ToolProbe(t, &engine, context)

		require.NoError(t, err)
		require.False(t, result.Available)
		require.Len(t, result.Advice, 1)
		require.Equal(t, message.ToolIntegrationsCommonToolNotDeployed, result.Advice[0].MessageCode)
		require.Equal(t, deployment.DeploymentPath, result.Advice[0].Metadata["deployPath"])
		engine.AssertExpectations(t)
	})

	for _, tc := range []struct {
		name       string
		inputIndex int
	}{
		{name: "source archive", inputIndex: 0},
		{name: "installer", inputIndex: 1},
	} {
		t.Run("reports error when the "+tc.name+" is not deployed", func(t *testing.T) {
			harness := jstest.LoadToolIntegration(t, "asct")
			engine := mocks.MockToolEngine{}
			context := jstest.EmptyToolContext()
			context.ToolsRoot = "/target/tools"
			installRoot, err := harness.Call[string](t, "resolvePersistentInstallRoot", context.ToolsRoot)
			require.NoError(t, err)
			deployment, err := harness.Call[deploymentPaths](t, "buildDeploymentPaths", context.ToolsRoot)
			require.NoError(t, err)
			require.Len(t, deployment.RequiredInputs, 2)
			missingPath := deployment.RequiredInputs[tc.inputIndex]

			engine.On("CreateTempDir").
				Return(harness.ToJSValPromise(t, "/tmp/asct-probe", nil)).Once()
			engine.On("ExecCommand", []string{"stat", installRoot + "/asct_venv"}, tool_goja.ExecOptions{}).
				Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
			engine.On("ExecCommand", []string{"stat", deployment.DeploymentPath}, tool_goja.ExecOptions{}).
				Return(harness.ToJSValPromise(t, process.CommandResult{}, nil)).Once()
			engine.On("ExecCommand", []string{"stat", missingPath}, tool_goja.ExecOptions{}).
				Return(harness.ToJSValPromise(t, process.CommandResult{Rc: 1}, nil)).Once()
			engine.On("ExecCommand", mock.Anything, tool_goja.ExecOptions{}).
				Return(harness.ToJSValPromise(t, process.CommandResult{}, nil))
			engine.On("GetPlatform").
				Return(conductor.PlatformConfiguration{OS: conductor.Linux}, nil)
			engine.On("GetLocality").Return("target", nil).Once()

			result, err := harness.ToolProbe(t, &engine, context)

			require.NoError(t, err)
			require.False(t, result.Available)
			require.Len(t, result.Advice, 1)
			require.Equal(t, "error", result.Advice[0].Level)
			require.Equal(t, message.ToolIntegrationsAsctIncompleteDeployment, result.Advice[0].MessageCode)
			require.Equal(t, deployment.DeploymentPath, result.Advice[0].Metadata["deployPath"])
			engine.AssertExpectations(t)
		})
	}

	t.Run("does not require installer inputs for a reusable install", func(t *testing.T) {
		harness := jstest.LoadToolIntegration(t, "asct")
		engine := mocks.MockToolEngine{}
		context := jstest.EmptyToolContext()
		context.ToolsRoot = "/target/tools"
		deployment, err := harness.Call[deploymentPaths](t, "buildDeploymentPaths", context.ToolsRoot)
		require.NoError(t, err)
		require.Len(t, deployment.RequiredInputs, 2)

		engine.On("CreateTempDir").
			Return(harness.ToJSValPromise(t, "/tmp/asct-probe", nil)).Once()
		engine.On("ExecCommand", mock.MatchedBy(func(command []string) bool {
			return len(command) != 2 || command[0] != "stat" ||
				(command[1] != deployment.RequiredInputs[0] && command[1] != deployment.RequiredInputs[1])
		}), tool_goja.ExecOptions{}).
			Return(harness.ToJSValPromise(t, process.CommandResult{}, nil))

		result, err := harness.ToolProbe(t, &engine, context)

		require.NoError(t, err)
		require.True(t, result.Available)
		require.Empty(t, result.Advice)
		engine.AssertExpectations(t)
	})
}

func TestASCTReusableInstall(t *testing.T) {
	installPaths := map[string]string{
		"installRoot": "/target/install",
		"venvDir":     "/target/install/asct_venv",
		"binaryPath":  "/target/install/asct_venv/bin/asct",
	}

	for _, tc := range []struct {
		name     string
		helpRC   int32
		reusable bool
	}{
		{name: "rejects a failing ASCT entrypoint", helpRC: 1},
		{name: "accepts a healthy cached install", reusable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			harness := jstest.LoadToolIntegration(t, "asct")
			engine := mocks.MockToolEngine{}
			engine.On("ExecCommand", mock.MatchedBy(func(command []string) bool {
				return len(command) == 2 && command[0] == "stat"
			}), tool_goja.ExecOptions{}).
				Return(harness.ToJSValPromise(t, process.CommandResult{}, nil)).Twice()
			engine.On("ExecCommand", []string{installPaths["binaryPath"], "--help"}, tool_goja.ExecOptions{}).
				Return(harness.ToJSValPromise(t, process.CommandResult{Rc: tc.helpRC}, nil)).Once()

			reusable, err := harness.CallAwait[bool](t, "hasReusableInstall", &engine, installPaths)

			require.NoError(t, err)
			require.Equal(t, tc.reusable, reusable)
			engine.AssertExpectations(t)
		})
	}
}
