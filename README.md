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

## How To Build

> [!NOTE]
> This build process is fully supported on Linux and macOS 13 or later. For Windows users, we recommend using WSL.

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

### Step 2 - Clone the repository

```bash
git clone https://github.com/arm/performix.git
cd performix
```

### Step 3 - Bootstrap the repository with `mise`
Bootstrap the [mise](https://mise.jdx.dev/) toolchain:

```bash
./bootstrap
```

If bootstrap configured mise for the first time, start a new shell.

### Step 4 - Use `task` to build
Then use [Task](https://taskfile.dev/) to install dependencies, generate
sources, and build APX:

```bash
mise exec -- task install
```

This command assumes you accepted bootstrap's prompt to add mise to your
`PATH`. Otherwise, use the installed path directly:

```bash
"$HOME/.local/bin/mise" exec -- task install
```

## Using the Arm Performix CLI
The resulting CLI binary will be located at `core/apap-cli/apx`.

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
