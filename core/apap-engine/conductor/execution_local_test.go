// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestLocalExecutionContract(t *testing.T) {
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	directory := filepath.Join(t.TempDir(), "space ' & 世界")
	require.NoError(t, os.Mkdir(directory, 0700))
	marker := filepath.Join(directory, "unexpected")
	args := append(processcontracttest.ArgumentValues(), processcontracttest.MarkerArgument(marker))
	value := "secret ' & $()\n世界\\"
	request := &process.LaunchCommand{
		Command: append([]string{probe}, args...), WorkingDirectory: directory,
		Environment: map[string]string{processcontracttest.EnvironmentKeysVariable: `["VALUE","INHERITED"]`,
			processcontracttest.ExitCodeVariable: "23", "VALUE": value},
	}
	t.Setenv("INHERITED", "kept")
	runner := &LocalCommandRunner{}
	result, err := runner.RunProcess(context.Background(), request)
	require.NoError(t, err)
	require.EqualValues(t, 23, result.Rc)
	report, err := processcontracttest.DecodeReport(result.Stdout)
	require.NoError(t, err)
	require.Equal(t, args, report.Arguments)
	require.Equal(t, value, report.Environment["VALUE"])
	require.Equal(t, "kept", report.Environment["INHERITED"])
	actualDirectory, err := filepath.EvalSymlinks(directory)
	require.NoError(t, err)
	require.Equal(t, actualDirectory, report.WorkingDirectory)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err))
	request.Command[0] = filepath.Base(probe)
	request.Environment["PATH"] = filepath.Dir(probe)
	_, err = runner.RunProcess(context.Background(), request)
	require.NoError(t, err)
	request.Command = []string{filepath.Join(directory, "missing-secret")}
	_, err = runner.RunProcess(context.Background(), request)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
}

func TestPOSIXExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	runner := &LocalCommandRunner{}
	t.Run("script arguments and separate output", func(t *testing.T) {
		result, err := runner.RunScript(context.Background(), &ScriptRequest{
			Shell: ShellPOSIX, Source: `printf '%s' "$1"; printf error >&2; exit 7`, Arguments: []string{"' ; $()\n"},
		})
		require.NoError(t, err)
		require.Equal(t, "' ; $()\n", result.Stdout)
		require.Equal(t, "error", result.Stderr)
		require.EqualValues(t, 7, result.Rc)
	})
	t.Run("executable format failure never falls back to shell", func(t *testing.T) {
		directory := t.TempDir()
		program := filepath.Join(directory, "no-shebang")
		require.NoError(t, os.WriteFile(program, []byte("touch unexpected\n"), 0600))
		// The fixture must be executable to test format rejection rather than permissions.
		require.NoError(t, os.Chmod(program, 0700))
		_, err := runner.RunProcess(context.Background(), &process.LaunchCommand{Command: []string{program}, WorkingDirectory: directory})
		var failure *ExecutionError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, process.FailureExecutableFormat, failure.FailureReason)
		_, err = os.Stat(filepath.Join(directory, "unexpected"))
		require.True(t, os.IsNotExist(err))
	})
	t.Run("cancellation preserves partial output", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ready := filepath.Join(t.TempDir(), "ready")
		var result *process.CommandResult
		var err error
		done := make(chan struct{})
		go func() {
			result, err = runner.RunScript(ctx, &ScriptRequest{
				Shell: ShellPOSIX, Source: `printf started; : > "$1"; sleep 30`, Arguments: []string{ready},
			})
			close(done)
		}()
		require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 5*time.Second, 10*time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled process did not finish")
		}
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, "started", result.Stdout)
	})
}

func TestExecutionValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		request *process.LaunchCommand
		kind    ExecutionErrorKind
	}{
		{"nil request", nil, ExecutionInvalid},
		{"missing command", &process.LaunchCommand{}, ExecutionInvalid},
		{"empty executable", &process.LaunchCommand{Command: []string{""}}, ExecutionInvalid},
		{"NUL argument", &process.LaunchCommand{Command: []string{"unused", "bad\x00"}}, ExecutionInvalid},
		{"invalid environment name", &process.LaunchCommand{Command: []string{"unused"}, Environment: map[string]string{"A=B": "secret"}}, ExecutionInvalid},
		{"NUL directory", &process.LaunchCommand{Command: []string{"unused"}, WorkingDirectory: "bad\x00"}, ExecutionInvalid},
		{"unsupported elevation", &process.LaunchCommand{Command: []string{"unused"}, AsPrivileged: true}, ExecutionUnsupported},
		{"unsupported affinity", &process.LaunchCommand{Command: []string{"unused"}, Affinity: []string{"0"}}, ExecutionUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := (&LocalCommandRunner{}).RunProcess(context.Background(), test.request)
			var executionErr *ExecutionError
			require.ErrorAs(t, err, &executionErr)
			require.Equal(t, test.kind, executionErr.Kind)
		})
	}
}

func TestLocalExecutionKeepsResultWhenCancellationArrivesAfterCompletion(t *testing.T) {
	for _, code := range []int32{0, 1, 23} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			// Given a command that has already completed with output and an exit code.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			expected := &process.CommandResult{Rc: code, Stdout: "completed", Stderr: "diagnostic"}
			run := func(context.Context, *process.LaunchCommand) (*process.CommandResult, error) {
				// Cancellation arrives after completion, before the caller handles the result.
				cancel()
				if code != 0 {
					return expected, &exec.ExitError{}
				}
				return expected, nil
			}

			// When the local runner handles the completed command.
			result, err := runLocalProcess(ctx, &process.LaunchCommand{Command: []string{"completed"}}, run)

			// Then late cancellation does not replace the valid result.
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.NoError(t, err)
			require.Same(t, expected, result)
		})
	}
}

func TestLocalExecutionPreservesCancellationReportedByProcessRunner(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			// Given a process runner that reports an interrupted command and partial output.
			expected := &process.CommandResult{Rc: -1, Stdout: "started"}
			run := func(context.Context, *process.LaunchCommand) (*process.CommandResult, error) { return expected, cause }

			// When the local runner handles the interruption.
			result, err := runLocalProcess(context.Background(), &process.LaunchCommand{Command: []string{"interrupted"}}, run)

			// Then the original cancellation error and partial output reach the caller.
			require.ErrorIs(t, err, cause)
			require.Same(t, expected, result)
		})
	}
}

func TestLocalExecutionReportsCleanupFailureAfterNonzeroExit(t *testing.T) {
	// Given a child exit joined with a failed cleanup operation.
	expected := &process.CommandResult{Rc: 19}
	run := func(context.Context, *process.LaunchCommand) (*process.CommandResult, error) {
		return expected, errors.Join(&exec.ExitError{}, os.ErrPermission)
	}
	// When the local execution API handles the completed command.
	result, err := runLocalProcess(context.Background(), &process.LaunchCommand{Command: []string{"completed"}}, run)
	// Then a nonzero exit does not hide failed cleanup.
	require.Same(t, expected, result)
	var failure *ExecutionError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, ExecutionLaunch, failure.Kind)
}
