# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import json
import tempfile
import unittest
from pathlib import Path

import create_ai_insights_results_archive as archives
import pathways_artifacts as artifacts

class InvestigationArtifactTests(unittest.TestCase):
    def test_recipe_run_id_requires_successful_completion(self):
        completed = {"run_id": "fixture", "run_status": "completed"}
        for result, expected in (
            (completed, "fixture"),
            ({**completed, "isError": True}, None),
            ({**completed, "run_status": "failed"}, None),
            ({"run_id": "fixture"}, None),
            ({**completed, "error": {"message": "failed"}}, None),
        ):
            with self.subTest(result=result):
                call = {"completed_event": True, "arguments": {}, "result": {"Ok": result}}
                self.assertEqual(expected, artifacts.mcp_recipe_run_id(call))

    def test_pathways_retains_only_contract_and_exact_recipe_arguments(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            attempt = root / "case" / "performix_mcp" / "attempts" / "001"
            attempt.mkdir(parents=True)
            arguments = {"recipe": "code_hotspots", "target": "target-1", "timeout": 20,
                         "launch": {"command": "./workload --size 10", "working_dir": "/workload"},
                         "parameters": {"frequency": 123}}
            events = [
                {"payload": {"type": "function_call", "namespace": "mcp__arm_performix",
                             "name": "run_recipe", "call_id": "run-1", "arguments": json.dumps(arguments)}},
                {"payload": {"type": "mcp_tool_call_end", "call_id": "run-1",
                             "invocation": {"server": "arm-performix", "tool": "run_recipe", "arguments": arguments},
                             "result": {"Ok": {"run_id": "123", "run_status": "completed"}}}},
            ]
            contents = b"".join(json.dumps(e).encode() + b"\n" for e in events) + b'{"partial":'
            (attempt / "codex_session.jsonl").write_bytes(contents)
            for name in ("workload_history.pack", "workload_final.zip"):
                (attempt / name).write_text("discard")
            retained = {"codex_last_message.txt", "llm_response.md", "score.json", "score.md",
                        "codex_exec.jsonl", "codex_stderr.txt"}
            for name in retained:
                (attempt / name).write_text(f"preserve {name}")
            artifacts.preserve_pathways_artifacts(attempt, [])
            self.assertEqual({"codex_session.jsonl", "performix_runs.json"} | retained,
                             {p.name for p in attempt.iterdir()})
            for name in retained:
                self.assertEqual(f"preserve {name}", (attempt / name).read_text())
            self.assertEqual(contents, (attempt / "codex_session.jsonl").read_bytes())
            runs = json.loads((attempt / "performix_runs.json").read_text())
            self.assertEqual(arguments, runs["runs"][0]["mcp_calls"][0]["arguments"])
            self.assertEqual(1, len(runs["runs"]))
            self.assertEqual([3], runs["invalid_session_lines"])

    def test_mcp_repetitions_and_failed_calls_are_preserved_without_run_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            events = []
            for call_id, result in (("one", {"structuredContent": {"run_id": "1", "run_status": "completed"}}),
                                    ("two", {"content": [{"type": "text", "text": '{"run_id":"2","run_status":"completed"}'}]}),
                                    ("failed", {"isError": True, "structuredContent": {"run_id": "3"}})):
                events.append({"payload": {"type": "mcp_tool_call_end", "call_id": call_id,
                    "invocation": {"server": "arm-performix", "tool": "run_recipe",
                                   "arguments": {"recipe": "code_hotspots", "parameters": {"frequency": 150}}},
                    "result": {"Ok": result}}})
            (root / "codex_session.jsonl").write_text("\n".join(json.dumps(e) for e in events))
            artifacts.preserve_pathways_artifacts(root, [])
            record = json.loads((root / "performix_runs.json").read_text())
            self.assertEqual(["1", "2"], [run["run_id"] for run in record["runs"]])
            self.assertEqual("failed", record["unmatched_mcp_calls"][0]["call_id"])
            for run in record["runs"]:
                self.assertEqual({"frequency": 150}, run["mcp_calls"][0]["arguments"]["parameters"])
            self.assertIn("not proof", record["coverage"])

    def test_item_events_preserve_recipe_evidence_and_reject_unsuccessful_results(self):
        arguments = {"recipe": "code_hotspots", "target": "target-1"}
        result = {"structuredContent": {"run_id": "123", "run_status": "completed"}}
        for status, response, expected in (
            ("completed", result, "123"), ("failed", None, None),
            ("completed", {**result, "isError": True}, None),
            ("completed", {**result, "is_error": True}, None),
            ("in_progress", None, None),
        ):
            with self.subTest(status=status, response=response), tempfile.TemporaryDirectory() as tmp:
                item = {"type": "McpToolCall", "id": "call-1", "server": "arm-performix",
                        "tool": "run_recipe", "arguments": arguments}
                events = [{"payload": {"type": "item_started", "item": item}}]
                if status != "in_progress":
                    events.append({"payload": {"type": "item_completed", "item": {
                        **item, "status": status, "result": response}}})
                session = Path(tmp) / "session.jsonl"
                session.write_text("\n".join(json.dumps(event) for event in events))
                calls, invalid = artifacts.mcp_recipe_calls(session)
                self.assertEqual([], invalid)
                self.assertEqual(1, len(calls))
                self.assertEqual(arguments, calls[0]["arguments"])
                self.assertEqual(expected, artifacts.mcp_recipe_run_id(calls[0]))

    def test_missing_transcript_does_not_claim_complete_run_coverage(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifacts.preserve_pathways_artifacts(root, [])
            record = json.loads((root / "performix_runs.json").read_text())
            self.assertEqual([], record["runs"])
            self.assertIn("not proof", record["coverage"])

    def test_session_fallback_preserves_raw_codex_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            contents = b'{"type":"thread.started"}\n{"partial":'
            (root / "codex_exec.jsonl").write_bytes(contents)
            artifacts.preserve_pathways_artifacts(root, [])
            for name in ("codex_exec.jsonl", "codex_session.jsonl"):
                self.assertEqual(contents, (root / name).read_bytes())

    def test_archive_filters_pathways_but_preserves_ordinary_attempts(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            expected = set()
            for prefix, case in (("", "path"), ("invocations/first", "path"), ("invocations/second", "ordinary")):
                run = root / prefix
                attempt = run / case / "performix_mcp/attempts/001"
                (attempt / "codex_home").mkdir(parents=True)
                # No completed-capture marker: the manifest must classify this attempt.
                for name in ("score.json", "llm_response.md", "codex_exec.jsonl", "codex_stderr.txt",
                             "bootstrap-details.txt", "codex_home/auth.json"):
                    (attempt / name).write_text("evidence")
                expected.update(attempt / name for name in
                                ("score.json", "llm_response.md", "codex_exec.jsonl", "codex_stderr.txt"))
                if case == "ordinary":
                    expected.add(attempt / "bootstrap-details.txt")
            entries = archives.attempt_result_entries(root, {
                "tests": [{"id": "path", "pathways_execution": True}, {"id": "ordinary"}],
            })
            self.assertEqual(expected, {entry.source for entry in entries})
