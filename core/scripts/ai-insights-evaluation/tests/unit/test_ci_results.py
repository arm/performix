# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import ci_results
from codex_session_metrics import persist_codex_session_log
from create_ai_insights_results_archive import attempt_result_entries
from pathways_artifacts import preserve_pathways_artifacts


class CiResultsTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.results = self.root / "results"
        self.destination = self.root / "upload"
        self.destination.mkdir()

    def write(self, relative, content="evidence"):
        path = self.results / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def test_interrupted_pathways_capture_excludes_secrets_and_preserves_non_pathways_files(self):
        prefix = "invocations/one/path/performix_mcp/attempts/001/"
        for name in ("codex_session.jsonl", "score.json", "codex_exec.jsonl", "codex_stderr.txt", "agent_home/.ssh/target_key",
                     "performix_state/data/run.db", "codex_home/auth.json", "codex_workspace/main.c",
                     "codex_stdout.txt", "other.json"):
            self.write(prefix + name)
        # No completed-capture marker exists: classification must use the manifest.
        self.write("invocations/one/ordinary/performix_mcp/attempts/001/nested/details.txt")
        self.write("invocations/one/ordinary/performix_mcp/attempts/001/response.json")
        self.write("ordinary/performix_mcp/attempts/001/codex_workspace/main.c")
        self.write("ordinary/performix_mcp/attempts/001/performix_state/data/run.db")
        self.write("ordinary/performix_mcp/attempts/001/agent_home/.config/settings")
        self.write("imported_sources/source.c")
        ci_results.stage_results(self.results, self.destination, {"tests": [
            {"id": "path", "pathways_execution": {"mode": "host_ssh"}}, {"id": "ordinary"}]})
        copied = {p.relative_to(self.destination / "results").as_posix()
                  for p in self.destination.rglob("*") if p.is_file()}
        self.assertEqual(copied, {prefix + "codex_session.jsonl", prefix + "score.json",
                         prefix + "codex_exec.jsonl", prefix + "codex_stderr.txt",
                         "invocations/one/ordinary/performix_mcp/attempts/001/nested/details.txt",
                         "invocations/one/ordinary/performix_mcp/attempts/001/response.json"})

    def test_cli_stages_reports_once_and_only_prerecorded_metadata(self):
        reports = {"ai-insights-evaluation.xml": "combined",
                   "ai-insights-performance-dashboard.json": "[]",
                   "graphs/index.html": "graph", "payload/metadata.json": "{}"}
        for name, content in reports.items():
            self.write("reporting/" + name, content)
        inputs = self.root / "inputs/run"
        inputs.mkdir(parents=True)
        (inputs / "metadata.json").write_text("{}")
        (inputs / "run.db").write_text("input data")
        manifest = self.root / "manifest.json"
        manifest.write_text('{"tests": []}')
        self.destination.rmdir()
        with patch("sys.argv", ["ci_results", "--results-dir", str(self.results),
                                "--destination", str(self.destination), "--manifest", str(manifest),
                                "--run-artifact-base", str(inputs.parent)]):
            ci_results.main()
        copied = {p.relative_to(self.destination).as_posix(): p.read_text()
                  for p in self.destination.rglob("*") if p.is_file()}
        self.assertEqual(copied, {**{"results/reporting/" + k: v for k, v in reports.items()},
                                  "prerecorded_runs/run/metadata.json": "{}"})

    def test_missing_junit_does_not_prevent_partial_evidence_staging(self):
        self.write("path/performix_mcp/attempts/001/score.json")
        ci_results.stage_results(self.results, self.destination, {"tests": [
            {"id": "path", "pathways_execution": {"mode": "host_ssh"}}]})
        ci_results.stage_prerecorded_metadata(self.root / "missing-inputs", self.destination)
        self.assertTrue((self.destination / "results/path/performix_mcp/attempts/001/score.json").is_file())
        self.assertFalse((self.destination / "results/reporting").exists())

    def test_multiple_session_transcripts_survive_cleanup_staging_and_archiving(self):
        prefix = "path/performix_mcp/attempts/001/"
        attempt = self.write(prefix + "codex_session_notes.jsonl").parent
        transcripts = {f"codex_session_{i:03d}.jsonl": f'{{"session": {i}}}\n' for i in range(2)}
        for name, content in transcripts.items():
            self.write(prefix + "codex_home/sessions/" + name, content)
        with self.assertRaisesRegex(RuntimeError, "found 2"):
            persist_codex_session_log(attempt / "codex_home", attempt)

        preserve_pathways_artifacts(attempt, [])
        manifest = {"tests": [{"id": "path", "pathways_execution": True}]}
        ci_results.stage_results(self.results, self.destination, manifest)
        entries = attempt_result_entries(self.destination / "results", manifest)
        self.assertEqual(set(transcripts) | {"performix_runs.json"}, {entry.source.name for entry in entries})
        for name, content in transcripts.items():
            self.assertEqual(content, (self.destination / "results" / prefix / name).read_text())
