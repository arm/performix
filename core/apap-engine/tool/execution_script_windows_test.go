// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

func TestWindowsScriptLargePayload(t *testing.T) {
	agent, _ := newExecutionRPCFixture(t)
	// Both source and the literal argument exceed 64 KiB. Go passes the payload
	// in a Unicode process environment, outside the Windows command-line limit.
	source := "param($value)\n" + strings.Repeat("# padding\n", 8192) + "[Console]::Write($value)"
	value := strings.Repeat("literal '$() & 世界\r\n", 8192)
	for _, test := range []struct {
		name   string
		runner conductor.ExecutionRunner
	}{
		{"local", &conductor.LocalCommandRunner{}},
		{"agent RPC", agent},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := test.runner.RunScript(ctx, &conductor.ScriptRequest{
				Shell: conductor.ShellPowerShell, Source: source, Arguments: []string{value},
			})
			require.NoError(t, err)
			require.Zero(t, result.Rc, result.Stderr)
			require.Equal(t, len(value), len(result.Stdout))
			require.True(t, value == result.Stdout, "large argument must arrive unchanged")
			require.Empty(t, result.Stderr)
		})
	}
}

func TestWindowsPowerShellCallerVariables(t *testing.T) {
	// Given local and agent runners and a script that inspects wrapper names.
	agent, _ := newExecutionRPCFixture(t)
	for _, test := range []struct {
		name   string
		runner conductor.ExecutionRunner
	}{
		{"local", &conductor.LocalCommandRunner{}},
		{"agent RPC", agent},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// When the caller reads names that it did not declare.
			result, err := test.runner.RunScript(ctx, &conductor.ScriptRequest{
				Shell: conductor.ShellPowerShell,
				Source: `param($value)
foreach ($name in @('Request', 'source', 'payload', 'scriptArguments', 'callerModule', 'statusState')) {
  if ($null -ne (Get-Variable -Name $name -ErrorAction SilentlyContinue)) { throw "Unexpected variable: $name" }
}
[Console]::Write($value)`,
				Arguments: []string{"caller value"},
			})
			// Then wrapper variables are absent and explicit arguments still arrive.
			require.NoError(t, err)
			require.Zero(t, result.Rc, result.Stderr)
			require.Equal(t, "caller value", result.Stdout)
			require.Empty(t, result.Stderr)
		})
	}
}

func TestWindowsCmdLargeScript(t *testing.T) {
	// Given a script exceeding cmd's command-line limit, with short individual lines.
	agent, _ := newExecutionRPCFixture(t)
	source := "@echo off\r\n" + strings.Repeat("rem padding\r\n", 8192) + "echo 世界\r\nexit /b 19\r\n"
	for _, test := range []struct {
		name   string
		runner conductor.ExecutionRunner
	}{
		{"local", &conductor.LocalCommandRunner{}},
		{"agent RPC", agent},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "scripts with spaces")
			require.NoError(t, os.Mkdir(directory, 0700))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// When the complete script runs through either execution route.
			result, err := test.runner.RunScript(ctx, &conductor.ScriptRequest{
				Shell: conductor.ShellCmd, Source: source,
				Environment: map[string]string{"TEMP": directory, "TMP": directory},
			})
			// Then its final command executes and its exit code is preserved.
			require.NoError(t, err)
			require.EqualValues(t, 19, result.Rc, result.Stderr)
			require.Equal(t, "世界\r\n", result.Stdout)
			require.Empty(t, result.Stderr)
			files, err := os.ReadDir(directory)
			require.NoError(t, err)
			require.Empty(t, files, "temporary script must be deleted")
		})
	}
}

func TestWindowsScriptPathOverride(t *testing.T) {
	// Given a PATH containing no shell executables and both execution routes.
	agent, _ := newExecutionRPCFixture(t)
	path := t.TempDir()
	for _, runner := range []struct {
		name      string
		execution conductor.ExecutionRunner
	}{{"local", &conductor.LocalCommandRunner{}}, {"agent RPC", agent}} {
		for _, script := range []struct {
			shell  conductor.Shell
			source string
			output string
		}{
			{conductor.ShellPowerShell, `[Console]::Write($env:PATH)`, path},
			{conductor.ShellCmd, `echo %PATH%`, path + "\r\n"},
		} {
			t.Run(runner.name+"/"+string(script.shell), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				// When a script overrides PATH using Windows' case-insensitive spelling.
				result, err := runner.execution.RunScript(ctx, &conductor.ScriptRequest{
					Shell: script.shell, Source: script.source,
					Environment: map[string]string{"Path": path},
				})
				// Then the shell starts and the caller observes its requested PATH.
				require.NoError(t, err)
				require.Zero(t, result.Rc, result.Stderr)
				require.Equal(t, script.output, result.Stdout)
				require.Empty(t, result.Stderr)
			})
		}
	}
}

func TestWindowsScriptEmptyEnvironmentValues(t *testing.T) {
	// Given an explicitly empty variable, a replaced inherited value, and an absent variable.
	t.Setenv("APX_TEST_REPLACED_EMPTY", "inherited")
	t.Setenv("APX_TEST_MISSING_EMPTY", "")
	require.NoError(t, os.Unsetenv("APX_TEST_MISSING_EMPTY"))
	agent, _ := newExecutionRPCFixture(t)
	check := `$values = [Environment]::GetEnvironmentVariables(); if (-not $values.Contains('APX_TEST_NEW_EMPTY') -or $values['APX_TEST_NEW_EMPTY'] -cne '' -or -not $values.Contains('APX_TEST_REPLACED_EMPTY') -or $values['APX_TEST_REPLACED_EMPTY'] -cne '' -or $values.Contains('APX_TEST_MISSING_EMPTY')) { exit 23 }; [Console]::Write('preserved')`
	for _, runner := range []struct {
		name      string
		execution conductor.ExecutionRunner
	}{{"local", &conductor.LocalCommandRunner{}}, {"agent RPC", agent}} {
		for _, script := range []struct {
			shell  conductor.Shell
			source string
		}{
			{conductor.ShellPowerShell, check},
			{conductor.ShellCmd, `"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -Command "` + check + `"`},
		} {
			t.Run(runner.name+"/"+string(script.shell), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				// When a script sets empty values and also restricts PATH.
				result, err := runner.execution.RunScript(ctx, &conductor.ScriptRequest{
					Shell: script.shell, Source: script.source,
					Environment: map[string]string{
						"APX_TEST_NEW_EMPTY": "", "APX_TEST_REPLACED_EMPTY": "", "PATH": t.TempDir(),
					},
				})
				// Then empty values remain present, including in a child of cmd.
				require.NoError(t, err)
				require.Zero(t, result.Rc, result.Stderr)
				require.Equal(t, "preserved", result.Stdout)
				require.Empty(t, result.Stderr)
			})
		}
	}
}

func TestWindowsCmdCancellationStopsTreeAndRemovesScript(t *testing.T) {
	// Given local and agent runners and a probe whose process tree stays alive.
	agent, _ := newExecutionRPCFixture(t)
	probe, cleanup, err := processcontracttest.BuildProbe(runtime.GOOS, runtime.GOARCH)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	for _, runner := range []struct {
		name      string
		execution conductor.ExecutionRunner
	}{{"local", &conductor.LocalCommandRunner{}}, {"agent RPC", agent}} {
		t.Run(runner.name, func(t *testing.T) {
			directory := t.TempDir()
			pidFile := filepath.Join(directory, "pids.json")
			scriptFile := filepath.Join(directory, "script-directory")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				result, err := runner.execution.RunScript(ctx, &conductor.ScriptRequest{
					Shell:  conductor.ShellCmd,
					Source: `echo %~dp0> "%APX_SCRIPT_PATH%"` + "\r\n" + `"%APX_TEST_PROBE%"`,
					Environment: map[string]string{
						"APX_SCRIPT_PATH": scriptFile, "APX_TEST_PROBE": probe,
						"APX_PROCESS_PROBE_TREE": "parent", "APX_PROCESS_PROBE_TREE_PIDS": pidFile,
					},
				})
				if err == nil && result.Rc != 0 {
					err = fmt.Errorf("cmd exited with %d: %s", result.Rc, result.Stderr)
				}
				done <- err
				close(done)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("cancelled script did not finish")
				}
			})
			// Readiness and stable process handles replace delayed writes and fixed sleeps.
			requireStopped := processcontracttest.AwaitProcessTree(t, pidFile)
			data, err := os.ReadFile(scriptFile)
			require.NoError(t, err)
			scriptDirectory := filepath.Clean(strings.TrimSpace(string(data)))
			require.DirExists(t, scriptDirectory)
			// When the caller cancels after the whole tree has started.
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled script did not return")
			}
			// Then every descendant has exited and the actual staging directory is gone.
			requireStopped()
			_, err = os.Stat(scriptDirectory)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestWindowsCmdDoesNotStageInCallerTemp(t *testing.T) {
	// Given a nonexistent caller TEMP and both execution routes.
	agent, _ := newExecutionRPCFixture(t)
	parent := t.TempDir()
	temp := filepath.Join(parent, "missing scripts")
	for _, runner := range []struct {
		name      string
		execution conductor.ExecutionRunner
	}{{"local", &conductor.LocalCommandRunner{}}, {"agent RPC", agent}} {
		t.Run(runner.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// When cmd runs with TEMP overridden to an unsuitable staging location.
			result, err := runner.execution.RunScript(ctx, &conductor.ScriptRequest{
				Shell: conductor.ShellCmd, Source: "echo %TEMP%",
				Environment: map[string]string{"TEMP": temp},
			})
			// Then startup succeeds and the script sees the exact requested value.
			require.NoError(t, err)
			require.Zero(t, result.Rc, result.Stderr)
			require.Equal(t, temp+"\r\n", result.Stdout)
			require.Empty(t, result.Stderr)
			_, err = os.Stat(temp)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
