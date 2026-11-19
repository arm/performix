// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package conductor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Arm-Debug/apap-cli/atperf-agent/process/processcontracttest"
)

// Exercise the explicit script API through local execution. Target deployment
// tests separately cover agent script arguments, streams and cancellation.
func testWindowsScriptOutcomes(t *testing.T, runner ExecutionRunner, probe string) {
	t.Helper()
	testWindowsScriptStreams(t, runner)
	for _, test := range []struct {
		name, source, nativeCode string
		expected                 int32
	}{
		{"parenthesized failure follows PowerShell 5.1", `(& $env:APX_TEST_PROBE)`, "37", 0},
		{"subexpression failure follows PowerShell 5.1", `$(& $env:APX_TEST_PROBE)`, "37", 0},
		{"explicit native exit", `& $env:APX_TEST_PROBE; exit $LASTEXITCODE`, "37", 37},
		{"explicit native exit after ignored PowerShell failure", `& $env:APX_TEST_PROBE; Write-Error 'failure' -ErrorAction Ignore; exit $LASTEXITCODE`, "37", 37},
		{"explicit native exit with stderr", `& $env:ComSpec /d /c 'echo native failure 1>&2 & exit /b 37'; exit $LASTEXITCODE`, "0", 37},
		{"native failure with trailing comment", "& $env:APX_TEST_PROBE # done", "37", 1},
		{"explicit return", `return "done"`, "0", 0},
		{"return after recovery", `& $env:APX_TEST_PROBE; return "recovered"`, "37", 0},
		{"named end block native failure", `param($unused) end { & $env:APX_TEST_PROBE }`, "37", 1},
		{"named end block explicit native exit", `end { & $env:APX_TEST_PROBE; exit $LASTEXITCODE }`, "37", 37},
		{"process block native failure", `process { & $env:APX_TEST_PROBE }`, "37", 1},
		{"named blocks recovery", `begin { & $env:APX_TEST_PROBE } end { Write-Output 'recovered' }`, "37", 0},
		{"named blocks return", `begin { & $env:APX_TEST_PROBE } end { return 'recovered' }`, "37", 0},
		{"native failure becomes script failure", `& $env:APX_TEST_PROBE`, "37", 1},
		{"repeated native failure", `& $env:APX_TEST_PROBE; & $env:APX_TEST_PROBE`, "37", 1},
		{"native pipeline failure", `& $env:APX_TEST_PROBE | Out-Null`, "37", 1},
		{"recovered native failure", `& $env:APX_TEST_PROBE; Write-Output 'Recovered successfully'`, "37", 0},
		{"native success", `& $env:APX_TEST_PROBE`, "0", 0},
		{"explicit exit", `exit 19`, "0", 19},
		{"nonterminating PowerShell failure", `Write-Error 'recoverable failure'`, "0", 1},
		{"PowerShell failure after native failure", `& $env:APX_TEST_PROBE; Write-Error 'failure'`, "37", 1},
		{"ignored PowerShell failure after native failure", `& $env:APX_TEST_PROBE; Write-Error 'failure' -ErrorAction Ignore`, "37", 1},
		{"expression failure after native failure", `& $env:APX_TEST_PROBE; 1 / 0`, "37", 1},
		{"native failure after PowerShell failure", `Write-Error 'failure'; & $env:APX_TEST_PROBE`, "37", 1},
		{"nonterminating PowerShell error followed by recovery", `Write-Error 'recoverable failure'; Write-Output 'recovered'`, "0", 0},
		{"caller requests terminating errors", `$ErrorActionPreference = 'Stop'; Write-Error 'failure'; Write-Output 'unreachable'`, "0", 1},
		{"caller owns former status variable", `$__apxExecutionStatus = 'caller'; & $env:APX_TEST_PROBE`, "37", 1},
		{"caller owns scoped case variant of status variable", `$script:__APXEXECUTIONSTATUS = 'caller'; & $env:APX_TEST_PROBE`, "37", 1},
		{"caller status mutation before return cannot change result", `$status.Succeeded = $false; return 'done'`, "0", 0},
		{"PowerShell failure", `throw 'test failure'`, "0", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := runner.RunScript(ctx, &ScriptRequest{Shell: ShellPowerShell, Source: test.source,
				Environment: map[string]string{"APX_TEST_PROBE": probe, processcontracttest.ExitCodeVariable: test.nativeCode, processcontracttest.EnvironmentKeysVariable: `[]`}})
			require.NoError(t, err)
			require.Equal(t, test.expected, result.Rc)
		})
	}
}

func testWindowsScriptStreams(t *testing.T, runner ExecutionRunner) {
	t.Helper()
	t.Run("literal arguments and separate output", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := runner.RunScript(ctx, &ScriptRequest{Shell: ShellPowerShell, Source: `param($value) [Console]::Write($value); [Console]::Error.Write('error'); exit 19`, Arguments: []string{"' & $() 世界"}})
		require.NoError(t, err)
		require.EqualValues(t, 19, result.Rc)
		require.Equal(t, "' & $() 世界", result.Stdout)
		require.Equal(t, "error", result.Stderr)
	})
}
