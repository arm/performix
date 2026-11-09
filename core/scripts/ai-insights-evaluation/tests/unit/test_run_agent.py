# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Check agent shutdown without leaving attempt-owned child processes running."""

import os
import signal
import subprocess
import sys
import time
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock, Mock, call

import pytest

from tests.evaluation import test_ai_insights_evaluation as evaluation


@pytest.mark.parametrize("outcome", ["success", "timeout", "unresponsive_timeout", "cancel"])
def test_agent_always_cleans_its_posix_group(monkeypatch, outcome):
    process = MagicMock(pid=12345, returncode=0)
    process.__enter__.return_value = process
    process.poll.return_value = None
    error = None
    if outcome in ("timeout", "unresponsive_timeout"):
        error = subprocess.TimeoutExpired(["agent"], 1)
        grace_result = subprocess.TimeoutExpired(["agent"], 15) if outcome == "unresponsive_timeout" else 0
        process.wait.side_effect = [error, grace_result, 0]
    elif outcome == "cancel":
        error = KeyboardInterrupt()
        process.wait.side_effect = [error, 0, 0]
    killpg = Mock()
    monkeypatch.setattr(evaluation, "os", SimpleNamespace(name="posix", killpg=killpg))
    monkeypatch.setattr(evaluation, "signal", SimpleNamespace(SIGINT=signal.SIGINT, SIGKILL=9))
    monkeypatch.setattr(evaluation.subprocess, "Popen", Mock(return_value=process))

    if error is None:
        result = evaluation.run_agent(["agent"], timeout=1)
        assert result.returncode == 0
        process.send_signal.assert_not_called()
    else:
        with pytest.raises(type(error)) as raised:
            evaluation.run_agent(["agent"], timeout=1)
        assert raised.value is error
        process.send_signal.assert_called_once_with(signal.SIGINT)
    killpg.assert_called_once_with(process.pid, 9)
    process.kill.assert_not_called()
    expected_waits = [call(timeout=1)]
    if error is not None:
        expected_waits.append(call(timeout=15))
    assert process.wait.call_args_list == [*expected_waits, call()]


def test_agent_tolerates_an_already_exited_group(monkeypatch):
    process = MagicMock(pid=12345, returncode=0)
    process.__enter__.return_value = process
    killpg = Mock(side_effect=ProcessLookupError)
    monkeypatch.setattr(evaluation, "os", SimpleNamespace(name="posix", killpg=killpg))
    monkeypatch.setattr(evaluation, "signal", SimpleNamespace(SIGINT=signal.SIGINT, SIGKILL=9))
    monkeypatch.setattr(evaluation.subprocess, "Popen", Mock(return_value=process))

    assert evaluation.run_agent(["agent"]).returncode == 0
    killpg.assert_called_once_with(process.pid, 9)
    assert process.wait.call_count == 2


def test_non_posix_timeout_still_kills_and_waits_for_parent(monkeypatch):
    process = MagicMock(returncode=1)
    process.__enter__.return_value = process
    process.poll.return_value = None
    process.wait.side_effect = [subprocess.TimeoutExpired(["agent"], 1), 1]
    monkeypatch.setattr(evaluation, "os", SimpleNamespace(name="nt"))
    monkeypatch.setattr(evaluation.subprocess, "Popen", Mock(return_value=process))

    with pytest.raises(subprocess.TimeoutExpired):
        evaluation.run_agent(["agent"], timeout=1)
    process.send_signal.assert_not_called()
    process.kill.assert_called_once()
    assert process.wait.call_args == call()


def process_is_running(pid):
    try:
        status = Path(f"/proc/{pid}/stat")
        if status.exists() and status.read_text().rsplit(")", 1)[1].split()[0] == "Z":
            return False
        os.kill(pid, 0)
    except (ProcessLookupError, FileNotFoundError):
        return False
    return True


def test_process_exiting_during_status_read_is_not_running(monkeypatch):
    monkeypatch.setattr(Path, "exists", lambda self: True)
    monkeypatch.setattr(Path, "read_text", Mock(side_effect=FileNotFoundError))

    assert not process_is_running(12345)


@pytest.mark.skipif(os.name != "posix", reason="Requires POSIX process groups")
@pytest.mark.parametrize("outcome", ["success", "timeout", "cancel"])
def test_agent_stops_lingering_child(tmp_path, monkeypatch, outcome):
    child_pid_file = tmp_path / "child.pid"
    child_code = """
import os, signal, time
from pathlib import Path
signal.signal(signal.SIGINT, signal.SIG_IGN)
Path(CHILD_PID_FILE).write_text(str(os.getpid()))
time.sleep(60)
""".replace("CHILD_PID_FILE", repr(str(child_pid_file)))
    parent_code = """
import signal, subprocess, sys, time
from pathlib import Path
signal.signal(signal.SIGINT, signal.SIG_IGN if OUTCOME == 'timeout' else signal.SIG_DFL)
subprocess.Popen([sys.executable, '-c', CHILD_CODE])
while not Path(CHILD_PID_FILE).exists():
    time.sleep(.01)
if OUTCOME != 'success':
    time.sleep(60)
""".replace("OUTCOME", repr(outcome)).replace("CHILD_CODE", repr(child_code)).replace(
        "CHILD_PID_FILE", repr(str(child_pid_file)))
    if outcome == "cancel":
        original_wait = subprocess.Popen.wait
        cancelled = False

        def cancel_when_child_ready(process, *args, **kwargs):
            nonlocal cancelled
            if not cancelled:
                deadline = time.monotonic() + 5
                while not child_pid_file.exists() and time.monotonic() < deadline:
                    time.sleep(.01)
                cancelled = True
                raise KeyboardInterrupt()
            return original_wait(process, *args, **kwargs)

        monkeypatch.setattr(subprocess.Popen, "wait", cancel_when_child_ready)
    try:
        argv = [sys.executable, "-c", parent_code]
        if outcome == "success":
            assert evaluation.run_agent(argv, timeout=5).returncode == 0
        else:
            error = subprocess.TimeoutExpired if outcome == "timeout" else KeyboardInterrupt
            with pytest.raises(error):
                evaluation.run_agent(argv, timeout=1)
        assert child_pid_file.exists(), "The child must have started before cleanup"
        child_pid = int(child_pid_file.read_text())
        deadline = time.monotonic() + 5
        while process_is_running(child_pid) and time.monotonic() < deadline:
            time.sleep(.01)
        assert not process_is_running(child_pid), "An attempt-owned child survived cleanup"
    finally:
        if child_pid_file.exists():
            child_pid = int(child_pid_file.read_text())
            if process_is_running(child_pid):
                os.kill(child_pid, signal.SIGKILL)
