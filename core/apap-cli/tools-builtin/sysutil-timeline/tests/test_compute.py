# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from compute import iowait_percent, rate_from_delta
from sources import CpuSnapshot


def test_rate_from_delta() -> None:
    assert rate_from_delta(10, 20, 2.0) == 5.0
    assert rate_from_delta(10, 10, 2.0) == 0.0
    assert rate_from_delta(20, 10, 2.0) == 0.0
    assert rate_from_delta(10, 20, 0.0) == 0.0


def test_iowait_percent_guardrails() -> None:
    assert iowait_percent(
        CpuSnapshot(total=100, idle=50, iowait=10),
        CpuSnapshot(total=100, idle=60, iowait=20),
    ) == 0.0
    assert iowait_percent(
        CpuSnapshot(total=100, idle=50, iowait=10),
        CpuSnapshot(total=120, idle=60, iowait=5),
    ) == 0.0
    assert iowait_percent(
        CpuSnapshot(total=100, idle=50, iowait=10),
        CpuSnapshot(total=120, idle=60, iowait=40),
    ) == 100.0
