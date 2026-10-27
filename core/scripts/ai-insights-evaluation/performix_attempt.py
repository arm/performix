# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Own private Performix storage and host processes for one evaluation attempt."""

import json
import os
import shutil
import socket
import subprocess
import sys
import time
from contextlib import contextmanager
from pathlib import Path

sys.path.append(str(Path(__file__).resolve().parents[1]))
from run_export_helper import import_run, run_cli, update_run_source

# Harness environment variables allowed into Codex and fixture-preparation processes.
HOST_SUBPROCESS_ENV_ALLOWLIST = {
    "PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "TMPDIR", "TMP", "TEMP", "TZ",
    "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "LC_COLLATE",
    "LC_NUMERIC", "LC_MONETARY", "LC_TIME",
    "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
    "SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS",
}


def prepare_performix_state(attempt_dir: Path) -> dict[str, str]:
    """Create the isolated persistent state used by one evaluation attempt."""
    state_dir = attempt_dir / "performix_state"
    config_dir = state_dir / "config"
    data_dir = state_dir / "data"
    config_dir.mkdir(parents=True)
    data_dir.mkdir(parents=True)
    state_path = state_dir / "state"
    state_path.mkdir()
    return {
        "config_dir": str(config_dir),
        "data_dir": str(data_dir),
        "state_dir": str(state_path),
    }


def performix_env(state):
    """Settings shared by fixture preparation, Codex and the attempt's MCP server."""
    required = ("config_dir", "data_dir", "state_dir")
    if any(not isinstance(state.get(key), str) or not state[key].strip() for key in required):
        raise RuntimeError("invalid Performix state")
    return {
        "APXD_CONFIG_DIR": state["config_dir"],
        "APXD_DATA_DIR": state["data_dir"],
        "XDG_STATE_HOME": state["state_dir"],
        "APXD_ENABLE_SECONDARY_RUN_PATHS": "false",
        **({"HOME": state["home_dir"]} if "home_dir" in state else {}),
    }


class PerformixAttempt:
    """Keep each (testcase, mode, attempt) independent of other Performix users."""

    def __init__(self, directory):
        self.directory = Path(directory)
        self.state = {}

    def prepare(self):
        self.state = prepare_performix_state(self.directory)
        home = self.directory / "agent_home"
        home.mkdir(mode=0o700)
        self.state.update(home_dir=str(home))

    def clean(self):
        """Remove this attempt's private Performix storage and home directory."""
        for name in ("performix_state", "agent_home"):
            path = self.directory / name
            if path.exists():
                shutil.rmtree(path)

    def environment(self):
        """Build the host environment for Codex and the fixture-import CLI/daemon.

        Copy allowed runtime settings (executable paths, locale, proxies and CA
        certificates) from the harness, then add this attempt's HOME, Performix
        state directories. The caller adds CODEX_HOME and API credentials only
        when launching Codex; its shell policy removes
        them from agent commands.

        This leaves the harness's own environment unchanged. Its OpenAI and
        Artifactory credentials are excluded from the returned environment.
        Remote target environments are configured separately.
        """
        env = {key: value for key, value in os.environ.items() if key.upper() in HOST_SUBPROCESS_ENV_ALLOWLIST}
        return {**env, **performix_env(self.state)}

    @contextmanager
    def daemon(self, cli):
        """Run a private daemon to import a fixture and associate its source files."""
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            port = listener.getsockname()[1]
        env = self.environment()
        env.update(APXD_SERVER_PORT=str(port),
                   APXD_HTTP_PORT="0", APXD_SERVER_HOSTNAME="127.0.0.1", APXD_DAEMON_AUTOSTART="false")
        def cli_call(*args):
            return run_cli([str(cli), *args], env=env, timeout=120).stdout
        with (Path(self.state["state_dir"]) / "daemon.log").open("a") as log:
            process = subprocess.Popen([str(cli), "daemon", "start", "--block"], env=env,
                                       stdout=log, stderr=log, start_new_session=True)
            try:
                deadline = time.monotonic() + 60
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("private Performix daemon exited before readiness")
                    try:
                        with socket.create_connection(("127.0.0.1", port), timeout=.2):
                            pass
                        break
                    except OSError:
                        if time.monotonic() > deadline:
                            raise RuntimeError("private Performix daemon did not become ready")
                        time.sleep(.1)
                yield env
                cli_call("daemon", "stop")
                process.wait(timeout=15)
            finally:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=15)

    def import_fixture(self, cli, archive, source_root):
        """Import into this attempt's store before its mode starts a daemon."""
        with self.daemon(cli) as env:
            def cli_call(*args):
                return run_cli([str(cli), *args], env=env, timeout=120).stdout
            run_id = import_run(cli, archive, env=env, timeout=120)
            if source_root:
                update_run_source(cli, run_id, source_root, env=env, timeout=120)
            cli_call("run", "info", run_id, "--json")
            listing = json.loads(cli_call("run", "list", "--json"))["data"]
            if [run["id"] for run in listing["runs"]] != [run_id]:
                raise RuntimeError("private Performix store contains unexpected runs")
            return run_id
