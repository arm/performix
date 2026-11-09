// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Remote launches retain S4U/Highest to detach from the SSH job.
// This tests registration policy with mocked Task Scheduler cmdlets, not Windows
// permission enforcement. Local launches bypass this block entirely.
func TestWindowsLauncherRegistrationPrincipal(t *testing.T) {
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		powershell, err = exec.LookPath("pwsh")
	}
	if err != nil {
		t.Skip("PowerShell is required to execute the Windows launcher registration policy")
	}
	script, err := os.ReadFile("../../scripts/verify-and-launch.ps1")
	require.NoError(t, err)
	_, settings, found := strings.Cut(string(script), "$settings = New-ScheduledTaskSettingsSet")
	require.True(t, found)
	settings, _, found = strings.Cut(settings, "# Register under the CURRENT USER")
	require.True(t, found)
	settings = "$settings = New-ScheduledTaskSettingsSet" + settings
	_, registration, found := strings.Cut(string(script), "$registration = @{")
	require.True(t, found)
	registration, _, found = strings.Cut(registration, "Register-ScheduledTask @registration | Out-Null")
	require.True(t, found)
	registration = "$registration = @{" + registration + "Register-ScheduledTask @registration | Out-Null"

	setup := `
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 3
$identity = @{ Name = 'test-user' }
$global:TaskPath = '\'
$global:TaskName = 'test-task'
$action = $trigger = 'unused'
function New-ScheduledTaskSettingsSet {
  param([switch]$AllowStartIfOnBatteries, [switch]$DontStopIfGoingOnBatteries,
        [switch]$StartWhenAvailable, $MultipleInstances, $Priority)
  return [pscustomobject]@{ Priority = $Priority }
}
function New-ScheduledTaskPrincipal {
  param($UserId, $LogonType, $RunLevel)
  if ($UserId -ne 'test-user') { throw 'Unexpected user' }
  return "$LogonType/$RunLevel"
}
function Register-ScheduledTask {
  param($TaskPath, $TaskName, $Action, $Trigger, $Settings, $Principal)
  if ($PSBoundParameters.ContainsKey('Principal')) {
    [Console]::WriteLine("$Principal/priority=$($Settings.Priority)")
  } else {
    [Console]::WriteLine('current-user')
  }
}
`
	// #nosec G204 -- Runs the repository's launcher code with fixed test inputs using a locally resolved PowerShell executable; no user input is included.
	out, err := exec.Command(powershell, "-NoProfile", "-NonInteractive", "-Command", setup+settings+registration).CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "S4U/Highest/priority=1", strings.TrimSpace(string(out)))
}

// A launch failure must report the original failing command, even if cleanup
// also fails. This tests error reporting only; it does not start an agent.
func TestWindowsLauncherReportsOriginalFailure(t *testing.T) {
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		powershell, err = exec.LookPath("pwsh")
	}
	if err != nil {
		t.Skip("PowerShell is required to execute the Windows launcher error handler")
	}
	script, err := os.ReadFile("../../scripts/verify-and-launch.ps1")
	require.NoError(t, err)
	_, handler, found := strings.Cut(string(script), "# Clean up task + agent on *any* unhandled error")
	require.True(t, found)
	handler, _, found = strings.Cut(handler, "# -------- Main --------")
	require.True(t, found)
	setup := `
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 3
$RequestHighestPrivileges = 0
function Kill-Agent-For-ThisPortFile { throw 'cleanup failed' }
function Remove-PortFileDir {}
function Remove-TempTask {}
`
	// #nosec G204 -- Executes repository error-handler code and a fixed synthetic failure, with no user input.
	out, err := exec.Command(powershell, "-NoProfile", "-NonInteractive", "-Command", setup+handler+"\nthrow 'synthetic launch failure'\n").CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, string(out))
	require.Equal(t, 1, exitErr.ExitCode())
	require.Contains(t, string(out), "FATAL: synthetic launch failure")
	require.Contains(t, string(out), "RequestHighestPrivileges=0")
	require.Contains(t, string(out), "throw 'synthetic launch failure'")
	require.NotContains(t, string(out), "FATAL: cleanup failed")
}

// Exercise the actual local launch block without starting a Windows agent. The
// fake process checks argument quoting and a hidden launch with independent
// standard handles. Windows token inheritance still needs native verification.
func TestWindowsHostLauncherInheritsCallerWithoutScheduledTask(t *testing.T) {
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		powershell, err = exec.LookPath("pwsh")
	}
	if err != nil {
		t.Skip("PowerShell is required to execute the Windows host launcher")
	}
	script, err := os.ReadFile("../../scripts/verify-and-launch.ps1")
	require.NoError(t, err)
	_, block, found := strings.Cut(string(script), "# Local host agents inherit")
	require.True(t, found)
	block, _, found = strings.Cut(block, "# Wait for the agent to publish")
	require.True(t, found)
	block = "# Local host agents inherit" + block
	setup := `
$ErrorActionPreference = 'Stop'
$RequestHighestPrivileges = 0
$resolvedAgent = '/tools/apx-agent.exe'
$AgentArgs = @('start', '--port-file-dir', 'C:\Users\Test User\Temp\', 'embedded"quote')
function New-Object($TypeName) {
  if ($TypeName -eq 'System.Diagnostics.ProcessStartInfo') {
    return [pscustomobject]@{FileName='';WorkingDirectory='';UseShellExecute=$false;WindowStyle=0;Arguments=''}
  }
  if ($TypeName -ne 'System.Diagnostics.Process') { throw "Unexpected type $TypeName" }
  $process = [pscustomobject]@{StartInfo=$null;Id=1234}
  $process | Add-Member ScriptMethod Start {
    if (-not $this.StartInfo.UseShellExecute -or $this.StartInfo.WindowStyle -ne [System.Diagnostics.ProcessWindowStyle]::Hidden) { throw 'Visible launch or inherited pipes' }
    if ($this.StartInfo.FileName -ne $resolvedAgent) { throw 'Wrong executable' }
    if ($this.StartInfo.Arguments -cne '"start" "--port-file-dir" "C:\Users\Test User\Temp\\" "embedded\"quote"') { throw $this.StartInfo.Arguments }
    return $true
  }
  $process | Add-Member ScriptMethod Dispose { $global:disposed = $true }
  return $process
}
function New-ScheduledTaskAction { throw 'Local launch must not use Task Scheduler' }
`
	// #nosec G204 -- Executes repository launcher code with fixed synthetic process inputs.
	out, err := exec.Command(powershell, "-NoProfile", "-NonInteractive", "-Command", setup+block+`
if ($global:AGENTPID -ne 1234 -or -not $global:disposed) { throw 'Process lifecycle not tracked' }
Write-Output 'local launch succeeded'
`).CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "local launch succeeded", strings.TrimSpace(string(out)))
}
