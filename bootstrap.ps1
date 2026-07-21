# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

$ErrorActionPreference = "Stop"

# Interactive first-run setup for the Performix developer toolchain. Run this
# from a fresh checkout to install mise if needed, optionally configure
# PowerShell, and install the tool versions pinned by mise.toml.

# Bootstrap is intentionally an interactive first-run script. Keeping it
# option-free avoids a second, less-tested non-interactive control path.
if ($args.Count -gt 0) {
    throw "Unknown option: $($args[0])"
}

$MiseBootstrapVersion = "2026.7.5"
$MiseBootstrapTag = "v$MiseBootstrapVersion"
$ProjectRoot = $PSScriptRoot
$DefaultMiseInstallPath = Join-Path $env:LOCALAPPDATA "mise\bin\mise.exe"
$MiseInstallPath = if ($env:MISE_INSTALL_PATH) { $env:MISE_INSTALL_PATH } else { $DefaultMiseInstallPath }
$JustInstalledMise = $false
$MiseNeedsPathSetup = $false
$PathSetupAccepted = $false
$ActivatedShellThisRun = $false
$UseColour = -not [Console]::IsOutputRedirected -and -not $env:NO_COLOR

function Write-Banner($Message) {
    if ($UseColour) {
        Write-Host "⚙️  " -ForegroundColor Magenta -NoNewline
        Write-Host $Message -ForegroundColor White
    } else {
        Write-Host $Message
    }
}

function Write-Info($Message) {
    if ($UseColour) {
        Write-Host "→ " -ForegroundColor Cyan -NoNewline
        Write-Host $Message
    } else {
        Write-Host $Message
    }
}

function Write-Success($Message) {
    if ($UseColour) {
        Write-Host "✓ " -ForegroundColor Green -NoNewline
        Write-Host $Message
    } else {
        Write-Host $Message
    }
}

function Write-Question($Message) {
    if ($UseColour) {
        Write-Host "Q: " -ForegroundColor Cyan -NoNewline
        Write-Host $Message
    } else {
        Write-Host "Q: $Message"
    }
}

function Write-Prompt($Message) {
    if ($UseColour) {
        Write-Host $Message -NoNewline
    } else {
        Write-Host $Message -NoNewline
    }
}

function Get-PlatformAsset {
    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { "x64" }
        "ARM64" { "arm64" }
        default { throw "Unsupported architecture for automatic mise bootstrap: $env:PROCESSOR_ARCHITECTURE" }
    }

    return "mise-v$MiseBootstrapVersion-windows-$arch.exe"
}

function Get-ExpectedSha256($Asset) {
    switch ($Asset) {
        "mise-v2026.7.5-windows-arm64.exe" { "27d3279d9d6a994d910561706f5ca99abcd8a03d38ad15b73ff0b5b3e148e4ea" }
        "mise-v2026.7.5-windows-x64.exe" { "1840f167ec8b161598e08b8ede769cf9954c0239b25bb7bdf0b326124b548c32" }
        default { throw "No checksum recorded for $Asset" }
    }
}

function Install-Mise($Destination) {
    $asset = Get-PlatformAsset
    $expected = Get-ExpectedSha256 $asset
    $url = "https://github.com/jdx/mise/releases/download/$MiseBootstrapTag/$asset"
    $tempPath = "$Destination.tmp"

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Destination) | Out-Null
    Write-Info "Downloading mise $MiseBootstrapVersion from GitHub Releases..."
    # PowerShell's progress renderer can dominate output for a single binary
    # download, so suppress it while keeping download errors visible.
    $previousProgressPreference = $ProgressPreference
    $ProgressPreference = "SilentlyContinue"
    try {
        Invoke-WebRequest -Uri $url -OutFile $tempPath
    } finally {
        $ProgressPreference = $previousProgressPreference
    }

    $actual = (Get-FileHash -Algorithm SHA256 -Path $tempPath).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        Remove-Item -Force $tempPath
        throw "Checksum mismatch for $asset. Expected $expected, got $actual."
    }

    Move-Item -Force $tempPath $Destination
}

function Get-PathMise {
    $command = Get-Command mise -ErrorAction SilentlyContinue
    if ($null -eq $command) {
        return $null
    }

    return $command.Source
}

function Select-Mise {
    $pathMise = Get-PathMise
    if ($pathMise) {
        # A mise already on PATH is treated as user-managed. Use it directly and
        # leave the user's PowerShell profile unchanged.
        return $pathMise
    }

    if (Test-Path -PathType Leaf $MiseInstallPath) {
        # A user-local mise exists but future shells may not see it yet. Prompt
        # for PATH and activation setup after selecting it.
        $script:MiseNeedsPathSetup = $true
        return $MiseInstallPath
    }

    Install-Mise $MiseInstallPath
    $script:JustInstalledMise = $true
    $script:MiseNeedsPathSetup = $true
    return $MiseInstallPath
}

function Add-SelectedMiseToProcessPath {
    # The current bootstrap process must use the same mise that was selected for
    # installation, even before the user's shell profile has been reloaded.
    $binDir = Split-Path -Parent $MiseBin
    $parts = @()
    if ($env:PATH) {
        $parts = $env:PATH -split ';' | Where-Object { $_ }
    }

    if ($parts | Where-Object { $_.TrimEnd('\') -ieq $binDir.TrimEnd('\') }) {
        return
    }

    $env:PATH = if ($env:PATH) { "$binDir;$env:PATH" } else { $binDir }
}

function Confirm-Yes($Prompt) {
    if (-not [Environment]::UserInteractive) {
        return $false
    }

    Write-Prompt "$Prompt [Y/n] "
    $answer = Read-Host
    return $answer -notin @("n", "N", "no", "NO", "No")
}

function Add-MiseToPath {
    $binDir = Split-Path -Parent $MiseInstallPath
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @()
    if ($userPath) {
        $parts = $userPath -split ';' | Where-Object { $_ }
    }

    if ($parts | Where-Object { $_.TrimEnd('\') -ieq $binDir.TrimEnd('\') }) {
        Write-Info "User PATH already contains $binDir"
        return
    }

    # Prepend the managed mise directory so an older user PATH entry cannot win
    # in the next PowerShell session.
    $newPath = if ($userPath) { "$binDir;$userPath" } else { $binDir }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Success "Added $binDir to the user PATH. Restart your shell to use it."
}

function Get-PathCommand {
    $binDir = Split-Path -Parent $MiseInstallPath
    return "`$env:PATH = `"$binDir;`$env:PATH`""
}

function Add-ManagedBlock($Path, $Name, $Content) {
    # Managed markers make bootstrap idempotent without trying to parse or merge
    # a developer's existing PowerShell profile.
    $start = "# >>> Performix $Name >>>"
    $end = "# <<< Performix $Name <<<"

    $parent = Split-Path -Parent $Path
    if ($parent) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    if (-not (Test-Path $Path)) {
        New-Item -ItemType File -Path $Path | Out-Null
    }

    $existing = Get-Content -Raw -Path $Path
    if ($existing.Contains($start)) {
        Write-Info "PowerShell profile already contains Performix $Name block: $Path"
        return
    }

    Add-Content -Path $Path -Value ""
    Add-Content -Path $Path -Value $start
    Add-Content -Path $Path -Value $Content
    Add-Content -Path $Path -Value $end
    Write-Success "Updated $Path with Performix $Name block"
}

function Add-MiseActivation {
    $profilePath = $PROFILE.CurrentUserCurrentHost
    $content = "(& `"$MiseInstallPath`" activate pwsh) | Out-String | Invoke-Expression"
    Add-ManagedBlock $profilePath "mise activation" $content
    $script:ActivatedShellThisRun = $true
}

function Get-MiseActivationCommand {
    return "(& `"$MiseInstallPath`" activate pwsh) | Out-String | Invoke-Expression"
}

function Test-MiseActivated {
    return -not [string]::IsNullOrEmpty($env:MISE_SHELL)
}

function Write-Intro {
    Write-Banner "Bootstrapping Performix toolchain"
    Write-Host "This will download required tools/versions and optionally configure your shell."
    Write-Host ""
}

Write-Intro
$MiseBin = Select-Mise
Add-SelectedMiseToProcessPath

if ($JustInstalledMise) {
    Write-Success "mise was installed to $MiseInstallPath."
} else {
    Write-Info "Using existing mise: $MiseBin"
}

if ($MiseNeedsPathSetup) {
    $miseBinDir = Split-Path -Parent $MiseInstallPath
    Write-Host ""
    Write-Question "Do you want to add $miseBinDir to your user PATH?"
    Write-Host "This lets you run 'mise' directly from PowerShell (recommended)."
    if (Confirm-Yes "Add mise to PATH?") {
        Add-MiseToPath
        $PathSetupAccepted = $true
    }
}

if ($MiseNeedsPathSetup) {
    Write-Host ""
    Write-Question "Do you want mise to activate the Performix toolchain automatically?"
    Write-Host "This makes 'task', 'go', 'node', 'npm', and 'protoc' use the versions pinned by this checkout when PowerShell is in the Performix repo (recommended)."
    Write-Host "Choose no if you prefer to manually invoke tools via 'mise exec --'."
    if (Confirm-Yes "Enable automatic toolchain activation?") {
        Add-MiseActivation
    }
}

Write-Host ""
Write-Info "Installing pinned tool versions with mise..."
# Trusting the checked-in mise.toml avoids an interactive trust prompt on a
# fresh clone while keeping mise's normal trust model for other directories.
& $MiseBin trust --yes (Join-Path $ProjectRoot "mise.toml")
& $MiseBin install -C $ProjectRoot

Write-Success "Performix bootstrap completed."
if (Test-MiseActivated) {
    Write-Info "Next: run 'task install' when you are ready to install dependencies, generate code, and build."
} elseif ($ActivatedShellThisRun) {
    Write-Info "Next: run this command to activate mise in this shell, then run 'task install':"
    if ($UseColour) {
        Write-Host "  $(Get-MiseActivationCommand)" -ForegroundColor White
    } else {
        Write-Host "  $(Get-MiseActivationCommand)"
    }
} elseif ($PathSetupAccepted) {
    Write-Info "Next: run this command to make mise available in this PowerShell session, then run 'mise exec -- task install':"
    if ($UseColour) {
        Write-Host "  $(Get-PathCommand)" -ForegroundColor White
    } else {
        Write-Host "  $(Get-PathCommand)"
    }
} elseif ($MiseNeedsPathSetup) {
    Write-Info "Next: run '& `"$MiseBin`" exec -- task install' when you are ready to install dependencies, generate code, and build."
} else {
    Write-Info "Next: run 'mise exec -- task install' when you are ready to install dependencies, generate code, and build."
}
