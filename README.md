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
> This build process is fully supported on Linux and MacOS systems. For Windows users, we recommend using WSL.

### Step 1 - Pre-requisites
Ensure the following are available on your system:
- C/C++ compiler toolchain
- `curl`
- `unzip`

For example, on Ubuntu or Debian:

```bash
sudo apt install build-essential curl unzip
```

### Step 2 - Bootstrap the repository with `mise`
This step requires an internet connection to download tools.

Bootstrap the [mise](https://mise.jdx.dev/) toolchain:

```bash
./bootstrap
```

If bootstrap configured mise for the first time, start a new shell.

### Step 3 - Use `task` to build
Then use [Task](https://taskfile.dev/) to install dependencies, generate
sources, and build APX:

```bash
mise exec -- task install
```

## Using the Arm Performix CLI
The resulting CLI binary will be located at `core/apap-cli/apx`.

To run the System Utilization recipe, you will need an SSH-accessible Linux AArch64 or x86_64 target.
Alternatively, you can run the recipe with `--target localhost` on a Linux AArch64 or x86_64 machine.

Example commands using the local machine:

```bash
cd core/apap-cli
./apx recipe run system_utilization --system-wide --timeout 30 --deploy-tools --target localhost
```

Example commands using a remote target:

```bash
cd core/apap-cli
./apx target add user@hostname:22:/path/to/private_key --name linux-target --default
./apx target prepare
./apx recipe run system_utilization --system-wide --timeout 30 --deploy-tools --target linux-target
```

