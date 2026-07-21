<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Run

## File Locks

Adjacent to the run directory, we have the `locks` directory. This directory
stores file locks that are used to synchronise operations related to runs.
The file locks rely on the underlying operating system's syscalls /capabilities.
On Linux/macOS it's `flock(2)` and on Windows it's `LockFileEx()`.

These file locks are advisory. Meaning it is still possible to write code that
modifies the run directory (or whatever in it). It is up to the developers to
be aware and make use of them whenever required.

### `lockfile`

This file lock is used to synchronise access to a run directory on a gRPC API
level. These are, for example, `run update`, `run rename` or `run delete`.

### `leasefile`

This file lock is used to indicate whether a run is still running or not
(i.e. liveliness). The recovery system relies on it to avoid false positives
when recovering a stale run.

It is locked only once during the lifetime of a run - while it is executing.
