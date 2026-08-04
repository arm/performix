# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import pytest

from collector import (
    INSUFFICIENT_SAMPLES_EXIT_CODE,
    TickScheduler,
    _has_sufficient_collection_duration,
    main,
)


class FakeClock:
    def __init__(self, now: float, step: float) -> None:
        self.now = now
        self.step = step
        self.slept: list[float] = []

    def monotonic(self) -> float:
        self.now += self.step
        return self.now

    def wait(self, duration: float) -> bool:
        self.slept.append(duration)
        self.now += duration
        return False


def test_first_tick_waits_full_interval() -> None:
    clock = FakeClock(now=100.0, step=0.001)
    interval = 1.0
    scheduler = TickScheduler.start(interval=interval, now=clock.monotonic())

    tick = scheduler.wait_for_tick(clock.monotonic, clock.wait)

    assert tick is not None
    _tick_time, elapsed = tick
    assert elapsed >= interval
    assert clock.slept


def test_scheduler_waits_each_tick() -> None:
    clock = FakeClock(now=0.0, step=0.0)
    interval = 1.0
    scheduler = TickScheduler.start(interval=interval, now=clock.monotonic())

    tick1 = scheduler.wait_for_tick(clock.monotonic, clock.wait)
    tick2 = scheduler.wait_for_tick(clock.monotonic, clock.wait)

    assert tick1 is not None
    assert tick2 is not None
    _tick_time, elapsed1 = tick1
    _tick_time, elapsed2 = tick2
    assert elapsed1 == interval
    assert elapsed2 == interval
    assert clock.slept == [interval, interval]


def test_scheduler_skips_sleep_when_behind() -> None:
    clock = FakeClock(now=100.0, step=2.0)
    interval = 1.0
    scheduler = TickScheduler.start(interval=interval, now=clock.monotonic())

    tick = scheduler.wait_for_tick(clock.monotonic, clock.wait)

    assert tick is not None
    _tick_time, elapsed = tick
    assert elapsed == 4.0
    assert not clock.slept


def test_scheduler_stops_during_long_wait() -> None:
    clock = FakeClock(now=0.0, step=0.0)
    scheduler = TickScheduler.start(interval=500.0, now=clock.monotonic())

    def request_stop(duration: float) -> bool:
        clock.slept.append(duration)
        return True

    tick = scheduler.wait_for_tick(clock.monotonic, request_stop)

    assert tick is None
    assert clock.slept == [500.0]


def test_scheduler_stops_at_deadline_before_next_tick() -> None:
    clock = FakeClock(now=0.0, step=0.0)
    scheduler = TickScheduler.start(interval=500.0, now=clock.monotonic())

    tick = scheduler.wait_for_tick(
        clock.monotonic,
        clock.wait,
        deadline=10.0,
    )

    assert tick is None
    assert clock.slept == [10.0]


def test_scheduler_collects_boundary_tick_after_late_wake() -> None:
    clock = FakeClock(now=0.0, step=0.001)
    interval = 1.0
    start = clock.monotonic()
    deadline = start + interval
    scheduler = TickScheduler.start(interval=interval, now=start)

    tick = scheduler.wait_for_tick(
        clock.monotonic,
        clock.wait,
        deadline=deadline,
    )

    assert tick is not None
    tick_time, elapsed = tick
    assert tick_time > deadline
    assert elapsed >= interval


def test_scheduler_includes_boundary_tick_despite_float_accumulation() -> None:
    clock = FakeClock(now=0.0, step=0.0)
    scheduler = TickScheduler.start(interval=0.1, now=clock.monotonic())

    ticks = [
        scheduler.wait_for_tick(clock.monotonic, clock.wait, deadline=0.3)
        for _ in range(3)
    ]

    assert all(tick is not None for tick in ticks)
    assert scheduler.wait_for_tick(
        clock.monotonic,
        clock.wait,
        deadline=0.3,
    ) is None


@pytest.mark.parametrize(
    ("interval", "duration", "is_sufficient"),
    [
        pytest.param(30.0, 0.0, True, id="unlimited-duration"),
        pytest.param(15.0, 30.0, True, id="exactly-two-samples"),
        pytest.param(15.01, 30.0, False, id="less-than-two-samples"),
        pytest.param(30.0, 30.0, False, id="one-sample"),
    ],
)
def test_minimum_collection_duration(
    interval: float,
    duration: float,
    is_sufficient: bool,
) -> None:
    assert (
        _has_sufficient_collection_duration(interval, duration)
        is is_sufficient
    )


def test_main_rejects_interval_outside_supported_range() -> None:
    assert main(["--interval", "0.001"]) == 2
    assert main(["--interval", "60.01"]) == 2
    assert main(["--interval", "inf"]) == 2


def test_main_rejects_duration_that_cannot_produce_two_samples(
    capsys,
) -> None:
    result = main(["--interval", "30", "--duration", "30"])

    assert result == INSUFFICIENT_SAMPLES_EXIT_CODE
    assert "at least 60 seconds" in capsys.readouterr().err
