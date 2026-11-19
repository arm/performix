<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Timeline identity fixture

`timeline-identities.json` is a reduced metadata export from real NeoProf run
`bfbb611dd351`, schema version `20260924`. It retains the first three process
and device records and first six thread records. It contains no stacks or
workload paths.

The capture's `counter.parquet` files use `unique_thread_id` in their `thread`
column. PID 4454 maps to process identity 1365. TID 4460 maps to thread identity
1332 and contributes counter rows at both 10 ms and 10 s bin durations.
`device_no` maps to the device metadata, whose `cluster_name` is the core type.

The collector's `analysis/utils/arrow_helpers.hpp` exports the system-wide
thread as `0xffffffff`. Idle thread 0 is a separate identity. Synthetic tests
add reused OS IDs, duplicate metadata, missing metadata, empty matches, and
system-wide rows without changing this capture fixture.
