# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Test disposable-target coordination without SSH or administrator setup."""

import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import call, Mock, patch

from pathways_attempt import PathwaysAttempt
from pathways_target_access import TargetAccess


def fake_pathways_attempt(root, events=None):
    events = events if events is not None else []
    runtime = PathwaysAttempt({}, root, root.parent)
    runtime.target = Mock(spec=TargetAccess)
    runtime.target.seed.side_effect = lambda *args: events.append("seed")
    def export(directory):
        directory.mkdir(parents=True)
        (directory / "known_hosts").write_text("trusted")
        key = directory / "target_key"
        key.write_text("scoped")
        return {"host": "target", "port": 22, "user": "target-user", "private_key_path": str(key)}
    runtime.target.export_connection.side_effect = export
    return runtime


class AttemptTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.events = []
        self.attempt = self.root / "results/001"
        self.attempt.mkdir(parents=True)
        self.runtime = fake_pathways_attempt(self.attempt, self.events)

    def prepare(self):
        workspace = self.attempt / "codex_workspace"
        workspace.mkdir()
        self.runtime.prepare_workspace(workspace, "baseline")

    def test_capture_removes_host_state_and_leaves_target_alone(self):
        self.prepare()
        (self.attempt / "score.json").write_text('{"final":{"final_label":"pass"}}')
        self.runtime.collect_evidence_and_clean_host()
        self.assertEqual(self.events, ["seed"])
        self.assertFalse((self.attempt / "agent_home").exists())
        self.assertFalse((self.attempt / "performix_state").exists())

    def test_direct_ssh_configuration_uses_sandbox_proxy(self):
        self.prepare()
        config = (self.attempt / "agent_home/.ssh/config").read_text()
        self.assertIn("HostName target", config)
        self.assertIn("ProxyCommand", config)
        self.assertNotIn("-W", config)

    def test_environment_keeps_runtime_settings_and_excludes_harness_credentials(self):
        self.prepare()
        runtime = {"PATH": "/bin", "LANG": "C.UTF-8", "https_proxy": "http://proxy:8080",
                   "REQUESTS_CA_BUNDLE": "/certs/ca.pem", "NODE_EXTRA_CA_CERTS": "/certs/node.pem"}
        excluded = dict.fromkeys(("OPENAI_API_KEY", "OPENAI_PROXY_TOKEN", "ARTIFACTORY_API_TOKEN",
                                 "artifactory_access_token", "OTHER_TOKEN", "OTHER_KEY", "OTHER_SECRET",
                                 "SSH_AUTH_SOCK", "SSH_AGENT_PID", "HOME", "CODEX_HOME", "NODE_OPTIONS"), "private")
        with patch("performix_attempt.os.environ", {**runtime, **excluded}):
            env = self.runtime.performix.environment()
            self.assertEqual("private", os.environ["ARTIFACTORY_API_TOKEN"])
        state = self.runtime.performix_state
        self.assertEqual({**runtime, "HOME": state["home_dir"],
                          "APXD_CONFIG_DIR": state["config_dir"], "APXD_DATA_DIR": state["data_dir"],
                          "XDG_STATE_HOME": state["state_dir"], "APXD_ENABLE_SECONDARY_RUN_PATHS": "false"}, env)

    def test_setup_failure_still_cleans_host(self):
        self.runtime.target.seed.side_effect = RuntimeError("SSH failed")
        with self.assertRaisesRegex(RuntimeError, "SSH failed"):
            self.prepare()
        self.runtime.collect_evidence_and_clean_host()
        self.assertFalse((self.attempt / "performix_state").exists())

    def test_fixture_import_rejects_a_store_containing_previous_runs(self):
        self.prepare()
        for needs_kill in (False, True):
            with self.subTest(needs_kill=needs_kill):
                process = Mock()
                process.poll.return_value = None
                if needs_kill:
                    process.wait.side_effect = [subprocess.TimeoutExpired("apx", 15), 0]
                replies = [
                    '{"data":{"new_id":{"value":"fixture"}}}',
                    '{"data":{}}',
                    '{"data":{"runs":[{"id":"fixture"},{"id":"previous"}]}}',
                ]
                with patch("performix_attempt.subprocess.Popen", return_value=process), \
                        patch("performix_attempt.socket.create_connection"), \
                        patch("performix_attempt.subprocess.run", side_effect=[
                            Mock(returncode=0, stdout=reply) for reply in replies]):
                    with self.assertRaisesRegex(RuntimeError, "unexpected runs"):
                        self.runtime.performix.import_fixture(Path("apx"), Path("fixture.zip"), None)
                process.terminate.assert_called_once()
                if needs_kill:
                    process.kill.assert_called_once()
                    self.assertEqual(process.wait.call_args_list, [call(timeout=15), call(timeout=15)])
                else:
                    process.kill.assert_not_called()
                    process.wait.assert_called_once_with(timeout=15)

    def test_fixture_import_and_source_update_share_the_private_daemon_environment(self):
        self.prepare()
        process = Mock()
        process.poll.side_effect = [None, 0]
        replies = ['{"data":{"new_id":{"value":"fixture"}}}', '', '',
                   '{"data":{"runs":[{"id":"fixture"}]}}', '']
        with patch("performix_attempt.subprocess.Popen", return_value=process) as start, \
                patch("performix_attempt.socket.create_connection") as connect, \
                patch("performix_attempt.subprocess.run", side_effect=[
                    Mock(returncode=0, stdout=reply) for reply in replies]) as run:
            self.assertEqual(self.runtime.performix.import_fixture(Path("apx"), Path("fixture.zip"), Path("source")), "fixture")
        commands = [call.args[0][1:] for call in run.call_args_list]
        self.assertEqual(commands[:2], [["run", "import", "fixture.zip", "--json"],
                                       ["run", "update", "fixture", "--source", "source"]])
        self.assertEqual(commands[-1], ["daemon", "stop"])
        env = start.call_args.kwargs["env"]
        connect.assert_called_once_with(("127.0.0.1", int(env["APXD_SERVER_PORT"])), timeout=.2)
        self.assertNotIn("APXD_AUTH_PORT", env)
        self.assertEqual(env["APXD_CONFIG_DIR"], self.runtime.performix_state["config_dir"])
        for call in run.call_args_list:
            self.assertEqual(call.kwargs["env"], env)
            self.assertEqual(call.kwargs["timeout"], 120)
        process.terminate.assert_not_called()
        process.wait.assert_called_once_with(timeout=15)

    def test_failed_capture_keeps_host_evidence_for_manual_investigation(self):
        self.prepare()
        (self.attempt / "codex_home").mkdir()
        with patch("pathways_attempt.persist_codex_session_log", side_effect=RuntimeError("session unavailable")):
            with self.assertRaisesRegex(RuntimeError, "session unavailable"):
                self.runtime.collect_evidence_and_clean_host()
        self.assertTrue((self.attempt / "codex_workspace").exists())


class AttemptPathTests(unittest.TestCase):
    def test_attempt_rejects_paths_outside_results(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            for invalid in (root, root.parent / "outside-results"):
                with self.assertRaisesRegex(ValueError, "outside"):
                    PathwaysAttempt({}, invalid, root)


class SshProxyTests(unittest.TestCase):
    def test_connect_obeys_injected_proxy_and_preserves_ssh_bytes(self):
        from pathways_ssh_proxy import connect
        received = iter(bytes([value]) for value in b"HTTP/1.1 200 Connection established\r\n\r\nSSH-banner")
        connection = Mock()
        connection.recv.side_effect = lambda count: next(received)
        with patch.dict("os.environ", {"HTTP_PROXY": "http://127.0.0.1:49152"}), \
                patch("pathways_ssh_proxy.socket.create_connection", return_value=connection) as create:
            self.assertIs(connect("10.0.0.1", 22), connection)
        create.assert_called_once_with(("127.0.0.1", 49152), timeout=15)
        self.assertIn(b"CONNECT 10.0.0.1:22 HTTP/1.1", connection.sendall.call_args.args[0])
        self.assertEqual(connection.recv(1), b"S")

    def test_denial_closes_connection_without_direct_fallback(self):
        from pathways_ssh_proxy import connect
        received = iter(bytes([value]) for value in b"HTTP/1.1 403 Forbidden\r\n\r\n")
        connection = Mock()
        connection.recv.side_effect = lambda count: next(received)
        with patch.dict("os.environ", {"HTTP_PROXY": "http://127.0.0.1:49152"}), \
                patch("pathways_ssh_proxy.socket.create_connection", return_value=connection) as create:
            with self.assertRaisesRegex(RuntimeError, "denied"):
                connect("10.0.0.1", 22)
        connection.close.assert_called_once()
        self.assertEqual(create.call_count, 1)

    def test_missing_proxy_does_not_use_direct_networking(self):
        from pathways_ssh_proxy import connect
        with patch.dict("os.environ", {}, clear=True), patch("pathways_ssh_proxy.socket.create_connection") as create:
            with self.assertRaisesRegex(RuntimeError, "HTTP_PROXY"):
                connect("target", 22)
        create.assert_not_called()
