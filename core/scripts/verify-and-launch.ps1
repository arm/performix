# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# verify-and-launch.ps1
# Launch apx-agent via a Scheduled Task (detached from SSH job), wait for its port file, print it.
# Registers the task under the CURRENT USER with highest privileges, non-interactive, waits for the port file, prints it.

param(
  [double]   $TimeoutSeconds        = 60.0,                    # total wait for port file
  [double]   $SleepIntervalSeconds  = 0.02,                    # polling interval
  [double]   $SigtermGraceSeconds   = 0.1,                     # grace before force kill (on error)
  [double]   $TaskLaunchTimeoutSeconds = 90.0,                 # max wait for scheduled task to actually start
  [string]   $AgentName             = "apx-agent",             # agent binary name
  [string]   $AgentFileName         = "$AgentName.exe",        # agent exe name
  [string[]] $AgentArgs             = @('start','--console-log')
)

Set-StrictMode -Version 3
$ErrorActionPreference = 'Stop'

# Global state
$global:PORTFILE     = $null
$global:AGENTPID     = $null
$global:TaskName     = $null
$global:TaskPath     = $null
$global:PortFileDir  = $null

# -------- Helpers --------

function Wait-For-TaskLaunch {
  param(
    [Parameter(Mandatory)][string]$TaskPath,
    [Parameter(Mandatory)][string]$TaskName,
    [Parameter(Mandatory)][DateTimeOffset]$StartedAfterUtc,
    [Parameter(Mandatory)][double]$TimeoutSeconds,
    [Parameter(Mandatory)][double]$SleepSeconds
  )

  $deadlineMs = [int64]([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() +
                        ($TimeoutSeconds * 1000.0))
  $sleepMs = [Math]::Max(1, [Math]::Round($SleepSeconds * 1000.0))

  while ([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() -lt $deadlineMs) {
    try {
      $task = Get-ScheduledTask -TaskPath $TaskPath -TaskName $TaskName -ErrorAction SilentlyContinue
      if ($task -and ($task.State -eq 'Running')) {
        return $true
      }
      # Task may have already completed (ran and exited quickly)
      $info = Get-ScheduledTaskInfo -TaskPath $TaskPath -TaskName $TaskName -ErrorAction SilentlyContinue
      if ($info -and $info.LastRunTime) {
        $lastRun = [DateTimeOffset]$info.LastRunTime
        if ($lastRun -gt $StartedAfterUtc.AddSeconds(-1)) {
          return $true
        }
      }
    } catch {}
    Start-Sleep -Milliseconds $sleepMs
  }

  # Timed out — cancel the task so it doesn't launch later unexpectedly
  try {
    Stop-ScheduledTask -TaskPath $TaskPath -TaskName $TaskName -ErrorAction SilentlyContinue
  } catch {}
  return $false
}

# Wait-For-PortFileInDir: watches the unique port-file directory for a .port file.
# Returns a hashtable with Pid, Port, and Path; or $null on timeout.
function Wait-For-PortFileInDir {
  param(
    [Parameter(Mandatory)][string]$Directory,
    [Parameter(Mandatory)][string]$AgentName,
    [Parameter(Mandatory)][double]$TimeoutSeconds,
    [Parameter(Mandatory)][double]$SleepSeconds
  )

  $pattern = "${AgentName}_*.port"
  $deadlineMs = [int64]([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() +
                        ($TimeoutSeconds * 1000.0))
  $sleepMs = [Math]::Max(1, [Math]::Round($SleepSeconds * 1000.0))

  while ([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() -lt $deadlineMs) {
    $files = Get-ChildItem -Path $Directory -Filter $pattern -File -ErrorAction SilentlyContinue
    foreach ($fi in $files) {
      # Only consider non-empty files (agent has finished writing)
      if ($fi.Length -le 0) { continue }

      # Extract PID from filename: <AgentName>_<pid>.port
      if ($fi.BaseName -match "^${AgentName}_(\d+)$") {
        $agentPid = [int]$Matches[1]
        $portStr = (Get-Content -LiteralPath $fi.FullName -ErrorAction SilentlyContinue | Select-Object -First 1)
        if ($portStr -and $portStr.Trim() -match '^\d+$') {
          return @{
            Pid  = $agentPid
            Port = [int]$portStr.Trim()
            Path = $fi.FullName
          }
        }
      }
    }
    Start-Sleep -Milliseconds $sleepMs
  }
  return $null
}

function Describe-Portfile {
  param([Parameter(Mandatory)][string]$Path)
  if (-not (Test-Path -LiteralPath $Path)) { return "portfile: (missing)" }
  $fi = Get-Item -LiteralPath $Path -ErrorAction SilentlyContinue
  if (-not $fi) { return "portfile: (unreadable)" }
  $mt = [DateTimeOffset]$fi.LastWriteTimeUtc
  "portfile: size={0}B, mtime(UTC)={1:O}" -f $fi.Length, $mt
}



function Kill-Agent-For-ThisPortFile {
  if (-not $global:AGENTPID) { return }
  try {
    if (Get-Process -Id $global:AGENTPID -ErrorAction SilentlyContinue) {
      Stop-Process -Id $global:AGENTPID -ErrorAction SilentlyContinue -Confirm:$false
      Start-Sleep -Milliseconds ([int][math]::Round($SigtermGraceSeconds * 1000.0))
      if (Get-Process -Id $global:AGENTPID -ErrorAction SilentlyContinue) {
        Stop-Process -Id $global:AGENTPID -Force -ErrorAction SilentlyContinue -Confirm:$false
      }
    }
  } catch {}
}

function Remove-TempTask {
  if ($global:TaskName) {
    try {
      Unregister-ScheduledTask -TaskPath $global:TaskPath -TaskName $global:TaskName `
        -Confirm:$false
    } catch {}
    $global:TaskName = $null
  }
}

function Remove-PortFileDir {
  if ($global:PortFileDir -and (Test-Path -LiteralPath $global:PortFileDir)) {
    try { Remove-Item -LiteralPath $global:PortFileDir -Recurse -Force -ErrorAction SilentlyContinue } catch {}
  }
}

function Error-And-Exit {
  param([Parameter(Mandatory)][string]$Message)
  try { Kill-Agent-For-ThisPortFile } catch {}
  try { Remove-PortFileDir } catch {}
  try { Remove-TempTask } catch {}
  Write-Error $Message
  exit 1
}

# Clean up task + agent on *any* unhandled error
trap {
  try { Kill-Agent-For-ThisPortFile } catch {}
  try { Remove-PortFileDir } catch {}
  try { Remove-TempTask } catch {}
  Write-Error ("FATAL: " + ($_.Exception.Message))
  if ($_.InvocationInfo) {
    Write-Error (" at {0}:{1}" -f $_.InvocationInfo.ScriptName,
                 $_.InvocationInfo.ScriptLineNumber)
  }
  exit 1
}

# -------- Main --------

# Resolve agent path and build arguments
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$agentPath = Join-Path -Path $scriptDir -ChildPath $AgentFileName
if (-not (Test-Path -LiteralPath $agentPath)) {
  throw "Agent executable not found: $agentPath"
}
$resolvedAgent = (Resolve-Path -LiteralPath $agentPath).ProviderPath

# Create a temporary directory for the agent port file
$global:PortFileDir = Join-Path -Path $env:TEMP -ChildPath ("${AgentName}-port-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $global:PortFileDir -Force | Out-Null

# Append --port-file-dir to agent arguments
$AgentArgs = @($AgentArgs) + @('--port-file-dir', $global:PortFileDir)

# Compose argument string for the scheduled task action; launch agent hidden via PowerShell
$escapedAgent = $resolvedAgent -replace "'","''"
$escapedArgs = $AgentArgs | ForEach-Object { "'{0}'" -f ($_ -replace "'","''") }
$agentDir = (Split-Path -Parent $resolvedAgent) -replace "'","''"
$startProcessCmd = "& { Start-Process -FilePath '$escapedAgent' -ArgumentList @($($escapedArgs -join ', ')) -WindowStyle Hidden -WorkingDirectory '$agentDir' }"
$action   = New-ScheduledTaskAction -Execute 'powershell.exe' `
             -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -Command $startProcessCmd"
$trigger  = New-ScheduledTaskTrigger -Once -At ([datetime]::Now.AddMinutes(-1))
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries `
             -DontStopIfGoingOnBatteries -StartWhenAvailable `
             -MultipleInstances IgnoreNew -Priority 1

$userId = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$principal = New-ScheduledTaskPrincipal -UserId $userId -LogonType S4U -RunLevel Highest

# Register under the CURRENT USER
$global:TaskPath = "\"
$global:TaskName = "$AgentName-once-" + [guid]::NewGuid().ToString('N')

# Register the scheduled task
Register-ScheduledTask -TaskPath $global:TaskPath -TaskName $global:TaskName `
  -Action $action -Trigger $trigger -Settings $settings -Principal $principal | Out-Null

# Start the task, then wait for the scheduler to actually launch it before timing the process
$preStartUtc = [DateTimeOffset]::UtcNow
Start-ScheduledTask -TaskPath $global:TaskPath -TaskName $global:TaskName | Out-Null

# Wait for the scheduled task to actually begin executing (handles CPU-stressed delays)
if (-not (Wait-For-TaskLaunch -TaskPath $global:TaskPath -TaskName $global:TaskName `
                              -StartedAfterUtc $preStartUtc `
                              -TimeoutSeconds $TaskLaunchTimeoutSeconds `
                              -SleepSeconds $SleepIntervalSeconds)) {
  Error-And-Exit ("scheduled task did not start within {0}s (CPU may be stressed)" -f $TaskLaunchTimeoutSeconds)
}

# Now that the task has launched, wait for a port file to appear in the unique directory
$result = Wait-For-PortFileInDir -Directory $global:PortFileDir `
                                 -AgentName $AgentName `
                                 -TimeoutSeconds $TimeoutSeconds `
                                 -SleepSeconds $SleepIntervalSeconds
if (-not $result) {
  Error-And-Exit ("port file not written in {0} within {1}s" -f $global:PortFileDir, $TimeoutSeconds)
}

$global:AGENTPID = $result.Pid
$global:PORTFILE = $result.Path

# Clean up the temporary port-file directory
try { Remove-Item -LiteralPath $global:PortFileDir -Recurse -Force -ErrorAction SilentlyContinue } catch {}

Remove-TempTask

# Print the port
Write-Output $result.Port
