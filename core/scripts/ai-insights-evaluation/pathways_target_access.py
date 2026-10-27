# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Prepare workload source and SSH credentials for a disposable target.

The supplied SSH account is also used by the evaluated agent. CI owns the
machine's lifetime; local developers own target cleanup. This module only
transfers source over SSH and prepares the agent's credentials.
"""

import os
import shlex
import shutil
import subprocess
import tempfile
import textwrap
from pathlib import Path


class TargetAccessError(RuntimeError):
    """A target command failed; its diagnostic is suitable for the terminal."""


class TargetAccess:
    """SSH access to an existing target, not a manager for the machine's lifetime."""

    def __init__(self, target):
        self.target = target

    def ssh(self, command, *, input=None):
        """Run a preparation command using the supplied SSH account."""
        result = subprocess.run(
            ["ssh", "-F", os.devnull, "-o", "IdentityAgent=none",
             "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
             "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10",
             "-i", self.target["key"], "-p", str(self.target["port"]),
             f"{self.target['user']}@{self.target['host']}", command],
            input=input, capture_output=True, check=False, timeout=120,
        )
        if result.returncode:
            detail = result.stderr.decode("utf-8", errors="replace").strip() or "no stderr output"
            raise TargetAccessError(
                f"SSH command failed on {self.target['user']}@{self.target['host']}:{self.target['port']} "
                f"(exit {result.returncode}): {detail}"
            )
        return result

    def seed(self, workspace, baseline):
        """Clone the host baseline into a new /workspace without replacing old files."""
        with tempfile.TemporaryDirectory(prefix="pathways-seed-") as directory:
            bundle = Path(directory) / "workload.bundle"
            subprocess.run(["git", "-C", str(workspace), "bundle", "create", str(bundle), "--all"],
                           check=True, capture_output=True)
            # A leftover workspace is an error. Target cleanup belongs to the
            # caller, and this setup must never erase an earlier local attempt.
            command = textwrap.dedent(f"""\
                set -eu
                if test -e /workspace || test -L /workspace; then
                    echo 'Pathways requires a clean target: /workspace already exists' >&2
                    echo 'Use a fresh target or clean up the previous attempt before retrying.' >&2
                    exit 1
                fi
                bundle=$(mktemp)
                trap 'rm -f "$bundle"' EXIT
                cat > "$bundle"
                sudo -n mkdir /workspace
                sudo -n chown "$(id -u):$(id -g)" /workspace
                git clone "$bundle" /workspace
                git -C /workspace checkout --detach {shlex.quote(baseline)}
                git -C /workspace remote remove origin
                git -C /workspace config user.name 'Pathways evaluation'
                git -C /workspace config user.email 'pathways@example.invalid'
                """)
            self.ssh(command, input=bundle.read_bytes())

    def export_connection(self, directory):
        """Copy target credentials and trusted host keys into the agent's private home."""
        directory.mkdir(parents=True, mode=0o700)
        key = directory / "target_key"
        shutil.copyfile(self.target["key"], key)
        key.chmod(0o600)
        name = self.target["host"] if self.target["port"] == 22 else f"[{self.target['host']}]:{self.target['port']}"
        hosts = subprocess.run(["ssh-keygen", "-F", name], check=True, capture_output=True)
        (directory / "known_hosts").write_bytes(hosts.stdout)
        return {"host": self.target["host"], "port": self.target["port"],
                "user": self.target["user"], "private_key_path": str(key)}
