#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os
import shutil
import subprocess
import sys
from pathlib import Path
from urllib.error import URLError
from urllib.request import urlopen

CPUS_JSON_URL = "https://raw.githubusercontent.com/ARM-software/data/master/cpus.json"

def main():
    repo_root = Path(__file__).resolve().parent.parent
    target = repo_root / "atperf-agent" / "systeminfo" / "armcpus" / "cpus.json"

    print(f"Downloading {CPUS_JSON_URL} to {target}")
    try:
        with urlopen(CPUS_JSON_URL) as response, target.open("wb") as out:
            shutil.copyfileobj(response, out)
    except URLError as exc:
        sys.exit(f"Unable to download cpus.json: {exc.reason}")

    print("Generating atperf-agent/systeminfo/armcpus/cpus.go")
    try:
        subprocess.run(
            ["go", "generate", "./systeminfo/armcpus"],
            cwd=repo_root / "atperf-agent",
            check=True,
        )
    except subprocess.CalledProcessError as exc:
        sys.exit(f"go generate failed with exit code {exc.returncode}")


if __name__ == "__main__":
    main()
