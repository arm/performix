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


class TestCodexSessionMetrics(unittest.TestCase):
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
            self.assertEqual({"tool_rounds": 1, "tool_calls_succeeded": 1, "tool_calls_failed": 0,
                              "tool_duration_seconds_succeeded": 2, "tool_duration_seconds_failed": 0},
                             collect_mcp_tool_metrics(home, "arm-performix"))

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
                collect_mcp_tool_metrics(home, "arm-performix")

    def test_metrics_reject_invalid_duration_status_and_result(self):
        item = {"type": "McpToolCall", "id": "call", "server": "arm-performix", "status": "completed",
                "duration": {"secs": 1, "nanos": 0}, "result": {}}
        for changes, message in (({"duration": {}}, "duration"), ({"status": "unknown"}, "status"),
                                 ({"result": None}, "response")):
            with self.subTest(changes=changes), tempfile.TemporaryDirectory() as tmp:
                _write_session(Path(tmp), {"type": "item_started", "item": item},
                               {"type": "item_completed", "item": {**item, **changes}})
                with self.assertRaisesRegex(RuntimeError, message):
                    collect_mcp_tool_metrics(Path(tmp), "arm-performix")

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

        metrics = collect_mcp_tool_metrics(codex_home, "arm-performix")

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

        metrics = collect_mcp_tool_metrics(codex_home, "arm-performix")

        self.assertEqual(metrics["tool_calls_succeeded"], 3)
        self.assertEqual(metrics["tool_rounds"], 2)
