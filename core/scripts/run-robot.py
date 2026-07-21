#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Orchestrator script for running Robot Framework tests.
For installation of workloads on the remote target, see download_and_prepare_workloads.py (optionally invoked by this script).

Steps performed by this script:
  1. (Optional) Remote-localhost setup: copy the repo to the target, install Go/gcc
     if missing, and build the apx CLI natively (avoids possible glibc mismatches).
     Setup is skipped automatically when the local working tree is clean and a
     stamp file on the target already matches the local git commit hash,
     indicating the binary is already up to date. Pass
     --force-remote-localhost-setup to bypass this check and always rebuild.
  2. (Optional) Workload preparation: download workload archives from GitHub and
     deploy them to the target. Requires GITHUB_TOKEN in the environment. Use
     --prepare-workloads to trigger this step (see below).
  3. Construct and invoke the `robot` command with the appropriate variables,
     exclude tags, and test-suite path.

Note on venv:
You can use `make robot-test TARGET=<name>` from the apap-cli directory to run this script, which activates the Python virtual environment automatically.
If you run this Python script directly, ensure a venv is activated that has required dependencies installed (see robot/requirements.txt).

Arguments:
  --target TARGET         Target JSON file <name> (without path or extension),
                          must match a file here: ./robot/resources/files/targets/<name>.json
                          Example value: user_192.168.1.1
  --workloads WORKLOADS   Stem of a prepared workloads JSON file located at
                          robot/resources/files/workloads/<WORKLOADS>.json.
                          This file maps workload names to their deployed paths on the
                          target, and is produced by running
                          scripts/download_and_prepare_workloads.py beforehand
                          (or automatically via --prepare-workloads).
                          Example: prepared_workloads  (reads prepared_workloads.json)
                          Optional; if omitted, workload-dependent tests will be skipped.
  --prepare-workloads NAMES
                          Download and deploy workloads to the target before running the tests.
                          Accepts an optional comma-separated list of workload names.
                          If passed without a value (--prepare-workloads alone), all workloads
                          defined in robot/resources/files/workloads/workloads.json are prepared.
                          Uses the target's SSH credentials from the target config JSON.
                          Requires GITHUB_TOKEN to be set in the environment.
                          The resulting paths are written to
                          robot/resources/files/workloads/prepared_workloads.json,
                          which is then automatically passed to Robot as --workloads.
                          Example (specific): --prepare-workloads netbench,simple-java-work
                          Example (all):      --prepare-workloads
  --launch-workload PATH  Workload command or binary path passed to Robot as the
                          LAUNCH_WORKLOAD variable. Used by tests that launch a single
                          foreground workload process (e.g. neoprof tests).
                          Overrides the default dd command. Optional.
  --include-tags TAG      Robot tag to include. May be repeated.
  --exclude-tags TAG      Additional Robot tag to exclude. May be repeated.
  --run-remote-localhost  Enable remote-localhost setup and additionally run tests
                          with the remote_localhost tag.
  --fail-fast             Stop the Robot run on the first test failure.
  --results-dir DIR       Where Robot writes output files (default: robot/results).
  --tests-dir DIR         Root directory of Robot test suites (default: robot/tests).
  --runner-os OS          Host OS used to derive exclude tags (Linux/Windows/Darwin).
                          Defaults to the runner's platform.
  --test-suite PATH       Override the test suite path(s) passed to robot. May be repeated.
                          Defaults to --tests-dir.
  --force-remote-localhost-setup
                          Force remote-localhost setup even if the target already has
                          an up-to-date build (matched by git commit hash stamp).
                          Only meaningful when --run-remote-localhost is also passed.
  --dry-run               Print all commands that would run without executing them.

Typical usage:

  # Run all tests against a remote target, including remote-localhost tests:
  python3 scripts/run-robot.py \
      --target user_192.168.1.1 \
      --launch-workload /home/user/workloads/megabench/megabench \
      --run-remote-localhost

  # Run jitdump tests: download and deploy all workloads to the target first, then run:
  GITHUB_TOKEN=<token> python3 scripts/run-robot.py \
      --target user_192.168.1.1 \
      --prepare-workloads \
      --test-suite robot/tests/tool/jitdump.robot

  # Run jitdump tests: download and deploy specific workloads only:
  GITHUB_TOKEN=<token> python3 scripts/run-robot.py \
      --target user_192.168.1.1 \
      --prepare-workloads netbench,simple-java-work \
      --test-suite robot/tests/tool/jitdump.robot

  # Use a pre-prepared workloads file (already deployed by download_and_prepare_workloads.py):
  python3 scripts/run-robot.py \
      --target user_192.168.1.1 \
      --workloads prepared_workloads \
      --test-suite robot/tests/tool/jitdump.robot

  # Run 2 specific target suites, excluding cpu_microarchitecture tag (e.g. for WoA target):
  python3 scripts/run-robot.py \
      --target woa_target \
      --launch-workload /home/user/workloads/megabench/megabench \
      --exclude-tags cpu_microarchitecture \
      --test-suite robot/tests/target/target.robot \
      --test-suite robot/tests/recipe/run.robot
"""

import argparse
import json
import os
import platform
import subprocess
import sys
import tarfile
import tempfile


SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
CORE_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, os.pardir))
REPO_ROOT = os.path.abspath(os.path.join(CORE_ROOT, os.pardir))

TARGET_CONFIG_DIR = os.path.join(CORE_ROOT, "robot", "resources", "files", "targets")
SSH_OPTS = ["-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=15"]

# Remote directory layout for the remote-localhost test setup.
REMOTE_BASE = "/tmp/apx-remote-localhost"
REMOTE_REPO = f"{REMOTE_BASE}/repo"
REMOTE_CLI_DIR = f"{REMOTE_REPO}/core/apap-cli"
# Stamp file written on the target after a successful remote-localhost setup.
# Contains the local git commit hash; used to skip rebuild when already up to date.
REMOTE_SETUP_STAMP = f"{REMOTE_CLI_DIR}/.setup-hash"

# Used for workload preparation via download_and_prepare_workloads.py:
PREPARED_WORKLOADS_STEM = "prepared_workloads"
WORKLOAD_CONFIG_DIR = os.path.join(CORE_ROOT, "robot", "resources", "files", "workloads")
WORKLOADS_CONFIG = os.path.join(WORKLOAD_CONFIG_DIR, "workloads.json")
DOWNLOAD_WORKLOADS_SCRIPT = os.path.join(SCRIPT_DIR, "download_and_prepare_workloads.py")

# Maps target arch (from target JSON) to the Go download arch identifier.
_GO_ARCH_MAP = {
    "aarch64": "arm64",
    "arm64": "arm64",
    "x86_64": "amd64",
    "amd64": "amd64",
}

def parse_args():
    parser = argparse.ArgumentParser(
        description="Run Robot Framework tests with optional remote-localhost setup."
    )

    parser.add_argument(
        "--target",
        required=True,
        help="Target JSON file name (without path or extension), e.g. user_192.168.1.1",
    )
    parser.add_argument(
        "--prepare-workloads",
        nargs="?",
        const="",
        default=None,
        metavar="NAMES",
        help=(
            "Download and deploy workloads to the target before running tests. "
            "Accepts an optional comma-separated list of workload names "
            "(e.g. --prepare-workloads netbench,simple-java-work). "
            "If passed without a value (--prepare-workloads), all workloads defined in "
            "robot/resources/files/workloads/workloads.json are prepared. "
            "Uses scripts/download_and_prepare_workloads.py with SSH credentials from the target config. "
            "Requires GITHUB_TOKEN to be set in the environment. "
            "The resulting workload paths file is written to "
            "robot/resources/files/workloads/prepared_workloads.json and automatically passed to Robot."
        ),
    )
    parser.add_argument(
        "--workloads",
        default=None,
        help="Workloads config file stem (e.g. prepared_workloads) passed to Robot as the WORKLOADS variable. "
             "Must be the name (without .json extension) of a file under robot/resources/files/workloads/ "
             "produced by scripts/download_and_prepare_workloads.py. "
             "If omitted (and --prepare-workloads is not used), workload-dependent tests will be skipped.",
    )
    parser.add_argument(
        "--launch-workload",
        default=None,
        help="Workload command or binary path passed to Robot as the LAUNCH_WORKLOAD variable.",
    )
    parser.add_argument(
        "--run-remote-localhost",
        action="store_true",
        default=False,
        help=(
            "Copy the repo to the remote target, install Go/gcc if needed, "
            "build the apx CLI natively, then include remote_localhost tests."
        ),
    )
    parser.add_argument(
        "--force-remote-localhost-setup",
        action="store_true",
        default=False,
        help=(
            "Force remote-localhost setup even if the target already has an up-to-date "
            "build (matched by git commit hash stamp). Only meaningful with --run-remote-localhost."
        ),
    )
    parser.add_argument(
        "--fail-fast",
        action="store_true",
        default=False,
        help="Pass --exitonfailure to robot so the run stops on the first failure.",
    )
    parser.add_argument(
        "--results-dir",
        default="robot/results",
        help="Directory where Robot writes its output files (default: robot/results).",
    )
    parser.add_argument(
        "--tests-dir",
        default="robot/tests",
        help="Root directory of the Robot test suites (default: robot/tests).",
    )
    parser.add_argument(
        "--runner-os",
        default=None,
        help=(
            "Host OS name used to derive exclude tags (e.g. Linux, Windows, Darwin). "
            "Defaults to the current platform detected via platform.system()."
        ),
    )
    parser.add_argument(
        "--include-tags",
        action="append",
        default=[],
        metavar="TAG",
        help="Robot tag to include. May be repeated.",
    )
    parser.add_argument(
        "--exclude-tags",
        action="append",
        default=[],
        metavar="TAG",
        help="Additional Robot tag to exclude. May be repeated.",
    )
    parser.add_argument(
        "--test-suite",
        action="append",
        default=[],
        metavar="PATH",
        help=(
            "Override the test suite path(s) passed to robot. May be repeated. "
            "Defaults to --tests-dir."
        ),
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        default=False,
        help="Print all commands that would be executed without running them.",
    )

    args = parser.parse_args()

    # Fill in runner-os default after parsing so it appears in the namespace.
    if args.runner_os is None:
        args.runner_os = platform.system()

    return args


def load_target_config(target_name):
    """Return target config from JSON file.
    Expands ``~`` in the ``key`` field so callers get a ready-to-use path.
    Exits with an error message when the config file does not exist.
    """
    path = os.path.join(TARGET_CONFIG_DIR, f"{target_name}.json")
    if not os.path.isfile(path):
        sys.exit(f"error: target config file not found: {path}")
    with open(path) as fh:
        config = json.load(fh)
    config["key"] = os.path.abspath(os.path.expanduser(config["key"]))
    return config


def run(cmd, dry_run=False, cwd=None):
    """Print and optionally execute *cmd*. Returns the process exit code, or 0 for dry-run."""
    print("Running:", " ".join(cmd), flush=True)
    if dry_run:
        return 0
    return subprocess.run(cmd, cwd=cwd).returncode


def run_remote(ssh_key, port, user_at_host, command, dry_run=False):
    """Run *command* on the remote host via SSH."""
    rc = run(["ssh", "-i", ssh_key, "-p", str(port), *SSH_OPTS, user_at_host, command], dry_run)
    if rc != 0:
        raise RuntimeError(f"SSH remote command failed (exit {rc}): {command}")


def scp_to_target(ssh_key, port, local_path, user_at_host, remote_path, dry_run=False):
    """Copy from *local_path* on runner/host to *remote_path* on remote via SCP."""
    rc = run(["scp", "-i", ssh_key, "-P", str(port), *SSH_OPTS, local_path, f"{user_at_host}:{remote_path}"], dry_run)
    if rc != 0:
        raise RuntimeError(f"SCP to target failed (exit {rc}): {local_path} -> {user_at_host}:{remote_path}")


class RemoteHost:
    """SSH connection parameters for a remote target. Holds key, port, and user@host."""

    def __init__(self, key, port, user, host, dry_run=False):
        self.key = key
        self.port = port
        self.user_at_host = f"{user}@{host}"
        self.dry_run = dry_run

    def run(self, command):
        """Run *command* on this host via SSH."""
        run_remote(self.key, self.port, self.user_at_host, command, self.dry_run)

    def scp(self, local_path, remote_path):
        """Copy *local_path* from the runner to *remote_path* on this host."""
        scp_to_target(self.key, self.port, local_path, self.user_at_host, remote_path, self.dry_run)


def setup_remote_localhost(target, dry_run=False, force=False):
    """Copy the repo to the target, install toolchain if needed, build CLI, verify.
    Raises RuntimeError if any remote command fails.

    Skips the entire setup when the remote stamp file matches the local git commit
    hash and the local working tree is clean, unless *force* is True or *dry_run*
    is True.
    """

    def _read_go_version():
        """Return the Go toolchain version declared in apap-cli/go.mod."""
        go_mod = os.path.join(CORE_ROOT, "apap-cli", "go.mod")
        with open(go_mod) as fh:
            for line in fh:
                parts = line.split()
                if len(parts) == 2 and parts[0] == "go":
                    return parts[1]
        sys.exit(f"error: could not find Go version in {go_mod}")

    def _local_git_hash():
        """Return the current HEAD commit hash, or None if git is unavailable."""
        result = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            capture_output=True,
            text=True,
            cwd=REPO_ROOT,
        )
        if result.returncode == 0:
            return result.stdout.strip()
        return None

    def _is_working_tree_dirty():
        """Return True when the working tree differs from HEAD."""
        result = subprocess.run(
            ["git", "status", "--porcelain", "--untracked-files=all"],
            capture_output=True,
            text=True,
            cwd=REPO_ROOT,
        )
        return result.returncode != 0 or bool(result.stdout.strip())

    def _remote_setup_hash():
        """Read the setup stamp file from the remote target.
        Returns the hash string if the stamp exists and is non-empty, otherwise None.
        """
        result = subprocess.run(
            [
                "ssh", "-i", remote_host.key, "-p", str(remote_host.port),
                *SSH_OPTS, remote_host.user_at_host,
                f"cat {REMOTE_SETUP_STAMP} 2>/dev/null",
            ],
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            return result.stdout.strip() or None
        return None

    def _create_repo_archive():
        """Build a filtered .tar.gz of the monorepo root and return the path to the temp file.
        The caller is responsible for deleting the file when done.
        When dry_run is True, skip creating the archive and return a placeholder path.
        """
        with tempfile.NamedTemporaryFile(suffix=".tar.gz", delete=False) as tmp:
            tmp_path = tmp.name

        if dry_run:
            print(f"[dry-run] Would create repo archive at {tmp_path}")
            return tmp_path

        print("Creating repo archive (this may take a moment)...")
        with tarfile.open(tmp_path, "w:gz") as tar:
            def _filter(tarinfo):
                name = tarinfo.name
                rel = name[2:] if name.startswith("./") else name.lstrip("/")
                # rel is "" for the root "." entry — always include.
                if not rel:
                    return tarinfo
                parts = rel.split("/")
                # Exclude node_modules, __pycache__, and Git metadata at any depth.
                if any(p in {"node_modules", "__pycache__", ".git"} for p in parts):
                    return None
                # Exclude dist variants at repo root only.
                if parts[0].startswith("dist"):
                    return None
                # Exclude core dist variants at the core root only.
                if len(parts) > 1 and parts[0] == "core" and parts[1].startswith("dist"):
                    return None
                # Exclude core/robot/results.
                if parts[:3] == ["core", "robot", "results"]:
                    return None
                # Exclude compiled bytecode.
                if rel.endswith(".pyc"):
                    return None
                # Exclude the pre-built CLI binary — it will be built natively on the target.
                if rel in {"core/apap-cli/apx", "core/apap-cli/apx.exe"}:
                    return None
                return tarinfo

            tar.add(REPO_ROOT, arcname=".", filter=_filter)
        size_mb = os.path.getsize(tmp_path) / (1024 * 1024)
        print(f"Archive created: {tmp_path} ({size_mb:.1f} MB)")
        return tmp_path

    remote_host = RemoteHost(target["key"], target["port"], target["user"], target["host"], dry_run)
    arch = target["arch"]
    go_arch = _GO_ARCH_MAP.get(arch)
    if go_arch is None:
        sys.exit(f"error: unsupported target arch for Go installation: {arch}")

    working_tree_dirty = _is_working_tree_dirty()

    if not force and not dry_run and not working_tree_dirty:
        local_hash = _local_git_hash()
        remote_hash = _remote_setup_hash()
        if local_hash and local_hash == remote_hash:
            print(f"Remote localhost setup is up to date (commit {local_hash[:12]}), skipping.")
            print("Pass --force-remote-localhost-setup to rebuild anyway.")
            return
    elif working_tree_dirty:
        print("Working tree has local changes; remote-localhost setup will not be skipped.")

    print("=== Zip repo on host, copy to target, then extract on target ===")
    tmp_archive = _create_repo_archive()
    try:
        remote_host.run(f"mkdir -p {REMOTE_BASE} && rm -rf {REMOTE_REPO} && mkdir -p {REMOTE_REPO}")
        remote_host.scp(tmp_archive, f"{REMOTE_BASE}/repo.tar.gz")
        remote_host.run(f"tar -xzf {REMOTE_BASE}/repo.tar.gz -C {REMOTE_REPO} && rm {REMOTE_BASE}/repo.tar.gz")
    finally:
        if not dry_run and os.path.exists(tmp_archive):
            os.unlink(tmp_archive)

    print("=== Installing build toolchain on target ===")

    # Detect distro and install gcc/make via the appropriate package manager.
    # Supported: Debian/Ubuntu (apt), RHEL/CentOS/Amazon Linux (dnf/yum), SLES (zypper).
    # build-essential (Debian) or the gcc+make+glibc-devel equivalents provide the
    # CGO toolchain needed by the Go build.
    remote_host.run(
        "(gcc --version > /dev/null 2>&1 && make --version > /dev/null 2>&1) || ("
        "  if command -v apt-get > /dev/null 2>&1; then"
        "    sudo apt-get update && sudo apt-get install -y build-essential;"
        "  elif command -v dnf > /dev/null 2>&1; then"
        "    sudo dnf install -y gcc gcc-c++ make glibc-devel;"
        "  elif command -v yum > /dev/null 2>&1; then"
        "    sudo yum install -y gcc gcc-c++ make glibc-devel;"
        "  elif command -v zypper > /dev/null 2>&1; then"
        "    sudo zypper install -y gcc gcc-c++ make glibc-devel;"
        "  else"
        "    echo 'error: no supported package manager found (apt-get/dnf/yum/zypper)' >&2; exit 1;"
        "  fi"
        ")"
    )

    go_version = _read_go_version()
    go_tarball = f"go{go_version}.linux-{go_arch}.tar.gz"
    go_url = f"https://go.dev/dl/{go_tarball}"
    # Install Go only when the required version is not already present
    remote_host.run(
        f"_go_ok() {{ command -v \"$1\" > /dev/null 2>&1 && \"$1\" version 2>/dev/null | grep -qF 'go{go_version} '; }}; "
        f"_go_ok go || _go_ok /usr/local/go/bin/go || "
        f"(wget -q {go_url} -O /tmp/{go_tarball} && "
        f"sudo tar -C /usr/local -xzf /tmp/{go_tarball} && "
        f"rm /tmp/{go_tarball})"
    )

    go_env = f"export PATH=$PATH:/usr/local/go/bin"

    print("=== Marking pre-bundled tools as fetched on target ===")
    remote_tools_dir = f"{REMOTE_CLI_DIR}/tools"
    remote_host.run(
        f"test -d {remote_tools_dir} || "
        f"(echo 'error: expected pre-bundled tools at {remote_tools_dir}' >&2; exit 1)"
    )
    remote_host.run(f"touch {remote_tools_dir}/.fetched")

    print("=== Building apx CLI natively on target (using make) ===")
    remote_host.run(f"{go_env} && cd {REMOTE_CLI_DIR} && make build")

    # Make the CLI directory world-writable so that non-privileged users can create
    # the config and log directories they need at runtime.
    print("=== Setting permissions on remote CLI directory ===")
    remote_host.run(f"chmod -R o+rwX {REMOTE_CLI_DIR}")

    print("=== Verify apx binary is present and executable ===")
    remote_host.run(f"test -x {REMOTE_CLI_DIR}/apx")

    print("=== Writing setup stamp on target ===")
    local_hash = _local_git_hash()
    if local_hash:
        setup_stamp = f"dirty:{local_hash}" if working_tree_dirty else local_hash
        remote_host.run(f"echo '{setup_stamp}' > {REMOTE_SETUP_STAMP}")
    else:
        print("WARNING: could not determine local git hash; setup stamp not written. Remote-localhost setup will not be skipped on future runs.")

    print("Remote localhost setup complete.")

def all_workload_names():
    """Return a comma-separated string of all workload names defined in workloads.json."""
    if not os.path.isfile(WORKLOADS_CONFIG):
        sys.exit(f"error: workloads config not found: {WORKLOADS_CONFIG}")
    with open(WORKLOADS_CONFIG) as fh:
        config = json.load(fh)
    return ",".join(config.keys())

def prepare_workloads(names, target, dry_run=False):
    """Download and deploy workloads to the target via download_and_prepare_workloads.py.

    Writes the resulting workload paths JSON to
    robot/resources/files/workloads/prepared_workloads.json and returns the
    stem ("prepared_workloads") for passing to Robot as the WORKLOADS variable.
    """

    output_path = os.path.join(WORKLOAD_CONFIG_DIR, f"{PREPARED_WORKLOADS_STEM}.json")
    cmd = [
        sys.executable,
        DOWNLOAD_WORKLOADS_SCRIPT,
        "--workloads", names,
        "--target-user", target["user"],
        "--target-host", target["host"],
        "--target-port", str(target["port"]),
        "--ssh-key", target["key"],
        "--output", output_path,
    ]
    rc = run(cmd, dry_run=dry_run)
    if rc != 0:
        sys.exit(f"error: workload preparation failed (exit {rc})")
    return PREPARED_WORKLOADS_STEM


def build_robot_command(args, target):
    """Return the robot CLI command as a list of strings.

    Exclude tags are composed as a single OR-joined expression mirroring the
    convention already used in the CI workflows:
      disabledORdisabled-{os}[ORremote_localhost][OR<extra>...]
    """
    os_lower = args.runner_os.lower()
    exclude_expr = f"disabledORdisabled-{os_lower}"

    if not args.run_remote_localhost:
        exclude_expr += "ORremote_localhost"

    for tag in args.exclude_tags:
        exclude_expr += f"OR{tag}"

    results_dir = os.path.join(CORE_ROOT, args.results_dir)
    robot_cwd = os.path.join(CORE_ROOT, "robot")

    cmd = [
        "robot",
        "-T",
        "--loglevel", "DEBUG",
        "--outputdir", results_dir,
        "--variable", f"TARGET:{target}",
        "--variable", "EXPORT_RUNS:True",
    ]

    for tag in args.include_tags:
        cmd += ["--include", tag]

    cmd += ["--exclude", exclude_expr]

    if args.workloads is not None:
        cmd += ["--variable", f"WORKLOADS:{args.workloads}"]

    if args.launch_workload is not None:
        cmd += ["--variable", f"LAUNCH_WORKLOAD:{args.launch_workload}"]

    if args.run_remote_localhost:
        cmd += ["--variable", f"REMOTE_LOCALHOST_DIR:{REMOTE_CLI_DIR}"]

    if args.fail_fast:
        cmd.append("--exitonfailure")

    if args.test_suite:
        for suite in args.test_suite:
            cmd.append(os.path.join(CORE_ROOT, suite))
    else:
        cmd.append(os.path.join(CORE_ROOT, args.tests_dir))

    return cmd, robot_cwd


def main():
    print("=== Starting Robot Framework test run ===")
    args = parse_args()
    try:
        run(["robot", "--version"])
    except FileNotFoundError:
        print("WARNING: 'robot' not found in PATH: is your virtual environment activated?", flush=True)

    target = load_target_config(args.target)

    if args.run_remote_localhost:
        try:
            setup_remote_localhost(target, dry_run=args.dry_run, force=args.force_remote_localhost_setup)
        except RuntimeError as exc:
            sys.exit(f"error: remote localhost setup failed: {exc}")

    if args.prepare_workloads is not None:
        names = args.prepare_workloads or all_workload_names()
        print("=== Preparing workloads on target ===" + ("" if args.prepare_workloads else " (all workloads)"))
        args.workloads = prepare_workloads(names, target, dry_run=args.dry_run)

    robot_cmd, robot_cwd = build_robot_command(args, args.target)
    return_code = run(robot_cmd, dry_run=args.dry_run, cwd=robot_cwd)
    sys.exit(return_code)


if __name__ == "__main__":
    main()
