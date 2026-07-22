# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os
import shlex
import subprocess
import tempfile
from dataclasses import dataclass

import paramiko
from robot.api import logger
from robot.libraries.BuiltIn import BuiltIn


@dataclass(slots=True)
class CommandResult:
    rc: int
    stdout: str
    stderr: str


class CommandService:
    """
    CommandService is a helper class that imeplements command running on host and target.
    It sets and uses various Robot variables:
        - G_LAST_RESULT is set to the result of the last command run.
        - G_LAST_PROCESS is set to the handle of the last process started.
        - G_TARGET_HOST, G_TARGET_USER, G_TARGET_KEY, G_TARGET_PORT are used for SSH connection.
        - G_TARGET_NAME and LOCALHOST_TARGET_NAME are used to determine if the target is localhost.
        - G_TARGET_OS is used to determine the target operating system.
    """

    def __init__(self, built_in: BuiltIn) -> None:
        self._built_in = built_in
        self._process_lib = None
        self._paramiko_client = None
        self._paramiko_client_info = None

    def run_host_command(self, cmd: str) -> CommandResult:
        process = self._get_process_library()

        proc = process.run_process(cmd, shell=True)
        result = CommandResult(rc=proc.rc, stdout=proc.stdout, stderr=proc.stderr)
        self._set_global("G_LAST_RESULT", result)
        self._log_result(result)
        return result

    def start_host_command(self, cmd):
        process = self._get_process_library()

        start_kwargs = {
            "shell": True,
            "stdout": os.path.join(tempfile.gettempdir(), "stdout.txt"),
            "stdin": subprocess.PIPE,
        }

        proc_handle = process.start_process(cmd, **start_kwargs)
        self._set_global("G_LAST_PROCESS", proc_handle)
        return proc_handle

    def run_target_command(self, cmd) -> CommandResult:
        target_name = self._get_var("G_TARGET_NAME")
        localhost_name = self._get_var("LOCALHOST_TARGET_NAME")
        target_os = self._get_var("G_TARGET_OS")

        command = self._strip_outer_quotes(cmd)

        if target_name == localhost_name:
            return self.run_host_command(command)

        result = self._run_remote_command(command, target_os)
        self._set_global("G_LAST_RESULT", result)
        self._log_result(result)
        return result

    def run_target_command_in_background(self, cmd: str) -> CommandResult:
        target_name = self._get_var("G_TARGET_NAME")
        localhost_name = self._get_var("LOCALHOST_TARGET_NAME")
        target_os = self._get_var("G_TARGET_OS")

        background_cmd = self._build_background_command(cmd, target_os)

        if target_name == localhost_name:
            return self.run_host_command(background_cmd)

        result = self._run_remote_command_background(background_cmd, target_os)
        self._set_global("G_LAST_RESULT", result)
        self._log_result(result)
        return result

    @staticmethod
    def _build_background_command(cmd: str, target_os: str) -> str:
        normalized = CommandService._strip_outer_quotes(cmd)

        os_lower = (target_os or "").strip().lower()
        if os_lower == "windows":
            raise NotImplementedError(
                "Running background processes for Windows targets is not implemented."
            )

        # Run in background and print the child PID.
        wrapped = f"{normalized} > /tmp/background-process.log 2>&1 & echo $!"
        return f"/bin/sh -lc {shlex.quote(wrapped)}"

    def _get_process_library(self):
        if self._process_lib is None:
            self._process_lib = self._built_in.get_library_instance("Process")
        return self._process_lib

    def _set_global(self, name, value) -> None:
        self._built_in.set_global_variable(f"${{{name}}}", value)

    def _get_var(self, name, default=None):
        return self._built_in.get_variable_value(f"${{{name}}}", default)

    @staticmethod
    def _strip_outer_quotes(text: str) -> str:
        if len(text) >= 2 and text[0] == text[-1] and text[0] in {'"', "'"}:
            return text[1:-1]
        return text

    @staticmethod
    def _log_result(result: CommandResult) -> None:
        logger.info(
            f"rc: {result.rc}  stdout: {result.stdout}  stderr: {result.stderr}"
        )

    def _run_remote_command(self, cmd: str, target_os: str) -> CommandResult:
        client = self._get_paramiko_client()

        os_lower = (target_os or "").strip().lower()
        is_linux = os_lower == "linux"
        if os_lower == "windows":
            remote_command = cmd
        else:
            remote_command = f"/bin/sh -lc {shlex.quote(cmd)}"

        logger.info(f"Running remote command:\n{remote_command}")

        _, stdout, stderr = client.exec_command(remote_command, get_pty=is_linux)
        stdout_data = stdout.read().decode("utf-8", errors="replace")
        stderr_data = stderr.read().decode("utf-8", errors="replace")
        exit_status = stdout.channel.recv_exit_status()

        return CommandResult(rc=exit_status, stdout=stdout_data, stderr=stderr_data)

    def _run_remote_command_background(self, cmd: str, target_os: str) -> CommandResult:
        client = self._get_paramiko_client()

        remote_command = cmd

        logger.info(f"Running remote background command:\n{remote_command}")

        transport = client.get_transport()
        channel = transport.open_session()
        channel.exec_command(remote_command)
        stdout_data = channel.makefile("r").read()
        stderr_data = channel.makefile_stderr("r").read()
        exit_status = channel.recv_exit_status()

        return CommandResult(rc=exit_status, stdout=stdout_data, stderr=stderr_data)

    def _get_paramiko_client(self) -> paramiko.SSHClient:
        host = self._get_var("G_TARGET_HOST")
        user = self._get_var("G_TARGET_USER")
        key_path = self._get_var("G_TARGET_KEY")
        port = self._get_var("G_TARGET_PORT", 22)

        if not host or not user or not key_path:
            raise AssertionError(
                "SSH connection details (host, user, key) must be set before running target commands."
            )

        expanded_key = os.path.expanduser(key_path)
        connection_info = (host, port, user, expanded_key)

        if self._paramiko_client:
            if self._paramiko_client_info == connection_info:
                transport = self._paramiko_client.get_transport()
                if transport and transport.is_active():
                    return self._paramiko_client
            self._paramiko_client.close()
            self._paramiko_client = None
            self._paramiko_client_info = None

        client = paramiko.SSHClient()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        client.connect(
            hostname=host,
            username=user,
            key_filename=expanded_key,
            port=port,
        )
        self._paramiko_client = client
        self._paramiko_client_info = connection_info
        return client
