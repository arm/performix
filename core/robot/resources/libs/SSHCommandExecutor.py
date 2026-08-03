# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os

import paramiko
from robot.api import logger


class SSHCommandExecutor:
    def __init__(self, log_context: str):
        self._log_context = log_context
        self._client = None
        self._connection_kwargs = None

    def connect(self, *, hostname: str, username: str, key_filename: str, port: int = 22):
        connection_kwargs = {
            "hostname": hostname,
            "username": username,
            "key_filename": os.path.expanduser(key_filename),
            "port": port,
        }

        if self._client and self._connection_kwargs == connection_kwargs:
            transport = self._client.get_transport()
            if transport and transport.is_active():
                return

        self.close()
        self._connect_with_kwargs(connection_kwargs)

    def close(self):
        if self._client:
            self._client.close()
        self._client = None
        self._connection_kwargs = None

    def is_connected(self) -> bool:
        if self._client is None:
            return False
        transport = self._client.get_transport()
        return bool(transport and transport.is_active())

    def exec_command(
        self,
        command: str,
        *,
        timeout=None,
        get_pty: bool = False,
        retry_on_open_channel: bool = True,
    ):
        return self._run_with_retry(
            command,
            timeout=timeout,
            get_pty=get_pty,
            retry_on_open_channel=retry_on_open_channel,
        )

    def describe_transport(self):
        if self._client is None:
            return "transport=None"

        transport = self._client.get_transport()
        if transport is None:
            return "transport=None"

        active = transport.is_active()
        authenticated = transport.is_authenticated()
        saved_exception = transport.get_exception()
        saved_exception_text = repr(saved_exception) if saved_exception else "None"
        return (
            f"transport_active={active}, "
            f"transport_authenticated={authenticated}, "
            f"transport_exception={saved_exception_text}"
        )

    def _connect_with_kwargs(self, connection_kwargs):
        client = paramiko.SSHClient()
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        client.connect(**connection_kwargs)
        self._client = client
        self._connection_kwargs = connection_kwargs

    def _run_with_retry(
        self,
        command,
        *,
        timeout,
        get_pty,
        retry_on_open_channel,
    ):
        if self._client is None:
            raise AssertionError("SSH client is not connected.")
        try:
            return self._client.exec_command(
                command,
                timeout=timeout,
                get_pty=get_pty,
            )
        except paramiko.SSHException as exc:
            if (
                retry_on_open_channel
                and self._is_open_channel_error(exc)
                and self._connection_kwargs is not None
            ):
                logger.info(
                    f"Unable to open SSH channel in {self._log_context}; "
                    f"{self.describe_transport()}. "
                    "Will reconnect and retry once."
                )
                connection_kwargs = self._connection_kwargs.copy()
                self.close()
                self._connect_with_kwargs(connection_kwargs)
                try:
                    return self._run_with_retry(
                        command,
                        timeout=timeout,
                        get_pty=get_pty,
                        retry_on_open_channel=False,
                    )
                except Exception:
                    logger.info(
                        f"SSH channel retry failed in {self._log_context}; "
                        f"{self.describe_transport()}."
                    )
                    raise
            raise

    @staticmethod
    def _is_open_channel_error(exc: Exception) -> bool:
        return "Unable to open channel" in str(exc)
