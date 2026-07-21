# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import logging
import shlex
import shutil
import subprocess
import time
from subprocess import CompletedProcess

from framework.constants import *
from framework.utils import extract_metric_groups


def run_cmd(cmd) -> CompletedProcess[str]:
    """Runs the specified command, and returns a CompletedProcess object"""
    logging.debug(f"Running command: {cmd}")
    result = subprocess.run(
        cmd,
        shell=True,
        text=True,
        capture_output=True,
        stdin=subprocess.DEVNULL,
    )

    if result.returncode != 0:
        raise subprocess.CalledProcessError(result.returncode, cmd, output=result.stdout, stderr=result.stderr)

    return result

def time_cmd(cmd) -> tuple[CompletedProcess[str], float, float]:
    """Runs the specified command, and returns a CompletedProcess object, as well as the start and end times of the
    command (measured in milliseconds since the epoch)."""
    start_ms = time.time_ns() / 1e6
    result = run_cmd(cmd)
    end_ms = time.time_ns() / 1e6

    logging.debug(f"Command completed in {end_ms - start_ms}ms.")

    return result, start_ms, end_ms

def invoke_topdown_tool(args, path = DEFAULT_TOPDOWN_TOOL_PATH) -> str:
    """Runs the topdown tool, returning the stripped stdout output"""
    tool_path = path
    cmd = f"{tool_path} {args}"
    result = run_cmd(cmd)
    return result.stdout.strip()

def invoke_atperf(args, path = DEFAULT_ATPERF_PATH) -> str:
    """Runs atperf, returning the stripped stdout output"""
    tool_path = path
    cmd = f"{tool_path} {args}"
    result = run_cmd(cmd)
    return result.stdout.strip()

def time_perf(args) -> tuple[str, float, float]:
    """Runs perf, returning the stripped stdout output, and the start and end
    times, in milliseconds since the epoch."""
    cmd = f"perf {args}"
    result, start, end = time_cmd(cmd)
    return result.stdout.strip(), start, end

def time_sl_record(args, path) -> tuple[str, float, float]:
    """Runs sl-record, returning the stripped stdout output, and the start and end
    times, in milliseconds since the epoch."""
    tool_path = path
    cmd = f"{tool_path} {args}"
    result, start, end = time_cmd(cmd)
    return result.stdout.strip(), start, end

def clean_environment():
    if OUTPUT_DIR.exists():
        shutil.rmtree(OUTPUT_DIR)

def atperf_prepare_target(target, atperf_path = DEFAULT_ATPERF_PATH):
    logging.info("Preparing target...")
    args = f"target prepare --target {target} {ATPERF_JSON_FLAG}"
    invoke_atperf(args, path=atperf_path)
    return True


def kill_process_by_name(process: str):
    """Kills any running processes matching the process name using sudo."""
    try:
        result = run_cmd(f"pgrep {process}").stdout.strip()
    except subprocess.CalledProcessError as e:
        return
    if not result:
        logging.debug(f"No {process} process found.")
        return
    for pid in result.splitlines():
        logging.debug(f"Killing {process} process with PID {pid}")
        if pid.isdigit():
            run_cmd(f"sudo kill -9 {pid}")
        else:
            logging.debug(f"Skipping invalid PID: {pid}")

def kill_sl_record():
    """Kills any running 'sl-record' process using sudo."""
    kill_process_by_name("sl-record")

def kill_atperf_engine(atperf_path: str = DEFAULT_ATPERF_PATH):
    """Kills any existing atperf engine by running the `daemon stop` command."""
    logging.info("Killing existing atperf engine...")
    args = "daemon stop"
    stdout = ""
    try:
        stdout = invoke_atperf(args, path=atperf_path)
        # Add delay to ensure atperf-agent dies too
        time.sleep(1)
    except subprocess.CalledProcessError as e:
        # Handle case where engine is not currently running
        if "engine.grpcconnection.SERVER_DID_NOT_RESPOND" in stdout:
            pass
        raise e


"""
Clone and run sysreport; write combined stdout to OUTPUT_DIR/sysreport.txt.
NOTE: Sysreport is a Linux-only tool, so can only be used on Linux targets.
Returns path to output file.
"""
def run_sysreport() -> Path:
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    repo_dir = Path("/tmp/atperf-eval/sysreport")
    out_file = OUTPUT_DIR / "sysreport.txt"
    print(f"Running sysreport, outputting to {out_file}...")

    if not repo_dir.exists():
        run_cmd(f"git clone --depth 1 https://github.com/ArmDeveloperEcosystem/sysreport.git {shlex.quote(str(repo_dir))}")
    src = repo_dir / "src" / "sysreport.py"
    with out_file.open("w") as f:
        output = run_cmd(f"python3 {shlex.quote(str(src))}")
        f.write(output.stdout.strip() + "\n")

    return out_file


def create_agent_connection(target, atperf_path = DEFAULT_ATPERF_PATH):
    """
    Creates a connection to the target agent (if one does not already exist) by
    running the ``target info`` command.
    :param target: the target to connect to the agent on
    :param atperf_path: the path to the atperf binary on the host machine
    """
    logging.info("Creating connection to target agent...")
    args = f"target info {target}"
    invoke_atperf(args, path=atperf_path)


def get_metric_groups(atperf_path = DEFAULT_ATPERF_PATH) -> list[str]:
    logging.info("Getting atperf CPU Microarchitecture metric groups...")
    cmd_args = f"recipe info cpu_microarchitecture {ATPERF_JSON_FLAG} {ATPERF_TARGET_FLAG} localhost"
    result = invoke_atperf(cmd_args, path=atperf_path)
    metric_groups = extract_metric_groups(result)
    logging.info(f"Found metric groups: {metric_groups}")
    return metric_groups