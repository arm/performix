# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Carry OpenSSH through the HTTP proxy provided to Codex sandbox commands.

OpenSSH does not use HTTP_PROXY automatically, so it needs a ProxyCommand to
reach the target through Codex's sandbox proxy. This script serves that role:
it opens an HTTP CONNECT tunnel through the proxy, which enforces network
policy, then relays SSH traffic between OpenSSH and the target.

This is host-side networking, not target isolation. There is no direct-network
fallback if the proxy is unavailable or denies the connection.
"""

import os
import socket
import sys
import threading
from urllib.parse import urlsplit


def connect(host, port):
    """Open an HTTP CONNECT tunnel to the target through the sandbox proxy."""
    proxy = urlsplit(os.environ.get("HTTP_PROXY") or os.environ.get("http_proxy", ""))
    if proxy.scheme != "http" or not proxy.hostname or not proxy.port:
        raise RuntimeError("Codex HTTP_PROXY is required for sandboxed SSH")
    connection = socket.create_connection((proxy.hostname, proxy.port), timeout=15)
    try:
        authority = f"[{host}]:{int(port)}" if ":" in host else f"{host}:{int(port)}"
        connection.sendall(f"CONNECT {authority} HTTP/1.1\r\nHost: {authority}\r\n\r\n".encode("ascii"))
        header = bytearray()
        while not header.endswith(b"\r\n\r\n"):
            value = connection.recv(1)
            if not value or len(header) >= 16384:
                raise RuntimeError("incomplete SSH proxy response")
            header.extend(value)
        if bytes(header).split(b"\r\n", 1)[0].split()[1:2] != [b"200"]:
            raise RuntimeError("SSH proxy denied the connection: " + bytes(header).split(b"\r\n", 1)[0].decode(errors="replace"))
        connection.settimeout(None)
        return connection
    except BaseException:
        connection.close()
        raise


def main():
    """Relay OpenSSH's input and output through the tunnel until it closes."""
    with connect(sys.argv[1], sys.argv[2]) as connection:
        def upload():
            try:
                while chunk := os.read(sys.stdin.fileno(), 65536):
                    connection.sendall(chunk)
                connection.shutdown(socket.SHUT_WR)
            except OSError:
                pass  # The SSH peer may already have closed the connection.
        threading.Thread(target=upload, daemon=True).start()
        while chunk := connection.recv(65536):
            sys.stdout.buffer.write(chunk)
            sys.stdout.buffer.flush()


if __name__ == "__main__":
    main()
