// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package process

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

const (
	probeEnvironmentOverride  = "APX_PROCESS_PROBE_OVERRIDE"
	probeEnvironmentInherited = "APX_PROCESS_PROBE_INHERITED"
)

func TestRequestPathControlsExecutableLookup(t *testing.T) {
	probe := processcontracttest.ProbePath(t)
	name := filepath.Base(probe)
	probeDirectory := filepath.Dir(probe)
	pathKey := "PATH"
	if runtime.GOOS == "windows" {
		pathKey = "Path"
		name = strings.TrimSuffix(name, ".exe")
	}
	t.Setenv("PATH", t.TempDir())
	_, err := exec.LookPath(name)
	require.Error(t, err, "probe must only be available through the request PATH")
	manager := NewProcessManager()
	command := LaunchCommand{Command: []string{name}, Environment: map[string]string{
		pathKey: probeDirectory,
		processcontracttest.EnvironmentKeysVariable: "[]",
		processcontracttest.ExitCodeVariable:        "0",
	}}
	result, err := manager.ExecCommand(&command)
	require.NoError(t, err)
	require.Zero(t, result.Rc)
	_, err = processcontracttest.DecodeReport(result.Stdout)
	require.NoError(t, err)
	started, err := manager.StartProcess(&StartProcess{LaunchCommand: command})
	require.NoError(t, err)
	defer manager.ReleaseProcessHandles([]int{started.Pid})
	exitCode, err := manager.WaitProcess(context.Background(), started.Pid)
	require.NoError(t, err)
	require.Zero(t, exitCode)

	// A request PATH must also replace, rather than supplement, the parent PATH.
	t.Setenv("PATH", probeDirectory)
	for _, override := range []string{"", t.TempDir()} {
		command.Environment[pathKey] = override
		result, err = manager.ExecCommand(&command)
		require.NoError(t, err)
		require.Equal(t, int32(127), result.Rc)
	}
	// Explicit paths bypass PATH, and resolving a name must not rewrite argv.
	cmd, err := manager.buildCmd(&LaunchCommand{
		Command: []string{probe, "with spaces", ""}, Environment: map[string]string{pathKey: ""},
	})
	require.NoError(t, err)
	require.NoError(t, cmd.command.Err)
	assert.Equal(t, []string{probe, "with spaces", ""}, cmd.command.Args)
	// Windows runners can place the checkout and temporary files on different drives.
	t.Chdir(filepath.Dir(probeDirectory))
	currentDirectory, err := os.Getwd()
	require.NoError(t, err)
	relativeDirectory, err := filepath.Rel(currentDirectory, probeDirectory)
	require.NoError(t, err)
	cmd, err = manager.buildCmd(&LaunchCommand{
		Command: []string{name}, Environment: map[string]string{pathKey: relativeDirectory},
	})
	require.NoError(t, err)
	require.ErrorIs(t, cmd.command.Err, exec.ErrDot)
}

func probeLaunchCommand(t *testing.T, executable, workingDirectory string, exitCode int) (*LaunchCommand, []string, string) {
	t.Helper()
	markerPath := filepath.Join(t.TempDir(), "unexpected-marker")
	arguments := append(processcontracttest.ArgumentValues(), processcontracttest.MarkerArgument(markerPath))
	environmentKeys := []string{probeEnvironmentOverride, probeEnvironmentInherited}
	environmentSelector, err := processcontracttest.EnvironmentSelector(environmentKeys...)
	require.NoError(t, err)
	return &LaunchCommand{
		Command: append([]string{executable}, arguments...),
		Environment: map[string]string{
			processcontracttest.EnvironmentKeysVariable: environmentSelector,
			processcontracttest.ExitCodeVariable:        strconv.Itoa(exitCode),
			probeEnvironmentOverride:                    "override value & literal",
		},
		WorkingDirectory: workingDirectory,
	}, arguments, markerPath
}

func assertProbeReport(t *testing.T, output string, arguments []string, expectedDirectory string, expectedExitCode int) {
	t.Helper()
	report, err := processcontracttest.DecodeReport(output)
	require.NoError(t, err)
	assert.Equal(t, arguments, report.Arguments)
	assert.Equal(t, "override value & literal", report.Environment[probeEnvironmentOverride])
	assert.Equal(t, "inherited value", report.Environment[probeEnvironmentInherited])
	assert.Equal(t, expectedExitCode, report.ExitCode)

	reportedDirectory, err := filepath.EvalSymlinks(report.WorkingDirectory)
	require.NoError(t, err)
	expectedDirectory, err = filepath.EvalSymlinks(expectedDirectory)
	require.NoError(t, err)
	assert.Equal(t, expectedDirectory, reportedDirectory)
}

func TestExecCommandPreservesProcessContract(t *testing.T) {
	t.Setenv(probeEnvironmentInherited, "inherited value")
	probe := processcontracttest.ProbePath(t)
	workingDirectory := t.TempDir()
	command, arguments, markerPath := probeLaunchCommand(t, probe, workingDirectory, 0)

	result, err := NewProcessManager().ExecCommand(command)
	require.NoError(t, err)
	require.Equal(t, int32(0), result.Rc)
	assertProbeReport(t, result.Stdout, arguments, workingDirectory, 0)
	_, err = os.Stat(markerPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestStartProcessPreservesProcessContractWithRelativeDirectory(t *testing.T) {
	t.Setenv(probeEnvironmentInherited, "inherited value")
	probe := processcontracttest.ProbePath(t)
	absoluteDirectory := t.TempDir()
	// Keep the base and target of the relative path on the same Windows drive.
	t.Chdir(filepath.Dir(absoluteDirectory))
	currentDirectory, err := os.Getwd()
	require.NoError(t, err)
	relativeDirectory, err := filepath.Rel(currentDirectory, absoluteDirectory)
	require.NoError(t, err)
	launchCommand, arguments, markerPath := probeLaunchCommand(t, probe, relativeDirectory, 0)
	manager := NewProcessManager()

	started, err := manager.StartProcess(&StartProcess{
		LaunchCommand: *launchCommand,
		Stdout:        StreamRedirect{Mode: Stream},
		Stderr:        StreamRedirect{Mode: Stream},
	})
	require.NoError(t, err)
	exitCode, err := manager.WaitProcess(context.Background(), started.Pid)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	stdout := &fakeSender{ctx: context.Background()}
	require.NoError(t, manager.StreamStdout(started.Pid, stdout))
	assertProbeReport(t, string(bytes.Join(stdout.chunks, nil)), arguments, absoluteDirectory, 0)
	_, err = os.Stat(markerPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	manager.ReleaseProcessHandles([]int{started.Pid})
}

func TestExecCommandFailureKinds(t *testing.T) {
	t.Run("program exits with an error", func(t *testing.T) {
		t.Setenv(probeEnvironmentInherited, "inherited value")
		probe := processcontracttest.ProbePath(t)
		workingDirectory := t.TempDir()
		command, arguments, _ := probeLaunchCommand(t, probe, workingDirectory, 23)

		result, err := NewProcessManager().ExecCommand(command)
		require.NoError(t, err)
		require.Equal(t, int32(23), result.Rc)
		assertProbeReport(t, result.Stdout, arguments, workingDirectory, 23)
	})

	t.Run("program fails to start", func(t *testing.T) {
		probe := processcontracttest.ProbePath(t)
		result, err := NewProcessManager().ExecCommand(&LaunchCommand{
			Command:          []string{probe},
			WorkingDirectory: filepath.Join(t.TempDir(), "does-not-exist"),
		})
		require.Nil(t, result)
		require.ErrorContains(t, err, "error starting command")
	})
}

func TestWindowsEnvironmentOverridesIgnoreCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows environment names are case-insensitive")
	}

	const environmentName = "APX_PROCESS_PROBE_CASE_OVERRIDE"
	t.Setenv(environmentName, "old value")
	environmentSelector, err := processcontracttest.EnvironmentSelector(environmentName)
	require.NoError(t, err)
	probe := processcontracttest.ProbePath(t)
	result, err := NewProcessManager().ExecCommand(&LaunchCommand{
		Command: []string{probe},
		Environment: map[string]string{
			processcontracttest.EnvironmentKeysVariable: environmentSelector,
			processcontracttest.ExitCodeVariable:        "0",
			strings.ToLower(environmentName):            "new value",
		},
	})
	require.NoError(t, err)
	report, err := processcontracttest.DecodeReport(result.Stdout)
	require.NoError(t, err)
	assert.Equal(t, "new value", report.Environment[environmentName])
}
