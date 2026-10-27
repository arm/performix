<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Local Engine Transport Security

## Summary

Performix runs an engine daemon on the local machine and connects to it over loopback TCP. Loopback prevents remote access, but it does not identify the OS user making a connection. Multiple local users can therefore reach the same TCP listeners if they discover the ports.

Performix uses per-user daemon management, mTLS for all gRPC services and bearer-token authentication for Arrow queries. This document explains that design and its security properties.

### Terms

- [gRPC](https://grpc.io): remote procedure call format for communication between the engine and clients (CLI, GUI and MCP).
  - Apap service handles targets, recipes, runs, rendering and daemon control.
  - Auth service handles target login operations such as SSH credentials and host verification.
- [Arrow](https://arrow.apache.org): columnar data format used to return query results efficiently from the engine to the GUI renderer over HTTP.

## Scope

This document covers communication between the CLI, GUI, MCP server and the engine daemon:

- the engine gRPC services (Apap and Auth)
- the Arrow HTTP `/query` endpoint
- CLI discovery of a per-user engine daemon

It does not cover authentication to remote targets, SSH security, or communication with target agents.

## Security Model

The relevant threat is another ordinary OS user on the same machine. Administrator/root access and processes already running as the same user are outside this boundary.

Loopback provides network isolation from remote machines, but ordinary TCP does not tell the engine which OS user connected. OS-native local IPC can provide user identity or access control, but Unix sockets and Windows named pipes require platform-specific implementations and do not fit the Chromium renderer's direct HTTP Arrow path well.

Owner-only files can securely hand connection details and secrets between processes belonging to one user:

- Unix uses owner-only directory and file modes.
- Windows uses the inherited access controls of the user's local application-data directory.

An unprivileged user normally cannot capture another user's loopback traffic, although OS configuration and permissions can affect this (e.g. CAP_NET_RAW, CAP_NET_ADMIN). TLS protects the traffic even when OS restrictions do not.

## Design

By default, CLI commands discover or start an engine daemon for the current OS user. Additional daemons can be started by specifying `--server-port`.

The Apap and Auth services share a loopback gRPC port protected by mTLS. Arrow queries use a separate loopback HTTP port protected by a bearer token. Therefore, gRPC traffic is authenticated and encrypted whilst Arrow traffic is authenticated but not encrypted.

### Rationale

Arrow over HTTP is used to efficiently return bulk data from the engine to the GUI. This is the most performance sensitive path so we choose not to enable encryption because:

- the endpoint is limited to data queries that run within a restricted DuckDB environment
- the connection is local only
- an authorization token is sufficient to isolate OS users

Encryption is more important for gRPC services because they expose more capabilities. The overhead of encryption is less significant because gRPC transfers are relatively small.

The certificate authority is on the host machine, so this design applies to local engine connections. The Apap and Auth services use the same TLS configuration and run in the same engine process. Sharing one gRPC port reduces the complexity.

### Per-user daemon management

The CLI discovers the current user's engine daemon or starts one automatically. It uses:

- dynamic gRPC port allocation using the approach already used by the GUI
- an owner-only `daemon.json` file containing the connection details
- a state lock so concurrent CLI commands cannot start two daemons or modify the metadata concurrently
- a lifetime lock held while the managed daemon is running
- a health check over mTLS to confirm that the discovered daemon is running and belongs to the current user
- removal and replacement of connection details that no longer identify a working daemon

During startup, the child acquires the lifetime lock and writes `daemon.json` while the parent holds the state lock. The parent releases the state lock only after the daemon is ready. If startup fails, the parent removes the matching metadata. The GUI and each MCP session start and own separate daemons.

### gRPC services

The Apap and Auth services share the same port.

- Transport: gRPC with mTLS over loopback TCP
- Authentication: client certificates signed by the per-user certificate authority
- Encryption: TLS

Both services use the same TLS configuration, so a second gRPC listener and port are unnecessary.

### Arrow queries

The GUI (Chromium renderer process) continues to fetch Arrow data directly from the engine over HTTP.

- Transport: HTTP over loopback TCP
- Authentication: in-memory bearer token
- Encryption: none

The engine rejects requests with a missing or invalid token. The token permits Arrow queries only; it cannot be used for gRPC calls.

### Arrow token flow

Electron obtains the token through its protected gRPC connection:

1. The engine generates a random token at startup and keeps it in memory.
2. Electron main connects to the engine using mTLS.
3. Electron requests the Arrow token through the Auth service.
4. Electron supplies the token to the renderer with the Arrow query details.
5. The renderer includes the token with each Arrow request.
6. The token becomes invalid when the daemon exits.

Issuing the token through mTLS prevents another user from obtaining one by finding the gRPC port. Finding the Arrow port is also insufficient without the token.

Arrow traffic remains unencrypted. A process that can capture loopback traffic could read the Arrow data and copy the token.
