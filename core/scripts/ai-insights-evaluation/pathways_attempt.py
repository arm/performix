# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Prepare one Pathways attempt on a disposable target and capture its evidence.

CI supplies a fresh machine for each attempt and terminates it afterward.
Local developers supply a clean target and clean it up manually. This module
manages the host's temporary files; it does not isolate, reset,
or destroy the target.
"""

import shlex
import shutil
import sys
import tempfile
from pathlib import Path

from codex_session_metrics import persist_codex_session_log
from pathways_target_access import TargetAccess

from performix_attempt import PerformixAttempt


class PathwaysAttempt:
    """Own one attempt's host resources and access its caller-supplied target."""

    def __init__(self, target, attempt_dir, results_dir):
        """Create one attempt; CI or the developer supplies a fresh target."""
        attempt_dir, results_dir = Path(attempt_dir).resolve(), Path(results_dir).resolve()
        if not attempt_dir.is_relative_to(results_dir) or attempt_dir == results_dir:
            raise ValueError("attempt is outside its results directory")
        self.target = TargetAccess(target)
        self.directory = attempt_dir
        # Agent-visible paths must not encode the scenario or attempt number.
        # Keep runtime state outside the labelled evidence tree, without a symlink
        # back to it. Explicit cleanup preserves state if evidence capture fails.
        self.runtime_directory = Path(tempfile.mkdtemp(prefix="performix-agent-")).resolve()
        self.performix = PerformixAttempt(self.runtime_directory)

    @property
    def performix_state(self):
        return self.performix.state

    def prepare_workspace(self, workspace, baseline):
        """Prepare private Performix state, target source and the agent's SSH access."""
        self.performix.prepare()
        self.target.seed(workspace, baseline)
        home = self.runtime_directory / "agent_home"
        connection = self.target.export_connection(home / ".ssh")
        helper = home / "ssh_proxy.py"
        shutil.copyfile(Path(__file__).with_name("pathways_ssh_proxy.py"), helper)
        proxy = shlex.join([sys.executable, str(helper), connection["host"], str(connection["port"])])
        config = home / ".ssh/config"
        config.write_text(
            f"Host pathways\n  HostName {connection['host']}\n  Port {connection['port']}\n  User {connection['user']}\n"
            f'  IdentityFile "{connection["private_key_path"]}"\n'
            f'  UserKnownHostsFile "{home / ".ssh/known_hosts"}"\n'
            "  IdentitiesOnly yes\n  IdentityAgent none\n  BatchMode yes\n  StrictHostKeyChecking yes\n"
            f"  ProxyCommand {proxy}\n", encoding="utf-8")
        return connection, f"ssh -F {shlex.quote(str(config))} pathways"

    def collect_evidence_and_clean_host(self):
        """Capture evidence and remove host state; leave the target to its owner."""
        from pathways_artifacts import is_pathways_artifact, preserve_pathways_artifacts
        directory = self.runtime_directory
        errors = []

        def capture(label, action):
            try:
                action()
            except Exception as exc:
                errors.append(f"{label}: {exc}")

        home = directory / "codex_home"
        if home.exists():
            capture("session capture", lambda: persist_codex_session_log(home, directory))
        capture("MCP recipe capture", lambda: preserve_pathways_artifacts(directory, errors))
        # Copy any available evidence even if another capture step failed.
        for path in directory.iterdir():
            if is_pathways_artifact(path.name) and path.is_file():
                capture(path.name, lambda: shutil.copyfile(path, self.directory / path.name))
        if errors:
            raise RuntimeError(f"Host state retained at {directory}: {'; '.join(errors)}")
        shutil.rmtree(directory)


def create_pathways_attempt(cfg, attempt_dir):
    """Read the caller-supplied target settings for one attempt."""
    values = cfg["target_prompt_values"]
    if not values["target_host"] or not values["target_user"] or not values["target_private_key_path"]:
        raise ValueError("Pathways requires target host, user and SSH private-key path")
    target = {"host": values["target_host"], "port": int(values["target_port"] or 22),
              "user": values["target_user"],
              "key": str(Path(values["target_private_key_path"]).expanduser().resolve())}
    return PathwaysAttempt(target, attempt_dir, cfg["results_dir"])
