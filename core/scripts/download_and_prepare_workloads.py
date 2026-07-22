#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Download and deploy one or more workloads defined in
robot/resources/files/workloads/workloads.json.

Each workload is deployed to:
  /home/<target-user>/robot-workloads/<workload>/<release>/

The script accepts a comma-separated list of workload names, prepares each one
on the target, and writes out a JSON mapping of workloads to their paths.
"""

import argparse
import json
import os
import posixpath
import shutil
import subprocess
import sys
import tempfile


SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, os.pardir))
WORKLOADS_CONFIG = os.path.join(REPO_ROOT, "robot", "resources", "files", "workloads", "workloads.json")
DOWNLOAD_ASSET_SCRIPT = os.path.join(SCRIPT_DIR, "download_github_release_asset.py")
SSH_OPTS = ["-o", "StrictHostKeyChecking=no", "-o", "ConnectTimeout=15"]
DEFAULT_OUTPUT = "workload_paths.json"

def run(cmd):
    print("Running:", " ".join(cmd))
    subprocess.run(cmd, check=True)


def run_remote(ssh_key, port, target, remote_command):
    try:
        run(["ssh", "-i", ssh_key, "-p", str(port), *SSH_OPTS, target, remote_command])
    except subprocess.CalledProcessError:
        raise RuntimeError(f"SSH remote command failed: {remote_command}") from None


def test_ssh_connection(ssh_key, port, target):
    if not os.path.isfile(ssh_key):
        sys.exit(f"SSH key not found at {ssh_key}")
    try:
        run_remote(ssh_key, port, target, "true")
    except RuntimeError:
        sys.exit(
            "Unable to establish SSH connection to target. "
            "Please verify your SSH credentials and network connection."
        )


def load_config(path):
    if not os.path.isfile(path):
        sys.exit(f"Workloads config not found at {path}")
    with open(path, "r") as handle:
        return json.load(handle)


def parse_workloads(raw):
    if raw is None:
        return []
    parts = [part.strip() for part in raw.split(",") if part.strip()]
    unique = []
    for name in parts:
        if name not in unique:
            unique.append(name)
    return unique


def download_asset(repo, release, asset, out_dir):
    if not os.path.isfile(DOWNLOAD_ASSET_SCRIPT):
        sys.exit(f"Required script not found: {DOWNLOAD_ASSET_SCRIPT}")
    cmd = [sys.executable, DOWNLOAD_ASSET_SCRIPT, "--repo", repo, "--tag", release, "--asset-name", asset, "--out", out_dir]
    run(cmd)
    asset_path = os.path.join(out_dir, asset)
    if not os.path.isfile(asset_path):
        sys.exit(f"Workload asset not found at {asset_path}")
    return asset_path


def deploy_to_target(asset_path, remote_dir, target, ssh_key, port):
    remote_asset = posixpath.join("/tmp", os.path.basename(asset_path))
    run_remote(ssh_key, port, target, f'mkdir -p "{remote_dir}"')
    run(["scp", "-i", ssh_key, "-P", str(port), *SSH_OPTS, asset_path, f"{target}:{remote_asset}"])
    run_remote(ssh_key, port, target, f'tar -xzf "{remote_asset}" -C "{remote_dir}" && rm -f "{remote_asset}"')


def prepare_single_workload(name, config, target_user, target, ssh_key, port, archive_dir):
    if name not in config:
        sys.exit(f"Workload '{name}' not found in {WORKLOADS_CONFIG}")

    details = config[name]
    repo = details["repo"]
    release = details["release"]
    asset = details["asset"]
    relative_path = details["relative_workload_path"]

    remote_dir = posixpath.join("/home", target_user, "robot-workloads", name, release)
    workload_path = posixpath.join(remote_dir, relative_path)

    # Skip download and deployment if the workload is already present on the target.
    # The release version is encoded in remote_dir, so a version bump produces a new
    # path and will not be incorrectly skipped.
    check = subprocess.run(
        ["ssh", "-i", ssh_key, "-p", str(port), *SSH_OPTS, target, f"test -e '{workload_path}'"],
        capture_output=True,
    )
    if check.returncode == 0:
        print(f"Workload '{name}' already present at {workload_path}, skipping.")
        return {"workload_path": workload_path}

    asset_path = download_asset(repo, release, asset, archive_dir)
    deploy_to_target(asset_path, remote_dir, target, ssh_key, port)
    return {"workload_path": workload_path}


def build_workload_payload(workloads, config, target_user, target, ssh_key, port):
    archive_dir = tempfile.mkdtemp(prefix="workloads-")
    payload = {}
    try:
        for workload in workloads:
            payload[workload] = prepare_single_workload(workload, config, target_user, target, ssh_key, port, archive_dir)
    finally:
        shutil.rmtree(archive_dir, ignore_errors=True)

    return payload


def write_output(payload, output_path):
    normalized = os.path.abspath(output_path)
    os.makedirs(os.path.dirname(normalized) or ".", exist_ok=True)
    with open(normalized, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, separators=(",", ":"))
        handle.write("\n")
    return normalized


def parse_args():
    parser = argparse.ArgumentParser(description="Download and prepare workloads on the target.")
    parser.add_argument("--workloads", help="Comma-separated list of workloads to prepare.")
    parser.add_argument("--target-user", required=True, help="SSH user for the target.")
    parser.add_argument("--target-host", required=True, help="Target IP or host.")
    parser.add_argument("--target-port", type=int, default=22, help="SSH port for the target (default: 22).")
    parser.add_argument("--ssh-key", required=True, help="Path to the SSH private key.")
    parser.add_argument("--output", default=DEFAULT_OUTPUT, help=f"Output path to the workload paths JSON.")
    parser.add_argument("--config", default=WORKLOADS_CONFIG, help=f"Path to workloads config JSON.")
    return parser.parse_args()


def main():
    args = parse_args()
    workloads = parse_workloads(args.workloads)

    payload = {}
    if workloads:
        config = load_config(args.config)
        ssh_key = os.path.abspath(os.path.expanduser(args.ssh_key))
        port = args.target_port
        target = f"{args.target_user}@{args.target_host}"
        test_ssh_connection(ssh_key, port, target)
        print("Preparing workloads:", ", ".join(workloads))
        payload = build_workload_payload(workloads, config, args.target_user, target, ssh_key, port)

    output_path = write_output(payload, args.output)
    print(f"Wrote workload metadata to {output_path}")


if __name__ == "__main__":
    main()
