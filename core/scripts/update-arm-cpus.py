#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import subprocess
import sys
from pathlib import Path

import requests

CPUS_JSON_URL = "https://raw.githubusercontent.com/ARM-software/data/master/cpus.json"

def main():
    repo_root = Path(__file__).resolve().parent.parent
    target = repo_root / "atperf-agent" / "systeminfo" / "armcpus" / "cpus.json"

    print(f"Downloading {CPUS_JSON_URL} to {target}")
    try:
        response = requests.get(CPUS_JSON_URL, timeout=120)
        response.raise_for_status()
        target.write_bytes(response.content)
    except requests.RequestException as exc:
        sys.exit(f"Unable to download cpus.json: {exc}")

    print("Generating atperf-agent/systeminfo/armcpus/cpus.go")
    try:
        subprocess.run(
            ["go", "run", "generate_cpus.go", "-in", "cpus.json", "-out", "cpus.go"],
            cwd=target.parent,
            check=True,
        )
    except subprocess.CalledProcessError as exc:
        sys.exit(f"CPU data generation failed with exit code {exc.returncode}")


if __name__ == "__main__":
    main()
