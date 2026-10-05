# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import logging
from unittest.mock import patch

from pytorch_collect.tracker import TraceTracker
from pytorch_collect.writer import Writer


class MockWriter(Writer):
    def __init__(self):
        self.api_calls = []

    def write_api_call(self, api_call):
        self.api_calls.append(api_call)


def assert_warning_logged(caplog, message):
    assert any(
        record.levelno == logging.WARNING and record.message == message
        for record in caplog.records
    )


def test_records_api_call_with_operator_call_and_timestamps():
    writer = MockWriter()
    tracker = TraceTracker(writer)

    with patch("pytorch_collect.tracker.time_monotnonic_raw", side_effect=[100, 110, 150, 200]):
        api_call_id = tracker.begin_api_call(
            "add", [{"type": "Tensor"}], {"alpha": {"value": 1}}
        )
        operator_call_id = tracker.begin_operator_call(
            "aten.add.Tensor",
            [{"type": "Tensor"}],
            {"alpha": {"value": 1}},
        )
        tracker.end_operator_call(operator_call_id, {"type": "Tensor"})
        tracker.end_api_call(api_call_id, {"type": "Tensor"})

    assert len(writer.api_calls) == 1

    api_call = writer.api_calls[0]
    assert api_call.id == 0
    assert api_call.name == "add"
    assert api_call.timestamp_begin == 100
    assert api_call.timestamp_end == 200
    assert api_call.args == [{"type": "Tensor"}]
    assert api_call.kwargs == {"alpha": {"value": 1}}
    assert api_call.output == {"type": "Tensor"}
    assert len(api_call.operator_calls) == 1

    operator_call = api_call.operator_calls[0]
    assert operator_call.id == 0
    assert operator_call.name == "aten.add.Tensor"
    assert operator_call.timestamp_begin == 110
    assert operator_call.timestamp_end == 150
    assert operator_call.args == [{"type": "Tensor"}]
    assert operator_call.kwargs == {"alpha": {"value": 1}}
    assert operator_call.output == {"type": "Tensor"}


def test_begin_api_call_replaces_existing_api_call_and_warns(caplog):
    writer = MockWriter()
    tracker = TraceTracker(writer)

    with patch("pytorch_collect.tracker.time_monotnonic_raw", side_effect=[100, 200]):
        old_id = tracker.begin_api_call("old", [], {})
        new_id = tracker.begin_api_call("new", [{"type": "Tensor"}], {})

    assert old_id == 0
    assert new_id == 1
    assert_warning_logged(caplog, "Resetting tracked API call!")
    assert tracker.api_call.name == "new"
    assert tracker.api_call.timestamp_begin == 200
    assert tracker.api_call.args == [{"type": "Tensor"}]
    assert tracker.api_call.kwargs == {}
    assert writer.api_calls == []


def test_end_api_call_without_api_call_warns_and_does_not_write(caplog):
    writer = MockWriter()
    tracker = TraceTracker(writer)

    tracker.end_api_call(0, {"type": "Tensor"})

    assert_warning_logged(caplog, "No tracked API call!")
    assert writer.api_calls == []


def test_begin_operator_call_without_api_call_warns_and_does_not_start_operator_call(caplog):
    tracker = TraceTracker(MockWriter())

    operator_call_id = tracker.begin_operator_call("aten.add.Tensor", [], {})

    assert_warning_logged(caplog, "No tracked API call!")
    assert operator_call_id == -1
    assert tracker.operator_call is None


def test_end_operator_call_without_api_call_warns(caplog):
    writer = MockWriter()
    tracker = TraceTracker(writer)

    tracker.end_operator_call(0, {"type": "Tensor"})

    assert_warning_logged(caplog, "No tracked API call!")
    assert tracker.operator_call is None
    assert writer.api_calls == []


def test_end_operator_call_without_operator_call_warns_and_keeps_api_call(caplog):
    writer = MockWriter()
    tracker = TraceTracker(writer)

    with patch("pytorch_collect.tracker.time_monotnonic_raw", return_value=100):
        tracker.begin_api_call("add", [], {})

    tracker.end_operator_call(0, {"type": "Tensor"})

    assert_warning_logged(caplog, "No tracked operator call!")
    assert tracker.api_call.name == "add"
    assert tracker.api_call.operator_calls == []
    assert writer.api_calls == []


def test_end_api_call_with_stale_id_warns_and_keeps_current_call(caplog):
    writer = MockWriter()
    tracker = TraceTracker(writer)

    with patch("pytorch_collect.tracker.time_monotnonic_raw", side_effect=[100, 200]):
        stale_id = tracker.begin_api_call("old", [], {})
        current_id = tracker.begin_api_call("new", [], {})

    tracker.end_api_call(stale_id, {"type": "Tensor"})

    assert_warning_logged(
        caplog,
        f"API call ID mismatch: expected {current_id}, got {stale_id}!",
    )
    assert tracker.api_call.id == current_id
    assert tracker.api_call.name == "new"
    assert writer.api_calls == []


def test_end_operator_call_with_stale_id_warns_and_keeps_current_call(caplog):
    tracker = TraceTracker(MockWriter())

    with patch("pytorch_collect.tracker.time_monotnonic_raw", side_effect=[100, 110, 120]):
        tracker.begin_api_call("add", [], {})
        stale_id = tracker.begin_operator_call("aten.old", [], {})
        current_id = tracker.begin_operator_call("aten.new", [], {})

    tracker.end_operator_call(stale_id, {"type": "Tensor"})

    assert_warning_logged(
        caplog,
        f"Operator call ID mismatch: expected {current_id}, got {stale_id}!",
    )
    assert tracker.operator_call.id == current_id
    assert tracker.operator_call.name == "aten.new"
    assert tracker.api_call.operator_calls == []
