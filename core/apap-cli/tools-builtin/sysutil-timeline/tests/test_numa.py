# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from types import SimpleNamespace

from collector import (
    _aggregate_numa_values,
    _build_header,
    _numa_node_ids_in_stable_order,
)
from sources import NumaStats, parse_numastat_output, read_numastat


NUMASTAT_SAMPLE = """
                           node0           node1
numa_hit              100             200
numa_miss             10              20
interleave_hit        0               4
local_node            80              120
other_node            5               15
"""


def test_parse_numastat_output_aggregates_nodes() -> None:
    stats = parse_numastat_output(NUMASTAT_SAMPLE)

    assert stats is not None
    assert stats.counters == {
        "numa_hit": 300,
        "numa_miss": 30,
        "interleave_hit": 4,
        "local_node": 200,
        "other_node": 20,
    }
    assert stats.node_allocations == {
        "node0": 85,
        "node1": 135,
    }


def test_parse_numastat_output_handles_single_node() -> None:
    stats = parse_numastat_output(
        """
                           node0
numa_hit              100
numa_miss             10
interleave_hit        0
local_node            80
other_node            5
"""
    )

    assert stats is not None
    assert stats.counters["numa_hit"] == 100
    assert stats.node_allocations == {"node0": 85}


def test_numastat_many_nodes_drive_header_columns_in_numeric_order() -> None:
    stats = parse_numastat_output(
        """
                           node0           node1           node2           node10
numa_hit              100             200             300             400
numa_miss             10              20              30              40
interleave_hit        0               4               8               12
local_node            80              120             160             200
other_node            5               15              25              35
"""
    )

    assert stats is not None
    node_ids = _numa_node_ids_in_stable_order(stats.node_allocations)
    header = _build_header(
        cpu_ids=["cpu"],
        disk_devs=[],
        net_ifaces=[],
        numa_node_ids=node_ids,
    )

    assert node_ids == ["node0", "node1", "node2", "node10"]
    assert [
        column
        for column in header
        if column.startswith("numa_node")
        and column.endswith("_allocations_per_s")
    ] == [
        "numa_node0_allocations_per_s",
        "numa_node1_allocations_per_s",
        "numa_node2_allocations_per_s",
        "numa_node10_allocations_per_s",
    ]


def test_read_numastat_returns_none_when_missing_or_failed() -> None:
    def missing(_args):
        raise FileNotFoundError("numastat")

    def failed(_args):
        return SimpleNamespace(returncode=1, stdout="")

    assert read_numastat(command_runner=missing) is None
    assert read_numastat(command_runner=failed) is None


def test_parse_numastat_output_returns_none_for_unsupported_output() -> None:
    assert parse_numastat_output("not default numastat output") is None
    assert parse_numastat_output(NUMASTAT_SAMPLE.replace("numa_miss", "bad_metric")) is None


def test_aggregate_numa_values_use_interval_deltas() -> None:
    prev = NumaStats(
        counters={
            "numa_hit": 100,
            "numa_miss": 10,
            "interleave_hit": 1,
            "local_node": 80,
            "other_node": 20,
        },
        node_allocations={},
    )
    curr = NumaStats(
        counters={
            "numa_hit": 140,
            "numa_miss": 16,
            "interleave_hit": 3,
            "local_node": 110,
            "other_node": 30,
        },
        node_allocations={},
    )

    assert _aggregate_numa_values(prev, curr, 2.0) == [
        "20.00",
        "3.00",
        "1.00",
        "15.00",
        "5.00",
        "75.00",
        "25.00",
    ]


def test_numa_node_ids_sort_by_numeric_suffix() -> None:
    assert _numa_node_ids_in_stable_order({
        "node10": 0,
        "node2": 0,
        "node0": 0,
        "node1": 0,
    }) == ["node0", "node1", "node2", "node10"]


def test_aggregate_numa_values_are_empty_when_unavailable_or_no_locality_delta() -> None:
    stats = NumaStats(
        counters={
            "numa_hit": 100,
            "numa_miss": 10,
            "interleave_hit": 1,
            "local_node": 80,
            "other_node": 20,
        },
        node_allocations={},
    )

    assert _aggregate_numa_values(None, stats, 1.0) == ["", "", "", "", "", "", ""]
    assert _aggregate_numa_values(stats, stats, 1.0) == [
        "0.00",
        "0.00",
        "0.00",
        "0.00",
        "0.00",
        "",
        "",
    ]
