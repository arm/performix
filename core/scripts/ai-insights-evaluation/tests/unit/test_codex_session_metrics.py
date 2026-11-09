# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for metrics extracted from persisted Codex sessions."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from codex_session_metrics import collect_mcp_tool_metrics, iter_mcp_events


def _write_session(codex_home: Path, *payloads: dict) -> Path:
    session = codex_home / "sessions" / "2026" / "07" / "28" / "session.jsonl"
    session.parent.mkdir(parents=True)
    session.write_text(
        "".join(
            json.dumps(
                {
                    "type": "response_item" if payload["type"] == "function_call" else "event_msg",
                    "payload": payload,
                }
            ) + "\n"
            for payload in payloads
        ),
        encoding="utf-8",
    )
    return session


def _write_exec(codex_home: Path, *events: tuple[str, str]) -> Path:
    path = codex_home / "codex_exec.jsonl"
    path.write_text("".join(json.dumps({"type": f"item.{state}", "item": {
        "type": "mcp_tool_call", "id": call_id, "server": "arm-performix",
    }}) + "\n" for state, call_id in events), encoding="utf-8")
    return path


class TestCodexSessionMetrics(unittest.TestCase):
    def test_clarification_without_mcp_calls_has_zero_metrics(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_session(home, {"type": "message", "role": "assistant", "content": [
                {"type": "output_text", "text": "Which workload should I investigate?"}]})
            self.assertEqual({"tool_rounds": 0, "tool_calls_succeeded": 0, "tool_calls_failed": 0,
                              "tool_duration_seconds_succeeded": 0.0, "tool_duration_seconds_failed": 0.0},
                             collect_mcp_tool_metrics(home, "arm-performix", _write_exec(home)))

    def test_unfinished_mcp_call_is_not_treated_as_zero_usage(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_session(home, {"type": "function_call", "namespace": "mcp__arm_performix",
                                  "name": "run_recipe", "call_id": "unfinished", "arguments": "{}"})
            with self.assertRaisesRegex(RuntimeError, "incomplete"):
                collect_mcp_tool_metrics(home, "arm-performix", _write_exec(home))

    def test_metrics_count_completed_items_without_counting_legacy_duplicates(self):
        item = {"type": "McpToolCall", "id": "call", "server": "arm-performix", "tool": "run_recipe",
                "status": "completed", "duration": {"secs": 2, "nanos": 0}, "result": {}}
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_session(home,
                           {"type": "function_call", "namespace": "mcp__arm_performix", "call_id": "call"},
                           {"type": "item_started", "item": item},
                           {"type": "mcp_tool_call_end", "call_id": "call", "invocation": item},
                           {"type": "item_completed", "item": item})
            raw = _write_exec(home, ("started", "item_1"), ("completed", "item_1"))
            self.assertEqual({"tool_rounds": 1, "tool_calls_succeeded": 1, "tool_calls_failed": 0,
                              "tool_duration_seconds_succeeded": 2, "tool_duration_seconds_failed": 0},
                             collect_mcp_tool_metrics(home, "arm-performix", raw))

    def test_partial_transcript_preserves_numbered_evidence_but_metrics_fail(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            session = _write_session(home, {"type": "mcp_tool_call_begin", "call_id": "call",
                                           "invocation": {"server": "arm-performix", "tool": "run_recipe"}})
            valid = session.read_bytes()
            session.write_bytes(valid + b'\xff\n{"partial":\n' + valid)
            invalid = []
            events = list(iter_mcp_events(session, "arm-performix", invalid_lines=invalid))
            self.assertEqual([1, 4], [number for number, _ in events])
            self.assertEqual([2, 3], invalid)
            with self.assertRaisesRegex(ValueError, "line 2"):
                collect_mcp_tool_metrics(home, "arm-performix", _write_exec(home))

    def test_exec_rounds_do_not_require_session_start_events_or_matching_ids(self):
        item = {"type": "McpToolCall", "server": "arm-performix", "status": "completed",
                "duration": {"secs": 0, "nanos": 250_000_000}, "result": {}}
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_session(home,
                           {"type": "item_completed", "item": {**item, "id": "exec-1"}},
                           {"type": "item_completed", "item": {**item, "id": "exec-2", "status": "failed"}})
            raw = _write_exec(home, ("started", "item_1"), ("started", "item_2"),
                              ("completed", "item_1"), ("completed", "item_2"))
            self.assertEqual({"tool_rounds": 1, "tool_calls_succeeded": 1, "tool_calls_failed": 1,
                              "tool_duration_seconds_succeeded": 0.25, "tool_duration_seconds_failed": 0.25},
                             collect_mcp_tool_metrics(home, "arm-performix", raw))

    def test_exec_events_must_pair_and_match_session_call_count(self):
        start, end = ("started", "item_1"), ("completed", "item_1")
        item = {"type": "McpToolCall", "id": "exec-1", "server": "arm-performix", "status": "completed",
                "duration": {"secs": 1, "nanos": 0}, "result": {}}
        for events, error in (([end], "no matching start"), ([start], "incomplete"),
                              ([start, start, end], "duplicate start"), ([start, end, end], "no matching start"),
                              ([], "differ between logs"),
                              ([start, end, ("started", "item_2"), ("completed", "item_2")], "differ between logs")):
            with self.subTest(events=events), tempfile.TemporaryDirectory() as tmp:
                home = Path(tmp)
                _write_session(home, {"type": "item_completed", "item": item})
                with self.assertRaisesRegex(RuntimeError, error):
                    collect_mcp_tool_metrics(home, "arm-performix", _write_exec(home, *events))

    def test_metrics_reject_invalid_duration_status_and_result(self):
        item = {"type": "McpToolCall", "id": "call", "server": "arm-performix", "status": "completed",
                "duration": {"secs": 1, "nanos": 0}, "result": {}}
        for changes, message in (({"duration": {}}, "duration"), ({"status": "unknown"}, "status"),
                                 ({"result": None}, "response")):
            with self.subTest(changes=changes), tempfile.TemporaryDirectory() as tmp:
                _write_session(Path(tmp), {"type": "item_started", "item": item},
                               {"type": "item_completed", "item": {**item, **changes}})
                with self.assertRaisesRegex(RuntimeError, message):
                    collect_mcp_tool_metrics(Path(tmp), "arm-performix", _write_exec(Path(tmp)))

    def test_collects_mcp_duration_by_outcome(self) -> None:
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        codex_home = Path(directory.name) / "codex_home"
        _write_session(
            codex_home,
            {"type": "function_call", "namespace": "mcp__arm_performix", "call_id": "call-1"},
            {
                "type": "item_completed",
                "item": {
                    "id": "call-1",
                    "type": "McpToolCall",
                    "server": "arm-performix",
                    "tool": "generate_ai_insights",
                    "status": "completed",
                    "duration": {"secs": 0, "nanos": 250_000_000},
                    "result": {},
                },
            },
            {"type": "function_call", "namespace": "mcp__arm_performix", "call_id": "call-2"},
            {
                "type": "item_completed",
                "item": {
                    "id": "call-2",
                    "type": "McpToolCall",
                    "server": "arm-performix",
                    "tool": "generate_ai_insights",
                    "status": "failed",
                    "duration": {"secs": 10, "nanos": 500_000_000},
                },
            },
            {"type": "function_call", "namespace": "mcp__arm_performix", "call_id": "call-3"},
            {
                "type": "item_completed",
                "item": {
                    "id": "call-3",
                    "type": "McpToolCall",
                    "server": "arm-performix",
                    "tool": "generate_ai_insights",
                    "status": "completed",
                    "duration": {"secs": 1, "nanos": 0},
                    "result": {"isError": True},
                },
            },
            {"type": "function_call", "namespace": "mcp__another_server", "call_id": "other-call"},
            {
                "type": "item_completed",
                "item": {
                    "id": "other-call",
                    "type": "McpToolCall",
                    "server": "another-server",
                    "tool": "other",
                    "status": "completed",
                    "duration": {"secs": 30, "nanos": 0},
                    "result": {},
                },
            },
        )

        raw = _write_exec(codex_home, ("started", "item_1"), ("completed", "item_1"),
                          ("started", "item_2"), ("completed", "item_2"),
                          ("started", "item_3"), ("completed", "item_3"))
        metrics = collect_mcp_tool_metrics(codex_home, "arm-performix", raw)

        self.assertEqual(
            metrics,
            {
                "tool_rounds": 3,
                "tool_calls_succeeded": 1,
                "tool_calls_failed": 2,
                "tool_duration_seconds_succeeded": 0.25,
                "tool_duration_seconds_failed": 11.5,
            },
        )

    def test_counts_overlapping_mcp_calls_as_one_round(self) -> None:
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        codex_home = Path(directory.name) / "codex_home"
        _write_session(
            codex_home,
            {"type": "function_call", "namespace": "mcp__arm_performix", "call_id": "call-1"},
            {"type": "function_call", "namespace": "mcp__arm_performix", "call_id": "call-2"},
            {
                "type": "item_completed",
                "item": {
                    "id": "call-1",
                    "type": "McpToolCall",
                    "server": "arm-performix",
                    "status": "completed",
                    "duration": {"secs": 1, "nanos": 0},
                    "result": {},
                },
            },
            {
                "type": "item_completed",
                "item": {
                    "id": "call-2",
                    "type": "McpToolCall",
                    "server": "arm-performix",
                    "status": "completed",
                    "duration": {"secs": 1, "nanos": 0},
                    "result": {},
                },
            },
            {"type": "function_call", "namespace": "mcp__arm_performix", "call_id": "call-3"},
            {
                "type": "item_completed",
                "item": {
                    "id": "call-3",
                    "type": "McpToolCall",
                    "server": "arm-performix",
                    "status": "completed",
                    "duration": {"secs": 1, "nanos": 0},
                    "result": {},
                },
            },
        )

        events = [("started", "item_1"), ("started", "item_2"), ("completed", "item_1"),
                  ("completed", "item_2"), ("started", "item_3"), ("completed", "item_3")]
        raw = _write_exec(codex_home, *events)
        metrics = collect_mcp_tool_metrics(codex_home, "arm-performix", raw)

        self.assertEqual(metrics["tool_calls_succeeded"], 3)
        self.assertEqual(metrics["tool_rounds"], 2)

        # The same model-request batch can execute sequentially instead.
        events[1], events[2] = events[2], events[1]
        raw = _write_exec(codex_home, *events)
        self.assertEqual(3, collect_mcp_tool_metrics(codex_home, "arm-performix", raw)["tool_rounds"])
