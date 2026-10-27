# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Tests for the GitHub Actions Perfetto trace converter."""

import argparse
import asyncio
import json
import os
import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError

from fastcore.foundation import L
from perfetto.protos.perfetto.trace.perfetto_trace_pb2 import Trace, TrackEvent

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "github"))

from actions_perfetto_trace import (  # noqa: E402
    GitHub,
    TraceWriter,
    TraceError,
    TraceOutput,
    WorkflowUnavailable,
    anchor_from_event,
    append_trace,
    build_trace,
    dependencies,
    load_source,
    match_jobs,
    observed_critical_path,
    parse_event_payload,
    parse_run,
    triggered_runs,
    workflow_definitions,
)


WORKFLOW = """
jobs:
  gate:
    uses: ./.github/workflows/gate.yml
  test:
    name: Test (${{ matrix.os }}, ${{ matrix.go-module }})
    needs: gate
    strategy:
      matrix:
        os: [linux, windows]
        go-module: [cli]
    steps:
      - run: echo test
  report:
    needs: [test]
"""

CHILD = """
jobs:
  check:
    name: Check Gate
    runs-on: ubuntu-latest
    steps:
      - run: echo ready
"""


def job(job_id, name, start, end):
    return {
        "id": job_id,
        "name": name,
        "created_at": start,
        "started_at": start,
        "completed_at": end,
        "status": "completed",
        "conclusion": "success",
        "html_url": f"https://github.com/example/repo/actions/jobs/{job_id}",
        "steps": [],
    }


class FakeGitHub:
    repository = "example/repo"

    async def workflow_text(self, path, sha):
        assert path == ".github/workflows/gate.yml"
        return CHILD

    async def runs(self, **query):
        return self.run_list

    async def pull_requests_for_commit(self, sha):
        return self.associated_pull_requests[sha]


def trace_parts(output):
    packets = Trace.FromString(output.data).packet
    descriptors = {packet.track_descriptor.uuid: packet.track_descriptor
                   for packet in packets if packet.HasField("track_descriptor")}
    events = [packet for packet in packets if packet.HasField("track_event")]
    return descriptors, events


def annotations(packet):
    return {item.name: item for item in packet.track_event.debug_annotations}


def slices_on_track(packets, track_uuid):
    stack = []
    slices = {}
    for packet in packets:
        event = packet.track_event
        if event.track_uuid != track_uuid:
            continue
        if event.type == TrackEvent.TYPE_SLICE_BEGIN:
            stack.append((event.name, packet.timestamp, packet))
        elif event.type == TrackEvent.TYPE_SLICE_END:
            name, start, begin = stack.pop()
            slices[name] = (start, packet.timestamp, begin)
    return slices


class PerfettoTraceTests(unittest.TestCase):
    def setUp(self):
        self.run = {
            "id": 42,
            "name": "CI",
            "event": "merge_group",
            "head_sha": "abc123",
            "head_branch": "queue/test",
            "path": ".github/workflows/ci.yml",
            "status": "completed",
            "conclusion": "success",
            "created_at": "2026-10-07T10:00:00Z",
            "updated_at": "2026-10-07T10:00:40Z",
            "html_url": "https://github.com/example/repo/actions/runs/42",
        }
        self.jobs = [
            job(1, "gate / Check Gate", "2026-10-07T10:00:01Z", "2026-10-07T10:00:03Z"),
            job(2, "Test (linux, cli)", "2026-10-07T10:00:04Z", "2026-10-07T10:00:20Z"),
            job(3, "Test (windows, cli)", "2026-10-07T10:00:05Z", "2026-10-07T10:00:30Z"),
            job(4, "report", "2026-10-07T10:00:31Z", "2026-10-07T10:00:35Z"),
        ]
        self.definitions = asyncio.run(workflow_definitions(
            WORKFLOW, self.run["path"], self.run["head_sha"], FakeGitHub(), {}
        ))

    def test_reusable_job_matrix_and_needs_expand_to_observed_jobs(self):
        matched, warnings = match_jobs(self.jobs, self.definitions)
        self.assertEqual(warnings, [])
        self.assertEqual(matched[1].matrix, {"os": "linux", "go-module": "cli"})
        self.assertEqual(
            dependencies(matched, self.definitions),
            {(1, 2), (1, 3), (2, 4), (3, 4)},
        )
        self.assertEqual(
            observed_critical_path(self.jobs, dependencies(matched, self.definitions)),
            [1, 3, 4],
        )

    def test_critical_path_uses_displayed_job_start(self):
        gate = job(1, "gate / Check Gate",
                   "2026-10-07T10:00:01Z", "2026-10-07T10:00:05Z")
        test = job(2, "Test (linux, cli)",
                   "2026-10-07T10:00:04Z", "2026-10-07T10:00:20Z")
        test["created_at"] = "2026-10-07T10:00:06Z"
        report = job(4, "report",
                     "2026-10-07T10:00:21Z", "2026-10-07T10:00:25Z")
        jobs = [gate, test, report]
        edges = {(1, 2), (2, 4)}
        self.assertEqual(observed_critical_path(jobs, edges), [1, 2, 4])

        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, packets = trace_parts(output)
        job_track = next(item for item in descriptors.values()
                         if item.name == test["name"])
        critical_group = next(item for item in descriptors.values()
                              if item.name == "[meta] Observed critical paths")
        critical_track = next(item for item in descriptors.values()
                              if item.parent_uuid == critical_group.uuid)
        for track in (job_track, critical_track):
            self.assertEqual(slices_on_track(packets, track.uuid)[test["name"]][:2],
                             (6_000_000_000, 20_000_000_000))

    def test_large_workflow_elapsed_span_reaches_last_job(self):
        jobs = [job(number, f"Job {number}", "2026-10-07T10:00:01Z",
                    "2026-10-07T10:00:10Z") for number in range(1, 9)]
        jobs.append(job(9, "Late job", "2026-10-07T10:00:01Z",
                        "2026-10-07T10:00:40Z"))
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: jobs}, {self.run["id"]: []},
        )
        descriptors, packets = trace_parts(output)
        workflow = next(item for item in descriptors.values()
                        if item.name == "CI (42)")
        self.assertEqual(
            slices_on_track(packets, workflow.uuid)["Workflow elapsed"][:2],
            (0, 40_000_000_000),
        )
        self.assertFalse(any(item.name == "Workflow elapsed"
                             for item in descriptors.values()))

    def test_large_matrix_elapsed_span_reaches_last_leg(self):
        workflow = """
jobs:
  test:
    name: Leg (${{ matrix.os }})
    strategy:
      matrix:
        os: [a, b, c, d, e, f, g, h, i]
    steps:
      - run: echo test
"""
        definitions = asyncio.run(workflow_definitions(
            workflow, self.run["path"], self.run["head_sha"], FakeGitHub(), {}
        ))
        jobs = [job(number, f"Leg ({letter})", "2026-10-07T10:00:01Z",
                    "2026-10-07T10:00:40Z" if letter == "i"
                    else "2026-10-07T10:00:10Z")
                for number, letter in enumerate("abcdefghi", start=1)]
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: jobs}, {self.run["id"]: definitions},
        )
        descriptors, packets = trace_parts(output)
        matrix = next(item for item in descriptors.values()
                      if item.name == "Matrix: test")
        self.assertEqual(
            slices_on_track(packets, matrix.uuid)["Matrix elapsed"][:2],
            (1_000_000_000, 40_000_000_000),
        )

    def test_elapsed_summaries_include_carried_forward_matrix_legs(self):
        workflow = """
jobs:
  test:
    name: Leg (${{ matrix.os }})
    strategy:
      matrix:
        os: [a, b, c, d, e, f, g, h, i]
"""
        definitions = asyncio.run(workflow_definitions(
            workflow, self.run["path"], self.run["head_sha"], FakeGitHub(), {}
        ))
        for mode in ("copied", "rerun", "running"):
            with self.subTest(mode=mode):
                run = {**self.run, "run_attempt": 2,
                       "run_started_at": "2026-10-07T10:00:28Z"}
                jobs = [job(i, f"Leg ({letter})", "2026-10-07T10:00:01Z",
                            "2026-10-07T10:00:10Z")
                        for i, letter in enumerate("abcdefghi", 1)]
                for member in jobs:
                    member.update(run_attempt=2, created_at="2026-10-07T10:00:30Z")
                if mode != "copied":
                    jobs[-1].update(started_at="2026-10-07T10:00:32Z",
                                    completed_at="2026-10-07T10:00:40Z")
                if mode == "running":
                    run["status"] = "in_progress"
                    jobs[-1].update(status="in_progress", completed_at=None)
                output = build_trace("example/repo", run, [run], {42: jobs},
                                     {42: definitions})
                descriptors, packets = trace_parts(output)
                for track_name, span_name, end in (
                    ("CI (42)", "Workflow elapsed", 30 if mode == "copied" else 40),
                    ("Matrix: test", "Matrix elapsed", 10 if mode == "copied" else 40),
                ):
                    track = next(item for item in descriptors.values()
                                 if item.name == track_name)
                    begin = next(packet for packet in packets
                                 if packet.track_event.track_uuid == track.uuid
                                 and packet.track_event.name == span_name)
                    self.assertEqual(begin.timestamp, 1_000_000_000)
                    self.assertIn("earlier attempts", annotations(begin)["timingNote"].string_value)
                    if mode != "running":
                        self.assertEqual(slices_on_track(packets, track.uuid)[span_name][:2],
                                         (1_000_000_000, end * 1_000_000_000))
                leg = next(item for item in descriptors.values() if item.name == "Leg (a)")
                self.assertNotIn("Queued", slices_on_track(packets, leg.uuid))

    def test_trace_contains_nested_jobs_steps_and_dependency_flows(self):
        self.run["id"] = 37_640_407_122
        for item in self.jobs:
            item["id"] += 112_857_000_000
        self.jobs[2]["created_at"] = "2026-10-07T10:00:03Z"
        self.jobs[2]["steps"] = [
            {"name": "Run tests", "number": 1, "conclusion": "success",
             "started_at": "2026-10-07T10:00:06Z",
             "completed_at": "2026-10-07T10:00:29Z"}
        ]
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: self.jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, packets = trace_parts(output)
        root = next(item for item in descriptors.values()
                    if item.name == "GitHub Actions: example/repo")
        workflow = next(item for item in descriptors.values()
                        if item.name == f"CI ({self.run['id']})"
                        and item.parent_uuid == root.uuid)
        matrix = next(item for item in descriptors.values()
                      if item.name == "Matrix: test")
        self.assertEqual(matrix.parent_uuid, workflow.uuid)
        self.assertEqual(json.loads(root.description)["anchorRun"],
                         self.run["html_url"])
        self.assertEqual(json.loads(workflow.description)["dependencyEdges"], 4)
        self.assertEqual(json.loads(matrix.description)["legs"], 2)
        self.assertFalse(any(packet.track_event.track_uuid in
                             (root.uuid, workflow.uuid, matrix.uuid)
                             for packet in packets))
        tracks = {item.name: item for item in descriptors.values()
                  if item.parent_uuid in (workflow.uuid, matrix.uuid)}
        self.assertEqual(tracks["Test (linux, cli)"].parent_uuid, matrix.uuid)
        self.assertEqual(tracks["Test (windows, cli)"].parent_uuid, matrix.uuid)
        self.assertEqual(tracks["report"].parent_uuid, workflow.uuid)
        self.assertEqual(len([item for item in descriptors.values()
                              if item.parent_uuid == matrix.uuid]), 2)

        begins = {name: next(packet for packet in packets
                             if packet.track_event.track_uuid == tracks[name].uuid
                             and packet.track_event.type == TrackEvent.TYPE_SLICE_BEGIN
                             and packet.track_event.name == name)
                  for name in ("gate / Check Gate", "Test (linux, cli)",
                               "Test (windows, cli)", "report")}
        ends = {name: next(packet for packet in packets
                           if packet.track_event.track_uuid == tracks[name].uuid
                           and packet.track_event.type == TrackEvent.TYPE_SLICE_END
                           and packet.timestamp == (3 if name == "gate / Check Gate"
                                                    else 20 if name == "Test (linux, cli)"
                                                    else 30 if name == "Test (windows, cli)"
                                                    else 35) * 1_000_000_000)
                for name in begins}
        windows_track = tracks["Test (windows, cli)"].uuid
        windows_spans = slices_on_track(packets, windows_track)
        queue = windows_spans["Queued"]
        windows_job = windows_spans["Test (windows, cli)"]
        self.assertEqual(len(set(ends["gate / Check Gate"].track_event.flow_ids)), 2)
        self.assertEqual(
            set(ends["gate / Check Gate"].track_event.flow_ids),
            set(begins["Test (linux, cli)"].track_event.flow_ids)
            | set(queue[2].track_event.flow_ids),
        )
        self.assertEqual(list(begins["Test (windows, cli)"].track_event.flow_ids), [])
        self.assertEqual(
            set(begins["report"].track_event.flow_ids),
            set(ends["Test (linux, cli)"].track_event.flow_ids)
            | set(ends["Test (windows, cli)"].track_event.flow_ids),
        )
        self.assertEqual(json.loads(
            annotations(begins["report"])["needs"].string_value),
            ["Test (linux, cli)", "Test (windows, cli)"],
        )
        step = next(packet for packet in packets
                    if packet.track_event.track_uuid == windows_track
                    and packet.track_event.name == "Run tests")
        self.assertEqual(queue[0:2], (3_000_000_000, 5_000_000_000))
        self.assertEqual(windows_job[0:2], (5_000_000_000, 30_000_000_000))
        self.assertEqual(queue[1], windows_job[0])
        self.assertEqual(
            {key: value.string_value for key, value in annotations(queue[2]).items()},
            {"created_at": self.jobs[2]["created_at"],
             "started_at": self.jobs[2]["started_at"]},
        )
        self.assertEqual(
            {key: annotations(windows_job[2])[key].string_value
             for key in ("started_at", "completed_at")},
            {"started_at": self.jobs[2]["started_at"],
             "completed_at": self.jobs[2]["completed_at"]},
        )
        self.assertEqual(step.timestamp, 6_000_000_000)
        self.assertNotIn("timingEstimated", annotations(step))
        step_url = (self.jobs[2]["html_url"] + "#step:1:1")
        self.assertEqual(annotations(step)["url"].string_value, step_url)
        self.assertTrue(any(packet.track_event.track_uuid == windows_track
                            and packet.track_event.type == TrackEvent.TYPE_SLICE_END
                            and packet.timestamp == 29_000_000_000
                            for packet in packets))
        critical = next(item for item in descriptors.values()
                        if item.name == "[meta] Observed critical paths")
        self.assertEqual(critical.parent_uuid, root.uuid)
        critical_track = next(item for item in descriptors.values()
                              if item.parent_uuid == critical.uuid)
        critical_steps = slices_on_track(packets, critical_track.uuid)
        self.assertEqual(
            annotations(critical_steps["Run tests"][2])["url"].string_value,
            step_url,
        )
        self.assertEqual(output.warnings, [])

    def test_incomplete_run_suppresses_all_critical_paths(self):
        active_run = {
            **self.run,
            "id": 43,
            "name": "Active CI",
            "status": "in_progress",
            "conclusion": None,
            "html_url": "https://github.com/example/repo/actions/runs/43",
        }
        with patch("actions_perfetto_trace.observed_critical_path",
                   side_effect=AssertionError("critical path must not be calculated")):
            output = build_trace(
                "example/repo", self.run, [self.run, active_run],
                {42: self.jobs, 43: []},
                {42: self.definitions, 43: self.definitions},
                captured_at=datetime(2026, 10, 7, 10, 0, 40,
                                     tzinfo=timezone.utc),
            )
        descriptors, packets = trace_parts(output)
        root = next(item for item in descriptors.values()
                    if item.name == "GitHub Actions: example/repo")
        critical = next(item for item in descriptors.values()
                        if item.name == "[meta] Observed critical paths")
        self.assertEqual(critical.parent_uuid, root.uuid)
        self.assertFalse(json.loads(critical.description)["available"])
        self.assertEqual(json.loads(critical.description)["incompleteRunIds"], [43])
        self.assertEqual(json.loads(root.description)["criticalWorkflowRun"], None)
        self.assertEqual([item for item in descriptors.values()
                          if item.parent_uuid == critical.uuid], [])

        marker = next(packet for packet in packets
                      if packet.track_event.track_uuid == critical.uuid)
        self.assertEqual(marker.track_event.type, TrackEvent.TYPE_INSTANT)
        self.assertEqual(marker.track_event.name, "Critical path unavailable")
        self.assertIn("runs are incomplete",
                      annotations(marker)["note"].string_value)
        self.assertEqual(json.loads(
            annotations(marker)["incompleteRunIds"].string_value), [43])

        workflows = [item for item in descriptors.values()
                     if item.parent_uuid == root.uuid and item.uuid != critical.uuid]
        self.assertEqual(len(workflows), 2)
        self.assertTrue(any(item.name == "Active CI (43)"
                            for item in workflows))
        self.assertTrue(all(json.loads(item.description)["observedCriticalPath"]
                            is None for item in workflows))
        self.assertTrue(any("incomplete runs [43]" in warning
                            for warning in output.warnings))

    def test_running_job_and_step_have_no_end_events(self):
        self.run["status"] = "in_progress"
        self.run["conclusion"] = None
        active = job(2, "Test (linux, cli)",
                     "2026-10-07T10:00:04Z", None)
        active.update({
            "created_at": "2026-10-07T10:00:03Z",
            "status": "in_progress",
            "conclusion": None,
            "steps": [
                {"name": "Setup", "number": 1, "status": "completed",
                 "conclusion": "success", "started_at": "2026-10-07T10:00:05Z",
                 "completed_at": "2026-10-07T10:00:06Z"},
                {"name": "Tests", "number": 2, "status": "in_progress",
                 "conclusion": None, "started_at": "2026-10-07T10:00:06Z",
                 "completed_at": None},
            ],
        })
        output = build_trace(
            "example/repo", self.run, [self.run],
            {42: [active]}, {42: self.definitions},
            captured_at=datetime(2026, 10, 7, 10, 0, 10,
                                 tzinfo=timezone.utc),
        )
        descriptors, packets = trace_parts(output)
        job_track = next(item for item in descriptors.values()
                         if item.name == active["name"])
        events = [packet for packet in packets
                  if packet.track_event.track_uuid == job_track.uuid]
        begins = {packet.track_event.name: packet for packet in events
                  if packet.track_event.type == TrackEvent.TYPE_SLICE_BEGIN}
        self.assertEqual(set(begins), {"Queued", active["name"], "Setup", "Tests"})
        self.assertEqual([packet.timestamp for packet in events
                          if packet.track_event.type == TrackEvent.TYPE_SLICE_END],
                         [4_000_000_000, 6_000_000_000])
        self.assertEqual(begins["Tests"].timestamp, 6_000_000_000)
        self.assertEqual(annotations(begins["Tests"])["status"].string_value,
                         "in_progress")
        self.assertNotIn("partial", annotations(begins["Tests"]))
        critical = next(item for item in descriptors.values()
                        if item.name == "[meta] Observed critical paths")
        marker = next(packet for packet in packets
                      if packet.track_event.track_uuid == critical.uuid)
        self.assertEqual(marker.timestamp, 10_000_000_000)

    def test_unstarted_job_has_open_queued_slice(self):
        self.run["status"] = "in_progress"
        pending = job(2, "Test (linux, cli)",
                      "2026-10-07T10:00:03Z", None)
        pending.update({"started_at": None, "status": "queued",
                        "conclusion": None})
        output = build_trace(
            "example/repo", self.run, [self.run],
            {42: [pending]}, {42: self.definitions},
            captured_at=datetime(2026, 10, 7, 10, 0, 10,
                                 tzinfo=timezone.utc),
        )
        descriptors, packets = trace_parts(output)
        job_track = next(item for item in descriptors.values()
                         if item.name == pending["name"])
        events = [packet for packet in packets
                  if packet.track_event.track_uuid == job_track.uuid]
        self.assertEqual([(packet.track_event.type, packet.track_event.name)
                          for packet in events],
                         [(TrackEvent.TYPE_SLICE_BEGIN, "Queued")])

    def test_running_job_uses_normalized_start(self):
        self.run["status"] = "in_progress"
        active = job(2, "Test (linux, cli)",
                     "2026-10-07T10:00:03Z", None)
        active.update({
            "created_at": "2026-10-07T10:00:05Z",
            "status": "in_progress",
            "conclusion": None,
            "steps": [
                {"name": "Early step", "number": 1, "status": "in_progress",
                 "started_at": "2026-10-07T10:00:04Z",
                 "completed_at": None},
            ],
        })
        output = build_trace(
            "example/repo", self.run, [self.run],
            {42: [active]}, {42: self.definitions},
            captured_at=datetime(2026, 10, 7, 10, 0, 10,
                                 tzinfo=timezone.utc),
        )
        descriptors, packets = trace_parts(output)
        job_track = next(item for item in descriptors.values()
                         if item.name == active["name"])
        job_begin = next(packet for packet in packets
                         if packet.track_event.track_uuid == job_track.uuid
                         and packet.track_event.name == active["name"])
        self.assertEqual(job_begin.timestamp, 5_000_000_000)
        self.assertIn("displayed job starts at created_at",
                      annotations(job_begin)["timingNote"].string_value)
        self.assertFalse(any(packet.track_event.name == "Queued"
                             for packet in packets))
        early_track = next(item for item in descriptors.values()
                           if item.name == "Early step")
        self.assertEqual(early_track.parent_uuid, job_track.uuid)

    def test_partial_rerun_keeps_carried_forward_job_at_original_time(self):
        self.run["run_attempt"] = 2
        self.run["run_started_at"] = "2026-10-07T10:00:28Z"
        gate = job(1, "gate / Check Gate",
                   "2026-10-07T10:00:01Z", "2026-10-07T10:00:03Z")
        gate.update({"run_attempt": 2,
                     "created_at": "2026-10-07T10:00:30Z"})
        inherited = job(2, "Test (linux, cli)",
                        "2026-10-07T10:00:04Z", "2026-10-07T10:00:20Z")
        inherited.update({
            "run_attempt": 2,
            "created_at": "2026-10-07T10:00:30Z",
            "steps": [{"name": "Original step", "number": 1,
                       "started_at": "2026-10-07T10:00:05Z",
                       "completed_at": "2026-10-07T10:00:10Z"}],
        })
        output = build_trace(
            "example/repo", self.run, [self.run],
            {42: [gate, inherited]}, {42: self.definitions},
        )
        descriptors, packets = trace_parts(output)
        startup = next(item for item in descriptors.values()
                       if item.name == "Before first job")
        startup_span = slices_on_track(packets, startup.uuid)["Waiting for first job record"]
        self.assertEqual(startup_span[:2], (28_000_000_000, 30_000_000_000))
        job_track = next(item for item in descriptors.values()
                         if item.name == inherited["name"])
        slices = slices_on_track(packets, job_track.uuid)
        self.assertEqual(slices[inherited["name"]][:2],
                         (4_000_000_000, 20_000_000_000))
        self.assertNotIn("Queued", slices)
        self.assertEqual(annotations(slices[inherited["name"]][2])
                         ["carriedForwardFromEarlierAttempt"].bool_value, True)
        self.assertIn("Original step", slices)
        self.assertTrue(any(packet.track_event.flow_ids for packet in packets))

    def test_multiple_sources_have_separate_roots_and_flow_ids(self):
        writer = TraceWriter()
        data = ("example/repo", self.run, [self.run],
                {self.run["id"]: self.jobs}, {self.run["id"]: self.definitions})
        append_trace(writer, *data, root_name="Source 1: example/repo", root_rank=1)
        first_flows = {flow for event in writer.events for flow in event.flow_ids}
        first_event_count = len(writer.events)
        append_trace(writer, *data, root_name="Source 2: example/repo", root_rank=2)
        second_flows = {flow for event in writer.events[first_event_count:]
                        for flow in event.flow_ids}
        self.assertTrue(first_flows)
        self.assertTrue(second_flows)
        self.assertTrue(first_flows.isdisjoint(second_flows))

        packets = Trace.FromString(writer.serialize()).packet
        descriptors = [packet.track_descriptor for packet in packets
                       if packet.HasField("track_descriptor")]
        roots = [item for item in descriptors if item.name.startswith("Source ")]
        self.assertEqual([item.name for item in roots],
                         ["Source 1: example/repo", "Source 2: example/repo"])
        for root in roots:
            self.assertEqual(len([
                item for item in descriptors
                if item.parent_uuid == root.uuid
                and item.name == "[meta] Observed critical paths"
            ]), 1)

    def test_sources_show_pre_job_gap_without_changing_job_queue(self):
        writer = TraceWriter()
        cases = (
            ("Source 1", 42, "2026-10-07T10:00:00Z",
             "2026-10-07T10:00:47Z", "2026-10-07T10:00:51Z",
             "2026-10-07T10:01:00Z", 47),
            ("Source 2", 43, "2026-10-07T11:00:00Z",
             "2026-10-07T11:00:02Z", "2026-10-07T11:00:05Z",
             "2026-10-07T11:00:10Z", 2),
        )
        for rank, (source, run_id, run_start, job_created, job_started,
                   job_completed, _) in enumerate(cases, start=1):
            run = {**self.run, "id": run_id, "created_at": run_start,
                   "updated_at": job_completed}
            first = job(run_id, "gate / Check Gate", job_started, job_completed)
            first["created_at"] = job_created
            append_trace(writer, "example/repo", run, [run],
                         {run_id: [first]}, {run_id: self.definitions},
                         root_name=source, root_rank=rank)

        packets = Trace.FromString(writer.serialize()).packet
        descriptors = {packet.track_descriptor.uuid: packet.track_descriptor
                       for packet in packets if packet.HasField("track_descriptor")}
        for source, _, _, _, _, _, gap_seconds in cases:
            root = next(item for item in descriptors.values()
                        if item.name == source)
            workflow = next(item for item in descriptors.values()
                            if item.parent_uuid == root.uuid
                            and item.name.startswith("CI ("))
            startup = next(item for item in descriptors.values()
                           if item.parent_uuid == workflow.uuid
                           and item.name == "Before first job")
            self.assertEqual(startup.sibling_order_rank, 0)
            self.assertFalse(any(packet.track_event.track_uuid == workflow.uuid
                                 for packet in packets if packet.HasField("track_event")))
            span = slices_on_track(packets, startup.uuid)["Waiting for first job record"]
            self.assertEqual(span[:2], (0, gap_seconds * 1_000_000_000))
            self.assertIn("cause is not available",
                          annotations(span[2])["timingNote"].string_value)
            first = next(item for item in descriptors.values()
                         if item.parent_uuid == workflow.uuid
                         and item.name == "gate / Check Gate")
            queue = slices_on_track(packets, first.uuid)["Queued"]
            self.assertEqual(queue[0], span[1])

    def test_matrix_legs_are_nested_and_ordered_when_api_jobs_are_interleaved(self):
        jobs = [self.jobs[index] for index in (0, 2, 3, 1)]
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, _ = trace_parts(output)
        matrix = next(item for item in descriptors.values()
                      if item.name == "Matrix: test")
        linux = next(item for item in descriptors.values()
                     if item.name == "Test (linux, cli)")
        windows = next(item for item in descriptors.values()
                       if item.name == "Test (windows, cli)")
        report = next(item for item in descriptors.values()
                      if item.name == "report")
        self.assertEqual((linux.parent_uuid, windows.parent_uuid),
                         (matrix.uuid, matrix.uuid))
        self.assertEqual((linux.sibling_order_rank, windows.sibling_order_rank),
                         (2, 3))
        self.assertEqual(report.sibling_order_rank, 4)
        self.assertEqual(output.warnings, [])

    def test_overlapping_steps_use_child_tracks(self):
        self.jobs[2]["steps"] = [
            {"name": "First", "started_at": "2026-10-07T10:00:06Z",
             "completed_at": "2026-10-07T10:00:12Z"},
            {"name": "Second", "started_at": "2026-10-07T10:00:10Z",
             "completed_at": "2026-10-07T10:00:15Z"},
        ]
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: self.jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, _ = trace_parts(output)
        job_track = next(item for item in descriptors.values()
                         if item.name == "Test (windows, cli)")
        children = {item.name for item in descriptors.values()
                    if item.parent_uuid == job_track.uuid}
        self.assertEqual(children, {"First", "Second"})
        self.assertTrue(any("cannot be nested" in warning
                            for warning in output.warnings))

    def test_same_second_steps_are_contiguous_spans(self):
        self.jobs[2]["steps"] = [
            {"name": name, "number": number, "conclusion": "success",
             "started_at": f"2026-10-07T10:00:{start:02d}Z",
             "completed_at": f"2026-10-07T10:00:{end:02d}Z"}
            for number, (name, start, end) in enumerate([
                ("A", 6, 6), ("B", 6, 6), ("C", 6, 7),
                ("D", 7, 7), ("E", 7, 30), ("F", 30, 30),
            ], start=1)
        ]
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: self.jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, packets = trace_parts(output)
        job_track = next(item for item in descriptors.values()
                         if item.name == "Test (windows, cli)")
        spans = slices_on_track(packets, job_track.uuid)
        steps = [spans[name] for name in "ABCDEF"]
        self.assertEqual(steps[0][0], 6_000_000_000)
        self.assertEqual(steps[-1][1], 30_000_000_000)
        self.assertTrue(all(start < end for start, end, _ in steps))
        self.assertTrue(all(left[1] == right[0]
                            for left, right in zip(steps, steps[1:])))
        self.assertTrue(all(annotations(begin)["timingEstimated"].bool_value
                            for _, _, begin in steps))
        self.assertIn("under 1 second",
                      annotations(spans["A"][2])["timingNote"].string_value)
        self.assertIn("adjusted",
                      annotations(spans["C"][2])["timingNote"].string_value)
        self.assertFalse(any(packet.track_event.type == TrackEvent.TYPE_INSTANT
                             and packet.track_event.name in "ABCDEF"
                             for packet in packets))
        critical_group = next(item for item in descriptors.values()
                              if item.name == "[meta] Observed critical paths")
        critical_track = next(item for item in descriptors.values()
                              if item.parent_uuid == critical_group.uuid)
        critical_steps = slices_on_track(packets, critical_track.uuid)
        self.assertEqual([(critical_steps[name][0], critical_steps[name][1])
                          for name in "ABCDEF"],
                         [(start, end) for start, end, _ in steps])
        self.assertIn("under 1 second",
                      annotations(critical_steps["A"][2])["timingNote"].string_value)
        self.assertEqual(output.warnings, [])

    def test_same_second_job_steps_remain_spans_without_room_to_nest(self):
        self.jobs[2]["created_at"] = "2026-10-07T10:00:05Z"
        self.jobs[2]["started_at"] = "2026-10-07T10:00:05Z"
        self.jobs[2]["completed_at"] = "2026-10-07T10:00:05Z"
        self.jobs[2]["steps"] = [
            {"name": name, "started_at": "2026-10-07T10:00:05Z",
             "completed_at": "2026-10-07T10:00:05Z"}
            for name in ("A", "B")
        ]
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: self.jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, packets = trace_parts(output)
        parent = next(item for item in descriptors.values()
                      if item.name == "Test (windows, cli)")
        children = [item for item in descriptors.values()
                    if item.parent_uuid == parent.uuid]
        self.assertEqual({item.name for item in children}, {"A", "B"})
        for child in children:
            start, end, _ = slices_on_track(packets, child.uuid)[child.name]
            self.assertLess(start, end)
        self.assertTrue(any("cannot be nested" in warning
                            for warning in output.warnings))

    def test_skipped_step_does_not_consume_estimated_time(self):
        self.jobs[2]["steps"] = [
            {"name": "A", "number": 1, "conclusion": "success",
             "started_at": "2026-10-07T10:00:06Z",
             "completed_at": "2026-10-07T10:00:06Z"},
            {"name": "Skipped", "number": 2, "conclusion": "skipped",
             "started_at": "2026-10-07T10:00:06Z",
             "completed_at": "2026-10-07T10:00:06Z"},
            {"name": "C", "number": 3, "conclusion": "success",
             "started_at": "2026-10-07T10:00:06Z",
             "completed_at": "2026-10-07T10:00:07Z"},
        ]
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: self.jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, packets = trace_parts(output)
        job_track = next(item for item in descriptors.values()
                         if item.name == "Test (windows, cli)")
        spans = slices_on_track(packets, job_track.uuid)
        self.assertEqual(spans["A"][0:2], (6_000_000_000, 6_500_000_000))
        self.assertEqual(spans["C"][0:2], (6_500_000_000, 7_000_000_000))
        self.assertFalse(any(packet.track_event.name == "Skipped"
                             for packet in packets))
        self.assertEqual(json.loads(
            annotations(spans["Test (windows, cli)"][2])["skippedSteps"].string_value),
            [{"name": "Skipped", "number": 2}],
        )

    def test_skipped_job_with_reversed_timestamps_is_an_instant(self):
        self.jobs[0].update({
            "created_at": "2026-10-07T10:00:03Z",
            "started_at": "2026-10-07T10:00:03Z",
            "completed_at": "2026-10-07T10:00:02Z",
            "conclusion": "skipped",
        })
        output = build_trace(
            "example/repo", self.run, [self.run],
            {self.run["id"]: self.jobs}, {self.run["id"]: self.definitions},
        )
        descriptors, packets = trace_parts(output)
        gate = next(item for item in descriptors.values()
                    if item.name == "gate / Check Gate")
        marker = next(packet for packet in packets
                      if packet.track_event.track_uuid == gate.uuid
                      and packet.track_event.name == "gate / Check Gate")
        self.assertEqual(marker.track_event.type, TrackEvent.TYPE_INSTANT)
        self.assertEqual(output.warnings, [])

    def test_reusable_display_name_and_truncated_matrix_name(self):
        workflow = """
jobs:
  caller:
    name: Smoke tests
    uses: ./.github/workflows/gate.yml
"""
        child = """
jobs:
  generate-matrix:
    runs-on: ubuntu-latest
  test:
    name: Robot (Linux x64 / x86 target)
    strategy:
      matrix:
        os: [linux]
"""

        class Source:
            repository = "example/repo"

            async def workflow_text(self, path, sha):
                return child

        definitions = asyncio.run(workflow_definitions(
            workflow, ".github/workflows/ci.yml", "abc123", Source(), {}
        ))
        observed = [
            job(11, "Smoke tests / generate-matrix", "2026-10-07T10:00:01Z",
                "2026-10-07T10:00:02Z"),
            job(12, "Smoke tests / Robot (Linux x64 / x86 target) (linux, apx...",
                "2026-10-07T10:00:02Z", "2026-10-07T10:00:03Z"),
        ]
        matched, warnings = match_jobs(observed, definitions)
        self.assertEqual(warnings, [])
        self.assertEqual([item.definition.path[-1] for item in matched],
                         ["generate-matrix", "test"])

    def test_matrix_reusable_caller_matches_child_jobs(self):
        for caller_name in ("Smoke tests", "Smoke tests (${{ matrix.machine }})"):
            with self.subTest(caller_name=caller_name):
                workflow = f"""
jobs:
  gate:
    runs-on: ubuntu-latest
  caller:
    name: {caller_name}
    needs: gate
    strategy:
      matrix:
        machine: [machine A]
    uses: ./.github/workflows/gate.yml
  report:
    needs: caller
    runs-on: ubuntu-latest
"""
                definitions = asyncio.run(workflow_definitions(
                    workflow, ".github/workflows/ci.yml", "abc123", FakeGitHub(), {}
                ))
                observed = [
                    job(10, "gate", "2026-10-07T10:00:01Z",
                        "2026-10-07T10:00:02Z"),
                    job(11, "Smoke tests (machine A) / Check Gate",
                        "2026-10-07T10:00:02Z", "2026-10-07T10:00:03Z"),
                    job(12, "report", "2026-10-07T10:00:03Z",
                        "2026-10-07T10:00:04Z"),
                ]
                matched, warnings = match_jobs(observed, definitions)
                self.assertEqual(warnings, [])
                self.assertEqual(dependencies(matched, definitions),
                                 {(10, 11), (11, 12)})

    def test_internal_needs_stay_within_matrix_caller_invocation(self):
        for name in ("caller", "Host (${{ matrix.os }})"):
            with self.subTest(name=name):
                class Source(FakeGitHub):
                    async def workflow_text(self, path, sha):
                        return "jobs:\n  build: {}\n  test:\n    needs: build\n"

                workflow = f"""
jobs:
  gate: {{}}
  caller:
    name: {name}
    needs: gate
    strategy:
      matrix:
        os: [linux, windows]
    uses: ./.github/workflows/gate.yml
  report:
    needs: caller
"""
                definitions = asyncio.run(workflow_definitions(
                    workflow, ".github/workflows/ci.yml", "abc", Source(), {}
                ))
                prefix = "caller" if name == "caller" else "Host"
                observed = [
                    job(10, "gate", "2026-10-07T10:00:00Z", "2026-10-07T10:00:01Z"),
                    job(11, f"{prefix} (linux) / build", "2026-10-07T10:00:01Z", "2026-10-07T10:00:02Z"),
                    job(12, f"{prefix} (windows) / build", "2026-10-07T10:00:01Z", "2026-10-07T10:00:03Z"),
                    job(13, f"{prefix} (linux) / test", "2026-10-07T10:00:04Z", "2026-10-07T10:00:08Z"),
                    job(14, f"{prefix} (windows) / test", "2026-10-07T10:00:03Z", "2026-10-07T10:00:07Z"),
                    job(15, "report", "2026-10-07T10:00:08Z", "2026-10-07T10:00:09Z"),
                ]
                matched, warnings = match_jobs(observed, definitions)
                self.assertEqual(warnings, [])
                edges = dependencies(matched, definitions)
                self.assertEqual(edges, {(10, 11), (10, 12), (11, 13), (12, 14),
                                         (11, 15), (12, 15), (13, 15), (14, 15)})
                self.assertEqual(observed_critical_path(observed, edges), [10, 11, 13, 15])

    def test_external_workflows_expand_at_recorded_revision_including_local_children(self):
        calls = []
        sha = "a" * 40
        workflow = "jobs:\n  caller:\n    uses: other/repo/.github/workflows/build.yml@main\n"

        class Source(FakeGitHub):
            async def workflow_text(self, path, revision, repository=None):
                calls.append((repository, path, revision))
                if path.endswith("build.yml"):
                    return "jobs:\n  build: {}\n  test:\n    needs: build\n    uses: ./.github/workflows/test.yml\n"
                return "jobs:\n  check: {}\n"

        definitions = asyncio.run(workflow_definitions(
            workflow, ".github/workflows/ci.yml", "root", Source(), {},
            references=[{"path": "other/repo/.github/workflows/build.yml@main", "sha": sha}],
        ))
        self.assertEqual(calls, [("other/repo", ".github/workflows/build.yml", sha),
                                 ("other/repo", ".github/workflows/test.yml", sha)])
        observed = [job(1, "caller / build", "2026-10-07T10:00:00Z", "2026-10-07T10:00:02Z"),
                    job(2, "caller / test / check", "2026-10-07T10:00:02Z", "2026-10-07T10:00:05Z")]
        matched, warnings = match_jobs(observed, definitions)
        self.assertEqual(warnings, [])
        self.assertEqual(dependencies(matched, definitions), {(1, 2)})
        self.assertEqual(observed_critical_path(observed, {(1, 2)}), [1, 2])

    def test_nested_external_workflow_uses_its_own_revision(self):
        first, second = "a" * 40, "b" * 40
        calls = []

        class Source(FakeGitHub):
            async def workflow_text(self, path, revision, repository=None):
                calls.append((repository, revision))
                if repository == "other/first":
                    return "jobs:\n  child:\n    uses: other/second/.github/workflows/ci.yml@v1\n"
                return "jobs:\n  check: {}\n"

        definitions = asyncio.run(workflow_definitions(
            f"jobs:\n  caller:\n    uses: other/first/.github/workflows/ci.yml@{first}\n",
            ".github/workflows/ci.yml", "root", Source(), {},
            references=[{"path": "other/second/.github/workflows/ci.yml@v1", "sha": second}],
        ))
        self.assertEqual(calls, [("other/first", first), ("other/second", second)])
        self.assertEqual(definitions[-1].path, ("caller", "child", "check"))

    def test_inaccessible_external_workflow_keeps_timings_and_known_dependencies(self):
        source = """
jobs:
  gate: {}
  private:
    needs: gate
    uses: other/private/.github/workflows/ci.yml@REF
  report:
    needs: private
""".replace("REF", "a" * 40)
        observed = [
            job(1, "gate", "2026-10-07T10:00:01Z", "2026-10-07T10:00:03Z"),
            job(2, "private / Fetch token", "2026-10-07T10:00:03Z", "2026-10-07T10:00:05Z"),
            job(3, "private / Review", "2026-10-07T10:00:05Z", "2026-10-07T10:00:10Z"),
            job(4, "report", "2026-10-07T10:00:10Z", "2026-10-07T10:00:12Z"),
        ]
        observed[2]["steps"] = [{"name": "Run review", "number": 1,
                                 "started_at": "2026-10-07T10:00:06Z",
                                 "completed_at": "2026-10-07T10:00:09Z"}]
        run = self.run
        fail_jobs = False

        class Source(FakeGitHub):
            async def get_run(self, run_id):
                return run

            async def jobs(self, run_id):
                if fail_jobs:
                    raise TraceError("jobs unavailable")
                return observed

            async def workflow_text(self, path, sha, repository=None):
                if repository:
                    raise WorkflowUnavailable(f"{repository}/{path}@{sha} cannot be read (HTTP 404)")
                return source

        args = argparse.Namespace(repo=None, max_concurrency=50, single_run=True,
                                  event_window_seconds=120, workflow_file=None,
                                  sources=[run["html_url"]])
        writer = TraceWriter()
        with patch("actions_perfetto_trace.GitHub", return_value=Source()):
            workflows, count, warnings = asyncio.run(load_source(
                run["html_url"], args, writer, 1,
            ))
        self.assertEqual((workflows, count), (1, 4))
        self.assertTrue(any("internal dependencies unavailable" in warning for warning in warnings))
        self.assertFalse(any("matched 0" in warning for warning in warnings))
        descriptors, packets = trace_parts(TraceOutput(writer.serialize(), warnings))
        workflow = next(item for item in descriptors.values() if item.name == "CI (42)")
        details = json.loads(workflow.description)
        self.assertFalse(details["workflowDefinitionsVerified"])
        self.assertEqual(details["dependencyEdges"], 4)
        self.assertIsNone(details["observedCriticalPath"])
        review = next(item for item in descriptors.values() if item.name == "private / Review")
        slices = slices_on_track(packets, review.uuid)
        self.assertEqual(slices["private / Review"][:2], (5_000_000_000, 10_000_000_000))
        self.assertEqual(slices["Run review"][:2], (6_000_000_000, 9_000_000_000))
        for name in ("private / Review", "Run review"):
            details = annotations(slices[name][2])
            self.assertFalse(details["workflowDefinitionAvailable"].bool_value)
            self.assertIn("internal dependencies are unavailable", details["dependencyNote"].string_value)
        self.assertEqual(json.loads(annotations(slices["private / Review"][2])["needs"].string_value),
                         ["gate"])
        gate = next(item for item in descriptors.values() if item.name == "gate")
        self.assertNotIn("workflowDefinitionAvailable",
                         annotations(slices_on_track(packets, gate.uuid)["gate"][2]))
        self.assertTrue(any(packet.track_event.flow_ids for packet in packets))
        self.assertTrue(any(packet.track_event.name == "Critical path unavailable" for packet in packets))
        fail_jobs = True
        with patch("actions_perfetto_trace.GitHub", return_value=Source()), self.assertRaisesRegex(
            TraceError, "jobs unavailable"
        ):
            asyncio.run(load_source(run["html_url"], args, TraceWriter(), 1))

    def test_inaccessible_matrix_workflow_matches_rendered_and_fallback_caller_names(self):
        class Source(FakeGitHub):
            async def workflow_text(self, path, sha, repository=None):
                raise WorkflowUnavailable("external workflow cannot be read (HTTP 403)")

        workflow = """
jobs:
  caller:
    name: Host (${{ matrix.os }})
    strategy:
      matrix:
        os: [linux, windows]
    uses: other/private/.github/workflows/ci.yml@REF
""".replace("REF", "a" * 40)
        definitions = asyncio.run(workflow_definitions(
            workflow, self.run["path"], self.run["head_sha"], Source(), {}
        ))
        jobs = [job(i, name, "2026-10-07T10:00:01Z", "2026-10-07T10:00:05Z")
                for i, name in enumerate(("Host (linux) / Review", "Host (windows) / Review",
                                          "caller / Review"), 1)]
        matched, warnings = match_jobs(jobs, definitions)
        self.assertEqual(warnings, [])
        self.assertEqual(len(matched), 3)
        self.assertTrue(all(item.definition.unavailable for item in matched))
        self.assertEqual(matched[0].matrix, {"os": "linux"})
        self.assertEqual(matched[-1].matrix, {})

    def test_external_workflow_parse_and_other_failures_remain_fatal(self):
        class Source(FakeGitHub):
            async def workflow_text(self, path, sha, repository=None):
                if error:
                    raise error
                return "jobs: ["

        workflow = "jobs:\n  caller:\n    uses: other/private/.github/workflows/ci.yml@" + "a" * 40
        for error in (None, TraceError("HTTP 500")):
            with self.subTest(error=error), self.assertRaises(TraceError):
                asyncio.run(workflow_definitions(
                    workflow, self.run["path"], self.run["head_sha"], Source(), {}
                ))

    def test_external_workflow_requires_immutable_revision(self):
        class Source(FakeGitHub):
            async def workflow_text(self, path, revision, repository=None):
                revisions.append(revision)
                return "jobs:\n  check: {}\n"

        revisions = []
        workflow = "jobs:\n  caller:\n    uses: other/repo/.github/workflows/build.yml@{}\n"
        with self.assertRaisesRegex(TraceError, "cannot resolve executed revision"):
            asyncio.run(workflow_definitions(
                workflow.format("main"), ".github/workflows/ci.yml", "root", Source(), {}
            ))
        sha = "b" * 40
        asyncio.run(workflow_definitions(
            workflow.format(sha), ".github/workflows/ci.yml", "root", Source(), {}
        ))
        self.assertEqual(revisions, [sha])

    def test_event_window_does_not_mix_separate_triggers(self):
        api = FakeGitHub()
        api.run_list = [
            self.run,
            {**self.run, "id": 43, "created_at": "2026-10-07T10:00:02Z"},
            {**self.run, "id": 44, "created_at": "2026-10-07T10:05:00Z"},
            {**self.run, "id": 45, "head_branch": "queue/other"},
        ]
        self.assertEqual(
            [run["id"] for run in asyncio.run(triggered_runs(api, self.run, 120))],
            [42, 43]
        )

    def test_pull_request_grouping_uses_pr_identity(self):
        anchor = {**self.run, "event": "pull_request", "head_branch": "feature",
                  "pull_requests": [{"number": 7}]}
        other = {**anchor, "id": 43, "pull_requests": [{"number": 8}]}
        historical = {**anchor, "id": 44, "pull_requests": [],
                      "created_at": "2026-10-07T10:00:02Z"}
        api = FakeGitHub()
        api.run_list = [anchor, other, historical]
        api.associated_pull_requests = {anchor["head_sha"]: {7}}
        self.assertEqual(
            [run["id"] for run in asyncio.run(triggered_runs(api, anchor, 120))],
            [42, 44],
        )

        api.associated_pull_requests = {anchor["head_sha"]: {7, 8}}
        warnings = []
        self.assertEqual(
            [run["id"] for run in asyncio.run(triggered_runs(api, anchor, 120,
                                                               warnings))],
            [42],
        )
        self.assertIn("44", warnings[0])

    def test_pull_request_grouping_prefers_same_commit_local_merge_ref(self):
        anchor = {**self.run, "event": "pull_request", "head_branch": "feature",
                  "pull_requests": [{"number": 7}]}
        shared = {**anchor, "id": 43,
                  "pull_requests": [{"number": 7}, {"number": 8}],
                  "referenced_workflows": [{
                      "path": "example/repo/.github/workflows/gate.yml@merge-sha",
                      "sha": "merge-sha", "ref": "refs/pull/7/merge",
                  }]}
        unrelated = {**anchor, "id": 44, "pull_requests": [{"number": 8}],
                     "referenced_workflows": [{
                         "path": "other/repo/.github/workflows/gate.yml@merge-sha",
                         "sha": "merge-sha", "ref": "refs/pull/7/merge",
                     }]}
        pinned = {**anchor, "id": 45, "pull_requests": [{"number": 8}],
                  "referenced_workflows": [{
                      "path": "example/repo/.github/workflows/gate.yml@refs/pull/7/merge",
                      "sha": "merge-sha", "ref": "refs/pull/7/merge",
                  }]}
        conflicted = {**anchor, "id": 46, "pull_requests": [{"number": 8}],
                      "referenced_workflows": shared["referenced_workflows"]}
        api = FakeGitHub()
        api.run_list = [anchor, shared, unrelated, pinned, conflicted]
        self.assertEqual(
            [run["id"] for run in asyncio.run(triggered_runs(api, anchor, 120))],
            [42, 43],
        )

    def test_pull_request_anchor_rejects_ambiguous_pr_identity(self):
        api = FakeGitHub()
        api.run_list = [
            {**self.run, "event": "pull_request", "head_branch": "feature",
             "pull_requests": [], "id": 43},
            {**self.run, "event": "pull_request", "head_branch": "feature",
             "pull_requests": [{"number": 7}], "id": 44},
        ]
        api.associated_pull_requests = {self.run["head_sha"]: {7, 8}}
        async def get_run(run_id):
            return next(run for run in api.run_list if run["id"] == run_id)
        api.get_run = get_run
        self.assertEqual(asyncio.run(anchor_from_event(
            api, "pull_request", [self.run["head_sha"]], None,
            "feature", 7, 120,
        ))["id"], 44)

    def test_pull_request_workflows_use_recorded_merge_revision(self):
        anchor = {**self.run, "event": "pull_request", "head_sha": "pr-head",
                  "head_branch": "feature", "pull_requests": [{"number": 7}],
                  "referenced_workflows": [{
                      "path": "example/repo/.github/workflows/gate.yml@merge-sha",
                      "ref": "refs/pull/7/merge", "sha": "merge-sha",
                  }]}
        sibling = {**anchor, "id": 43, "pull_requests": [],
                   "referenced_workflows": []}

        class Source(FakeGitHub):
            calls = []

            async def get_run(self, run_id):
                return anchor

            async def jobs(self, run_id):
                return ([{**item, "id": item["id"] + 10} for item in self.jobs_list]
                        if run_id == 43 else self.jobs_list)

            async def workflow_text(self, path, sha):
                self.calls.append((path, sha))
                return WORKFLOW if path == anchor["path"] else CHILD

        api = Source()
        api.jobs_list = self.jobs
        api.run_list = [anchor, sibling]
        api.associated_pull_requests = {"pr-head": {7}}
        args = argparse.Namespace(repo=None, max_concurrency=50, single_run=False,
                                  event_window_seconds=120, workflow_file=None,
                                  sources=[anchor["html_url"]])
        writer = TraceWriter()
        with patch("actions_perfetto_trace.GitHub", return_value=api):
            workflows, jobs, warnings = asyncio.run(load_source(
                anchor["html_url"], args, writer, 1,
            ))
        self.assertEqual((workflows, jobs), (2, 8))
        self.assertEqual(warnings, [])
        self.assertTrue(api.calls)
        self.assertEqual({sha for _, sha in api.calls}, {"merge-sha"})
        self.assertIn((".github/workflows/gate.yml", "merge-sha"), api.calls)

    def test_unknown_pr_workflow_revision_omits_dependency_graph(self):
        anchor = {**self.run, "event": "pull_request", "head_sha": "pr-head",
                  "pull_requests": [{"number": 7}],
                  "referenced_workflows": [{
                      "path": "example/repo/.github/workflows/gate.yml@refs/pull/8/merge",
                      "sha": "another-pr-merge", "ref": "refs/pull/8/merge",
                  }]}

        class Source(FakeGitHub):
            async def get_run(self, run_id):
                return anchor

            async def jobs(self, run_id):
                return self.jobs_list

            async def workflow_text(self, path, sha):
                raise AssertionError("workflow content must not be guessed from the PR head")

        api = Source()
        api.jobs_list = self.jobs
        args = argparse.Namespace(repo=None, max_concurrency=50, single_run=True,
                                  event_window_seconds=120, workflow_file=None,
                                  sources=[anchor["html_url"]])
        writer = TraceWriter()
        with patch("actions_perfetto_trace.GitHub", return_value=api):
            workflows, jobs, warnings = asyncio.run(load_source(
                anchor["html_url"], args, writer, 1,
            ))
        self.assertEqual((workflows, jobs), (1, 4))
        self.assertTrue(any("revision unknown" in warning for warning in warnings))
        packets = Trace.FromString(writer.serialize()).packet
        self.assertFalse(any(packet.track_event.flow_ids for packet in packets
                             if packet.HasField("track_event")))
        self.assertTrue(any(packet.track_event.name == "Critical path unavailable"
                            for packet in packets if packet.HasField("track_event")))

    def test_run_url_identifies_repository(self):
        self.assertEqual(parse_run(self.run["html_url"], None), ("example/repo", 42))

    def test_merge_group_payload_finds_its_run(self):
        payload = {
            "repository": {"full_name": "example/repo"},
            "merge_group": {"head_sha": "abc123",
                            "head_ref": "refs/heads/queue/test"},
        }
        repository, event, shas, occurred, branch, number = parse_event_payload(
            payload, None
        )
        self.assertEqual((repository, event, shas, branch, number),
                         ("example/repo", "merge_group", ["abc123"], "queue/test", None))
        self.assertIsNone(occurred)

        class Source(FakeGitHub):
            async def get_run(self, run_id):
                return self.run_list[0]

        api = Source()
        api.run_list = [self.run]
        self.assertEqual(asyncio.run(anchor_from_event(
            api, event, shas, occurred, branch, number, 120
        ))["id"], 42)

    def test_pull_request_payload_uses_branch_when_merge_sha_is_missing(self):
        payload = {
            "action": "opened",
            "repository": {"full_name": "example/repo"},
            "pull_request": {
                "number": 7, "created_at": "2026-10-07T10:00:00Z",
                "head": {"sha": "pr-head", "ref": "feature"},
            },
        }
        repository, event, shas, occurred, branch, number = parse_event_payload(
            payload, None
        )
        self.assertEqual((repository, event, shas, branch, number),
                         ("example/repo", "pull_request", ["pr-head"], "feature", 7))

        class Source(FakeGitHub):
            async def runs(self, **query):
                return [] if "head_sha" in query else [self.run]

            async def get_run(self, run_id):
                return self.run

        api = Source()
        api.run = {**self.run, "event": "pull_request",
                   "head_sha": "synthetic-merge", "head_branch": "feature",
                   "pull_requests": [{"number": 7}]}
        self.assertEqual(asyncio.run(anchor_from_event(
            api, event, shas, occurred, branch, number, 120
        ))["id"], 42)


class GitHubClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_workflow_access_errors_are_classified_without_hiding_server_errors(self):
        class FakeApi:
            def __init__(self):
                self.repos = self

            async def get_content(self, **params):
                raise HTTPError("https://api.github.com/contents", code, "failure", {}, None)

        for code in (401, 403, 404, 500):
            with self.subTest(code=code), patch.dict(os.environ, {"GITHUB_TOKEN": "test"}), patch(
                "actions_perfetto_trace.GhApi", return_value=FakeApi()
            ):
                api = GitHub("example/repo")
                with self.assertRaises(TraceError) as raised:
                    await api.workflow_text(".github/workflows/ci.yml", "a" * 40, "other/private")
                self.assertEqual(isinstance(raised.exception, WorkflowUnavailable), code != 500)

    async def test_ghapi_requests_obey_limit_and_return_plain_lists(self):
        active = 0
        peak = 0

        class FakeApi:
            def __init__(self):
                self.actions = self

            async def list_jobs_for_workflow_run(self, **params):
                nonlocal active, peak
                assert params == {"run_id": 42, "filter": "latest",
                                  "per_page": 100, "page": 1}
                active += 1
                peak = max(peak, active)
                await asyncio.sleep(0.001)
                active -= 1
                return {"jobs": L([{"steps": L([{"name": "Build"}])}])}

        with patch.dict(os.environ, {"GITHUB_TOKEN": "test"}), patch(
            "actions_perfetto_trace.GhApi", return_value=FakeApi()
        ):
            api = GitHub("example/repo", max_concurrency=2)
            results = await asyncio.gather(*(
                api.jobs(42) for _ in range(5)
            ))
        self.assertEqual(peak, 2)
        self.assertEqual(results[0], [{"steps": [{"name": "Build"}]}])
        self.assertIs(type(results[0]), list)


if __name__ == "__main__":
    unittest.main()
