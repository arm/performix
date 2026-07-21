# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import subprocess
import platform
import socket
import requests
from robot.api.deco import keyword, library

@library(scope='TEST')
class NetworkUtils:
    """Simple network utilities for Robot Framework."""

    @keyword('Ping Host')
    def ping_host(self, host, count=1, timeout=2):
        """Ping a host. Returns True if reachable."""
        param_count = "-n" if platform.system().lower() == "windows" else "-c"
        param_timeout = "-w" if platform.system().lower() == "windows" else "-W"

        try:
            result = subprocess.run(
                ["ping", param_count, str(count), param_timeout, str(timeout), host],
                capture_output=True,
                text=True
            )
            return result.returncode == 0
        except Exception:
            return False

    @keyword('Port Should Be Open')
    def port_should_be_open(self, host, port, timeout=2):
        """Fails if a TCP port is not open."""
        if not self._check_port(host, port, timeout):
            raise AssertionError(f"Port {port} on {host} is not open.")

    @keyword('Port Should Be Closed')
    def port_should_be_closed(self, host, port, timeout=2):
        """Fails if a TCP port is open."""
        if self._check_port(host, port, timeout):
            raise AssertionError(f"Port {port} on {host} is open but should be closed.")

    @keyword('Check Port')
    def _check_port(self, host, port, timeout):
        """Return True if the TCP port is open."""
        try:
            with socket.create_connection((host, port), timeout=timeout):
                return True
        except Exception:
            return False

    @keyword('HTTP GET Should Succeed')
    def http_get_should_succeed(self, url, timeout=5):
        """Fails if an HTTP GET doesn't return 2xx."""
        try:
            response = requests.get(url, timeout=timeout)
            if not (200 <= response.status_code < 300):
                raise AssertionError(f"HTTP GET {url} failed: {response.status_code}")
        except Exception as exc:
            raise AssertionError(f"HTTP GET {url} failed: {exc}")

    @keyword('Resolve Hostname')
    def resolve_hostname(self, hostname):
        """Return the IP address for a given hostname."""
        try:
            return socket.gethostbyname(hostname)
        except Exception as exc:
            raise AssertionError(f"Failed to resolve hostname {hostname}: {exc}")

    @keyword('Check Host Reachable')
    def check_host_reachable(self, host, port=80, timeout=2):
        """
        Quick reachability test.
        Tries ping first, then TCP as fallback.
        """
        if self.ping_host(host):
            return True

        return self._check_port(host, port, timeout)
