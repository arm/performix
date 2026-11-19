# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# RunScript uses this wrapper for both local and agent execution on Windows.
# It reads the UTF-8 request from an environment payload, then dispatches to
# Windows PowerShell 5.1 or cmd without interpolating arguments into source.
# PowerShell status is captured before returning to the wrapper; cmd source is
# written to a batch file in a directory owned and cleaned up by the Go launcher.

# Keep wrapper bookkeeping outside the caller's script/global variable scope.
& {
$scriptErrorActionPreference = $ErrorActionPreference
$ErrorActionPreference = 'Stop'
# Module auto-loading during wrapper setup must not add progress XML to stderr.
$scriptProgressPreference = $ProgressPreference
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = New-Object Text.UTF8Encoding($false)
$payload = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:APX_EXECUTION_SCRIPT_DATA)) | ConvertFrom-Json
[Environment]::SetEnvironmentVariable('APX_EXECUTION_SCRIPT_DATA', $null, 'Process')
$scriptDirectory = $env:APX_EXECUTION_SCRIPT_DIRECTORY
[Environment]::SetEnvironmentVariable('APX_EXECUTION_SCRIPT_DIRECTORY', $null, 'Process')

function Invoke-ApxPowerShellScript {
  param($Request, $ErrorPreference, $ProgressPreferenceValue)
  $scriptArguments = [string[]]@($Request.Arguments)
  # Capture status in the script's scope. Returning through & resets $? in 5.1.
  # Keep a shared object so an explicit return can skip the capture successfully.
  # Implicit completion uses PowerShell's 0/1 status. An explicit exit in the
  # caller still exits this process directly, including exit $LASTEXITCODE.
  $source = $Request.Source
  $parsed = [ScriptBlock]::Create($source)
  $variableNames = @($parsed.Ast.FindAll({
    param($node)
    $node -is [Management.Automation.Language.VariableExpressionAst]
  }, $true) | ForEach-Object { $_.VariablePath.UserPath.Split(':')[-1] })
  do {
    $statusName = '__apxExecutionStatus_' + [Guid]::NewGuid().ToString('N')
  } while ($variableNames -icontains $statusName)
  $callerModule = New-Module -ScriptBlock {}
  $statusState = @{ Succeeded = $true }
  $callerModule.SessionState.PSVariable.Set($statusName, $statusState)
  $capture = @'

$__apxExecutionStatus.Succeeded = $?

'@
  $reset = @'

$__apxExecutionStatus.Succeeded = $true

'@
  # Replace only our fixed templates, never the caller's source. Scoped names
  # and parameter references above are compared case-insensitively.
  $capture = $capture.Replace('__apxExecutionStatus', $statusName)
  $reset = $reset.Replace('__apxExecutionStatus', $statusName)
  if ($parsed.Ast.EndBlock -and $parsed.Ast.EndBlock.Unnamed) {
    # The leading newline keeps a trailing comment from swallowing the capture.
    $source += $capture
  } else {
    # Named blocks cannot have ordinary statements appended after them. Capture
    # at the end of each block that runs, preserving their order and param block.
    $tokens = $null
    $parseErrors = $null
    $null = [Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$parseErrors)
    $blocks = @($parsed.Ast.DynamicParamBlock, $parsed.Ast.BeginBlock, $parsed.Ast.ProcessBlock, $parsed.Ast.EndBlock)
    foreach ($block in ($blocks | Where-Object { $null -ne $_ } | Sort-Object { $_.Extent.StartOffset } -Descending)) {
      $open = $tokens | Where-Object { $_.Kind -eq 'LCurly' -and $_.Extent.StartOffset -ge $block.Extent.StartOffset } | Select-Object -First 1
      $source = $source.Insert($block.Extent.EndOffset - 1, $capture)
      # A return from this block means success, even after an earlier block failed.
      $source = $source.Insert($open.Extent.EndOffset, $reset)
    }
  }
  # Module scope cannot resolve the wrapper function's local variables.
  $callerModule.SessionState.PSVariable.Set('ProgressPreference', $ProgressPreferenceValue)
  $callerModule.SessionState.PSVariable.Set('ErrorActionPreference', $ErrorPreference)
  & $callerModule ([ScriptBlock]::Create($source)) @scriptArguments
  if ($statusState.Succeeded) { exit 0 }
  exit 1
}

function Invoke-ApxCmdScript {
  param($Request)
  # Local and agent execution own this wrapper and its descendants through
  # the shared native process lifecycle. Keep command parsing in cmd itself.
  # The process owner creates this directory and removes it after tree termination,
  # including cancellation. Close the file before cmd opens it for batch execution.
  $path = [IO.Path]::Combine($scriptDirectory, 'script.cmd')
  $codePageCommand = [IO.Path]::Combine([Environment]::SystemDirectory, 'chcp.com')
  $preamble = '@echo off' + "`r`n" + '"' + $codePageCommand + '" 65001 >nul' + "`r`n"
  [IO.File]::WriteAllText($path, $preamble + $Request.Source, (New-Object Text.UTF8Encoding($false)))
  $info = New-Object Diagnostics.ProcessStartInfo
  $info.FileName = [IO.Path]::Combine([Environment]::SystemDirectory, 'cmd.exe')
  $info.UseShellExecute = $false
  $info.Arguments = '/d /s /c ""' + $path + '""'
  $child = [Diagnostics.Process]::Start($info)
  try {
    $child.WaitForExit()
    $exitCode = $child.ExitCode
  } finally { $child.Dispose() }
  exit $exitCode
}

# Windows PowerShell 5.1 uses .NET Framework, whose environment setter treats an
# empty string as a request to delete a variable. ScriptRequest must distinguish
# an empty value from an absent variable, so use SetEnvironmentVariableW with a
# non-null empty string for that case. The native call changes this process only.
# Compile the interop helper before caller overrides can affect compiler startup
# or temporary-file locations; requests without empty values do not need it.
if (@($payload.Environment.PSObject.Properties | Where-Object { $_.Value -eq '' }).Count -gt 0) {
  Add-Type -Namespace Apx.Execution -Name NativeEnvironment -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("kernel32.dll", EntryPoint = "SetEnvironmentVariableW",
    CharSet = System.Runtime.InteropServices.CharSet.Unicode, ExactSpelling = true, SetLastError = true)]
[return: System.Runtime.InteropServices.MarshalAs(System.Runtime.InteropServices.UnmanagedType.Bool)]
public static extern bool SetVariable(string name, string value);
'@
}
# Apply script overrides only after the wrapper has started with its own PATH.
foreach ($entry in $payload.Environment.PSObject.Properties) {
  if ($entry.Value -eq '') {
    if (-not [Apx.Execution.NativeEnvironment]::SetVariable($entry.Name, [string]::Empty)) {
      throw [ComponentModel.Win32Exception]::new([Runtime.InteropServices.Marshal]::GetLastWin32Error())
    }
  } else {
    [Environment]::SetEnvironmentVariable($entry.Name, [string]$entry.Value, 'Process')
  }
}

switch ($payload.Shell) {
  'powershell' { Invoke-ApxPowerShellScript $payload $scriptErrorActionPreference $scriptProgressPreference }
  'cmd' { Invoke-ApxCmdScript $payload }
  default { exit 1 }
}
}
