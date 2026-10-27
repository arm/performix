<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# CPU discovery fixtures

These fixtures retain CPU topology, available cycle counters and matching event
categories from `sl-record --print counters.xml` and `--print events.xml`.
Other events were removed; an unrelated CPU category tests that event definitions
do not imply counter availability.

One representative catalogue is retained for each platform:

- Arm Linux: Neoverse-V2.
- x86 Linux: AMD EPYC (Perf Hardware).
- Android: Pixel 7 (A55/A78/X1), covering multiple CPU groups. Its catalogue came
  from the packaged 2.2.0 collector, build 37dac04.

`expected` records the native identity, numeric event and CPU membership. These
are collector-resolution tests; product support is not inferred from phone names.
