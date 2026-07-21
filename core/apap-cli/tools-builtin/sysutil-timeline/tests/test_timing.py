# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from collector import TickScheduler


class FakeClock:
    def __init__(self, now: float, step: float) -> None:
        self.now = now
        self.step = step
        self.slept: list[float] = []

    def monotonic(self) -> float:
        self.now += self.step
        return self.now

    def sleep(self, duration: float) -> None:
        self.slept.append(duration)
        self.now += duration


def test_first_tick_waits_full_interval() -> None:
    clock = FakeClock(now=100.0, step=0.001)
    interval = 1.0
    scheduler = TickScheduler.start(interval=interval, now=clock.monotonic())

    _tick_time, elapsed = scheduler.wait_for_tick(clock.monotonic, clock.sleep)

    assert elapsed >= interval
    assert clock.slept


def test_scheduler_waits_each_tick() -> None:
    clock = FakeClock(now=0.0, step=0.0)
    interval = 1.0
    scheduler = TickScheduler.start(interval=interval, now=clock.monotonic())

    _tick_time, elapsed1 = scheduler.wait_for_tick(clock.monotonic, clock.sleep)
    _tick_time, elapsed2 = scheduler.wait_for_tick(clock.monotonic, clock.sleep)

    assert elapsed1 == interval
    assert elapsed2 == interval
    assert clock.slept == [interval, interval]


def test_scheduler_skips_sleep_when_behind() -> None:
    clock = FakeClock(now=100.0, step=2.0)
    interval = 1.0
    scheduler = TickScheduler.start(interval=interval, now=clock.monotonic())

    _tick_time, elapsed = scheduler.wait_for_tick(clock.monotonic, clock.sleep)

    assert elapsed == 4.0
    assert not clock.slept
