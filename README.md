<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

![Arm Performix](docs/images/arm-performix-header-dark.png#gh-dark-mode-only)
![Arm Performix](docs/images/arm-performix-header-light.png#gh-light-mode-only)

# Arm Performix

Arm Performix is a performance analysis toolkit for developers building on Arm-based infrastructure. It combines target-side data collection with guided analysis, function-level insights, and desktop visualisations.

## What Does This Repository Contain?

Arm is gradually making source code for Arm Performix available to the public. Currently, you can build the Arm Performix CLI (`apx`) and use it to run the System Utilization recipe.

The repository is organised into three product components:

- [`core/`](core/) contains the CLI, engine, target-side agent, and generated
  clients.
- [`gui/`](gui/) contains the Electron desktop application.
- [`plugin-system/`](plugin-system/) contains the host-independent framework
  for visualisations and state-driven interactivity.

## How To Build

> [!NOTE]
> This build process is fully supported on Linux and macOS 13 or later. Native Windows builds are supported on x86-64
> using MSYS2 UCRT64, but Windows on Arm is not currently supported because the DuckDB dependency does not provide a
> Windows Arm64 library. Alternatively, Windows Subsystem for Linux (WSL) can be used.

Supported build platforms:

| Platform | x86-64 | Arm64 |
|----------|:------:|:-----:|
| Linux    |   ✅   |  ✅   |
| macOS    |   ✅   |  ✅   |
| Windows  |   ✅   |  ❌   |

### Step 1 - Pre-requisites

Ensure the following are available on your system:

- C/C++ compiler toolchain
- `git`
- `curl`
- `unzip`

For example, on Ubuntu or Debian:

```bash
sudo apt install build-essential git curl unzip
```

On macOS 13 or later, install the Xcode Command Line Tools (`curl` and `unzip`
are included with the operating system):

```bash
xcode-select --install
```

On x86-64 Windows, install [MSYS2](https://www.msys2.org/). Open its UCRT64
terminal to install the native prerequisites. Update MSYS2 first, closing and
reopening the terminal if requested, and repeat the update command until no
further update is required:

```bash
pacman -Syu
```

Install the required native build tools:

```bash
pacman -S --needed mingw-w64-ucrt-x86_64-gcc make git curl unzip
```

> [!IMPORTANT]
> The prebuilt DuckDB libraries currently require the emulated TLS symbols from
> MSYS2 GCC 15. MSYS2 GCC 16 will fail to link APX. Install the compatible
> GCC and runtime packages before building:

```bash
pacman -U \
  https://repo.msys2.org/mingw/ucrt64/mingw-w64-ucrt-x86_64-gcc-15.2.0-14-any.pkg.tar.zst \
  https://repo.msys2.org/mingw/ucrt64/mingw-w64-ucrt-x86_64-gcc-libs-15.2.0-14-any.pkg.tar.zst
```

If you update MSYS2 later, repeat this `pacman -U` command before rebuilding
APX until the DuckDB libraries support the current MSYS2 GCC toolchain.

Performix detects MSYS2 at its default `C:\msys64` location, and also supports
`C:\tools\msys64`. For another installation location, set `MSYS2_ROOT` to the
MSYS2 directory in PowerShell before running Performix commands:

```powershell
$env:MSYS2_ROOT = "D:\path\to\msys64"
```

The Task workflows select MSYS2 Bash and establish the UCRT64 environment
themselves. You do not need to add MSYS2, Git Bash, or WSL Bash to the front of
your Windows `PATH`.

### Step 2 - Clone the repository

```bash
git clone https://github.com/arm/performix.git
cd performix
```

### Step 3 - Bootstrap the repository with `mise`

Bootstrap the [mise](https://mise.jdx.dev/) toolchain:

On Linux or macOS:

```bash
./bootstrap
```

On x86-64 Windows, run the PowerShell bootstrap from the repository root:

```powershell
.\bootstrap.ps1
```

If bootstrap configured mise for the first time, start a new shell. On Windows,
open a new PowerShell or VS Code PowerShell terminal so that the user `PATH`
and optional mise activation added by bootstrap take effect.

### Step 4 - Use `task` to build

When building within the Arm network, first follow the [private package access
setup](DEVELOPMENT.md#package-access). It configures the credentials needed for
internal packages and release assets.

Then use [Task](https://taskfile.dev/) to install dependencies, generate
sources, and build APX. On Linux and macOS, or on Windows when you did not
enable automatic mise activation, run:

```bash
mise exec -- task install
```

This command assumes you accepted bootstrap's prompt to add mise to your
`PATH`. On Linux or macOS, otherwise use the installed path directly:

```bash
"$HOME/.local/bin/mise" exec -- task install
```

On Windows, run `task install` directly from a new PowerShell terminal if you
enabled automatic mise activation. Otherwise, invoke mise explicitly:

```powershell
task install
# Or, without automatic activation:
mise exec -- task install
# Or, if mise is not yet available on PATH:
& "$env:LOCALAPPDATA\mise\bin\mise.exe" exec -- task install
```

## Using the Arm Performix CLI

The resulting CLI binary will be located at `core/apap-cli/apx` (`apx.exe` on
Windows).

To run the System Utilization recipe, you will need an SSH-accessible Linux AArch64 or x86_64 target.
On a Linux AArch64 or x86_64 machine, you can use the local machine as the target with `--target localhost`.

Example commands using a local Linux machine:

```bash
cd core/apap-cli
./apx recipe ready system_utilization --system-wide --target localhost
./apx recipe run system_utilization --system-wide --timeout 30 --deploy-tools --target localhost
```

Example commands using a remote target:

```bash
cd core/apap-cli
./apx target add user@hostname:22:/path/to/private_key --name linux-target --default
./apx target prepare
./apx recipe ready system_utilization --system-wide --target linux-target
./apx recipe run system_utilization --system-wide --timeout 30 --deploy-tools --target linux-target
```
