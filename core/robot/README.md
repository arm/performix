<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Robot Framework

This directory contains the Robot Framework functional test suite for the Arm Performix CLI. The tests exercise the CLI end-to-end against a real target device over SSH, covering recipe execution, tool integrations, target management, and installation. They are intended to complement the unit tests and catch integration-level regressions that unit tests cannot detect.

## Directory structure

The `robot` directory consists of the following:
* `resources` - Various resources required by Robot, such as scripts, config files, binaries, libraries, and most importantly keyword resources
* `results` - Directory for storing output files from test runs (not required but helps keep things tidy)
* `tests` - Test suites that contain the Robot tests, organised using subdirectories to group related tests together
* Everything else are miscellaneous files relating to Git, linting, continuous integration, or configuring the virtual environment.

## Specifying a target

Tests require a target device to run against. The target is identified by a JSON config file stored at `robot/resources/files/targets/<name>.json`.

Create this file before running the tests. Here is an example JSON schema, note that you will need to replace the values with real ones appropriate to your target:

```json
{
    "name": "i-04413d15ac20a7521",
    "host": "192.168.1.1",
    "port": 22,
    "user": "robot01",
    "key": "/Users/robot01/.ssh/id_robot_key",
    "arch": "aarch64",
    "os": "Linux"
}
```

The `<name>` of this file (without the `.json` extension) is the value you pass as the `TARGET` argument when running tests.

## Building the Arm Performix CLI binary

The `apx` binary must be built before running the tests. See root-level [README.md](../README.md) for guidance.

## Running the tests

The recommended way to run the Robot tests is via `task core:test:robot` from
the repository root, or alternatively `make robot-test` from the `apap-cli`
directory. Both methods handle virtual environment setup and dependency
installation automatically.

### MCP client installation

The MCP client-installation suite is host-only and does not require a profiling
target. Its default `mock` mode runs the same human-readable and JSON status and
doctor checks before and after the install, idempotent install and uninstall
lifecycle for every supported client. It also covers `install --all`,
`uninstall --all`, conflicts, unreadable configuration and undetected clients.
The suite creates temporary application evidence and configuration files, and
uses minimal Claude Code, Codex and VS Code command stubs built by the Task
workflow for black-box testing.

From the repository root, run:

```shell
task core:build:apx
task core:test:robot TARGET=robot_localhost \
  ROBOT_ARGS="--test-suite robot/tests/mcp/client_installation.robot"
```

The opt-in `real` mode runs the same cases against installed clients while
retaining temporary configuration directories:

```shell
MCP_CLIENT_TEST_MODE=real task core:test:robot TARGET=robot_localhost \
  ROBOT_ARGS="--test-suite robot/tests/mcp/client_installation.robot"
```

Real-mode cases are skipped when the client is absent or its configuration
cannot be isolated safely on the host. Mock mode is the deterministic mode for
normal CI.

### Using `task core:test:robot` (recommended)

The `TARGET` argument is mandatory and must match the name of a config file in `robot/resources/files/targets/`:

```shell
# Run all tests against a target
task core:test:robot TARGET=my_device
```

To exclude specific tags, use `ROBOT_ARGS`:

```shell
# Exclude recipes not supported on x86-64 targets
task core:test:robot TARGET=my_device ROBOT_ARGS="--exclude-tags cpu_microarchitectureORinstruction_mixORmemory_access"

# Exclude multiple individual tags
task core:test:robot TARGET=my_device ROBOT_ARGS="--exclude-tags cpu_microarchitecture --exclude-tags instruction_mix"
```

Tests tagged `remote-localhost` will be skipped by default, because they require the CLI to be built and deployed on the remote target first and this takes a little extra time to set up. Pass `--run-remote-localhost` to enable automatic remote localhost setup and include those tests:

```shell
task core:test:robot TARGET=my_device ROBOT_ARGS="--run-remote-localhost --launch-workload /path/to/workload"
```

### Using `make robot-test` (alternative)

Run from the `apap-cli` directory. `TARGET` is mandatory and must match the name of a config file in `robot/resources/files/targets/`:

```shell
# Run all tests against a target (from the apap-cli directory)
make robot-test TARGET=my_device
```

To exclude specific tags or enable `remote-localhost` tests, use `ROBOT_ARGS` in the same way as shown above for `task core:test:robot`:

```shell
make robot-test TARGET=my_device ROBOT_ARGS="--exclude-tags cpu_microarchitecture"
make robot-test TARGET=my_device ROBOT_ARGS="--run-remote-localhost --launch-workload /path/to/workload"
```

For the full list of supported arguments, run:

```shell
python3 scripts/run-robot.py --help
```

### Running `robot` directly

If you need to invoke `robot` directly (e.g. for dry runs or targeting a single suite), you will need a Python virtual environment with `robot/requirements.txt` installed. You can reuse the one created automatically by `make robot-test` (located at `env/` in the repository root), or create your own:

```shell
python3 -m venv path/to/venv
source path/to/venv/bin/activate
python3 -m pip install -r robot/requirements.txt
```

The target is passed as a Robot variable:

```shell
robot -T --outputdir robot/results --exclude disabled --variable TARGET:<name> robot/tests
```

Note that `remote-localhost`-tagged tests will be skipped automatically if the remote localhost setup has not been performed (i.e. `apx` is not present on the target at `/tmp/apx-remote-localhost/repo/core/apap-cli/apx`). Use `task core:test:robot` or `make robot-test` with `--run-remote-localhost` to perform that setup.

#### Example commands

| Goal | Command |
| ---- | ------- |
| Run all tests, excluding disabled | `robot -T --outputdir robot/results --exclude disabled --variable TARGET:<name> robot/tests` |
| x86-64 target (exclude unsupported recipes) | `robot -T --outputdir robot/results --exclude disabledORcpu_microarchitectureORinstruction_mixORmemory_access --variable TARGET:<name> robot/tests` |
| Only run recipe tests | `robot -T --outputdir robot/results --include recipe --exclude disabled --variable TARGET:<name> robot/tests` |
| Only run target and run tests | `robot -T --outputdir robot/results --include targetORrun --exclude disabled --variable TARGET:<name> robot/tests` |
| Skip `remote-localhost` tests (note: `remote-localhost` tests require special setup on the target before they are run, this can be handled `./scripts/run-robot.py` with the `--run-remote-localhost` flag) | `robot -T --outputdir robot/results --exclude disabledORremote-localhost --variable TARGET:<name> robot/tests` |
| Dry run a single suite (no execution) | `robot --dryrun --output NONE --log NONE --report NONE robot/tests/recipe/recipe.robot` |
| Run without producing output files | `robot --output NONE --log NONE --report NONE --variable TARGET:<name> robot/tests` |

## Continuous integration sharding

The extended validation workflow runs independent parts of the Robot suite in parallel. Sharding is CI orchestration only; local `task core:test:robot` and `make robot-test` invocations remain single Robot runs unless you select suites or tags explicitly.

The main [Robot Framework workflow](../../.github/workflows/robot-framework.yaml) uses two partition shapes to balance run times. Both shapes cover all eligible ordinary tests:

| Host runner and target | Ordinary test partition | Additional coverage |
| ---------------------- | ----------------------- | ------------------- |
| Linux Arm64 host and provisioned Linux Arm64 target | `recipe` + `tool` + `non-recipe-non-tool` | Dedicated `remote-localhost` shard |
| Linux x86-64 host and provisioned Linux Arm64 target | `recipe` + `non-recipe` | None |
| Windows x86-64 host and provisioned Linux Arm64 target | `recipe` + `non-recipe` | None |
| Linux Arm64 host and static Raspberry Pi target | Selected static subset | Manual workflow dispatches only |

The ordinary suite assignments in each partition are exhaustive:

- The `arm64` partition runs `robot/tests/recipe`, `robot/tests/tool`, and every other top-level suite in separate shards. It also runs the dedicated `remote-localhost` shard.
- The `amd64` partition runs `robot/tests/recipe` separately; `non-recipe` includes every other top-level suite.

[`.github/robot-sharding.json`](../../.github/robot-sharding.json) is the source of truth for shard partitions, host assignments, workload preparation, and static targets. Each provisioned-target shard lists its top-level test suites and whether it prepares workloads. Static targets list their selected suite files separately.

Add every new top-level `robot/tests` directory to exactly one shard in each partition. Matrix generation fails before target provisioning if a suite is missing or duplicated.

The separate [x86 target workflow](../../.github/workflows/robot-framework-x86.yaml) and [Windows on Arm workflow](../../.github/workflows/robot-framework-woa.yaml) remain single Robot jobs with platform-specific suite and tag exclusions.

### Isolation requirements

Each concurrent provisioned shard has its own runner and target. The workflow derives a unique artifact suffix from each matrix row. Robot results, engine logs, and exported runs use this suffix so results from parallel jobs do not collide.

## Workload-dependent tests (skipped by default if not set up)

Some test suites (e.g. `jitdump.robot`) require workloads to be pre-installed on the target. The workloads are defined in `robot/resources/files/workloads/workloads.json` and downloaded as GitHub release assets from the `Arm-Debug/performix-workloads` repo. Requires `GITHUB_TOKEN` in the environment (a GitHub personal access token with read access to the `Arm-Debug` organisation).

If workloads are not prepared, workload-dependent tests skip automatically with an informational message.

### Setting up and running workload-dependent tests

Pass `--prepare-workloads` to download and deploy workloads to the target before running the tests. Used without a value it prepares all workloads defined in `robot/resources/files/workloads/workloads.json`. Pass a comma-separated list to prepare specific workloads only:

```shell
# Using task — prepare all workloads
GITHUB_TOKEN=<token> task core:test:robot TARGET=my_device ROBOT_ARGS="--prepare-workloads"

# Using task — prepare specific workloads only
GITHUB_TOKEN=<token> task core:test:robot TARGET=my_device \
    ROBOT_ARGS="--prepare-workloads netbench,simple-java-work,javabench"

# Using make (from the apap-cli directory)
GITHUB_TOKEN=<token> make robot-test TARGET=my_device ROBOT_ARGS="--prepare-workloads"
```

### Manual workload setup (alternative)

You can also prepare workloads separately using `scripts/download_and_prepare_workloads.py`, then pass the result to the test run via `--workloads`. This is useful when workloads are already deployed on the target.

```shell
# Step 1: download and deploy workloads, write paths to a JSON file
GITHUB_TOKEN=<token> python3 scripts/download_and_prepare_workloads.py \
    --workloads netbench,simple-java-work \
    --target-user myuser \
    --target-host 192.168.1.1 \
    --ssh-key ~/.ssh/id_rsa \
    --output robot/resources/files/workloads/prepared_workloads.json

# Step 2: run the tests, passing the stem of the output file
task core:test:robot TARGET=my_device \
    ROBOT_ARGS="--workloads prepared_workloads"
```

## Running the Robocop linter
Robocop is a linter for Robot Framework. The linter will automatically run in CI, but it can also be run manually (e.g. before pushing code for review).

Make sure you are in the root directory of the git repository. The Robocop configuration file to use is `robocop.toml` which exists in the `robot` directory.

To run the linter manually:
```
python -m robocop check --config robot/robocop.toml
```

## Generate test coverage report
To gather a list of all our robot tests and generate a report:

```
python -m robot.testdoc --name "Arm Performix - Functional Test Coverage" --exclude disabled robot/tests/* robot/results/coverage.html
```
