#!/usr/bin/env python3
# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Convert the Actions runs for one trigger into a Perfetto protobuf trace."""

from __future__ import annotations

import argparse
import asyncio
import base64
import json
import os
import re
import sys
from dataclasses import dataclass, replace
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

try:
    from fastcore.foundation import L
    from ghapi.all import GhApi
except ImportError as error:
    raise SystemExit("ghapi is required: python -m pip install ghapi") from error

try:
    from perfetto.protos.perfetto.trace.perfetto_trace_pb2 import TrackDescriptor, TrackEvent
    from perfetto.trace_builder.proto_builder import TraceProtoBuilder
except ImportError as error:
    raise SystemExit("perfetto is required: python -m pip install perfetto") from error

try:
    import yaml
except ImportError as error:
    raise SystemExit("PyYAML is required: python -m pip install PyYAML") from error


PERFETTO_SUMMARY_TRACK_LIMIT = 8


class TraceError(RuntimeError):
    """A run cannot be represented reliably."""


class WorkflowUnavailable(TraceError):
    """A workflow definition cannot be read with the supplied credentials."""


def plain_json(value: Any) -> Any:
    """Convert ghapi's AttrDict and L values to ordinary JSON containers."""
    if isinstance(value, dict):
        return {key: plain_json(item) for key, item in value.items()}
    if isinstance(value, (list, L)):
        return [plain_json(item) for item in value]
    return value


@dataclass(frozen=True)
class JobDefinition:
    path: tuple[str, ...]
    name: str
    needs: tuple[str, ...]
    matrix: bool
    reusable: bool
    unavailable: str | None = None


@dataclass(frozen=True)
class MatchedJob:
    job: dict[str, Any]
    definition: JobDefinition
    matrix: dict[str, str]
    invocation: tuple[str, ...] = ()


@dataclass(frozen=True)
class TraceOutput:
    data: bytes
    warnings: list[str]


@dataclass
class StepSpan:
    step: dict[str, Any]
    start_ns: int
    end_ns: int
    estimated: bool = False


def step_timing_details(span: StepSpan) -> dict[str, Any]:
    if not span.estimated:
        return {}
    step = span.step
    if step.get("started_at") == step.get("completed_at"):
        note = ("GitHub reports the same start and completion second. "
                "Actual duration was under 1 second; displayed duration is estimated.")
    else:
        note = ("Displayed step boundaries were adjusted to keep sequential steps "
                "contiguous; GitHub reports times to whole seconds.")
    return {"timingEstimated": True, "timingNote": note,
            "reportedStart": step.get("started_at"),
            "reportedCompletion": step.get("completed_at")}


def step_url(job: dict[str, Any], step: dict[str, Any]) -> str | None:
    job_url = job.get("html_url")
    number = step.get("number")
    if not job_url or not isinstance(number, int) or number < 1:
        return None
    return f"{job_url}#step:{number}:1"


def carried_forward_job(job: dict[str, Any]) -> bool:
    """A partial rerun can copy an earlier job with a fresh created_at."""
    created = timestamp(job.get("created_at"))
    completed = timestamp(job.get("completed_at"))
    return (job.get("run_attempt", 1) > 1 and created is not None
            and completed is not None and completed < created)


def job_execution_start(job: dict[str, Any]) -> datetime | None:
    created = timestamp(job.get("created_at"))
    started = timestamp(job.get("started_at"))
    if carried_forward_job(job):
        return started
    return started if started and (created is None or started >= created) else created


@dataclass(frozen=True)
class PendingEvent:
    timestamp_ns: int
    kind: int
    track_uuid: int
    depth: int
    order: int
    name: str | None = None
    args: dict[str, Any] | None = None
    flow_ids: tuple[int, ...] = ()


class TraceWriter:
    """Write track descriptors and time-ordered events with Perfetto's builder."""

    def __init__(self) -> None:
        self.builder = TraceProtoBuilder()
        self.events: list[PendingEvent] = []
        self.next_uuid = 1
        self.next_flow_id = 1

    def flow_id(self) -> int:
        flow_id = self.next_flow_id
        self.next_flow_id += 1
        return flow_id

    def track(self, name: str, parent: int | None = None, rank: int = 0,
              description: str | None = None) -> int:
        uuid = self.next_uuid
        self.next_uuid += 1
        descriptor = self.builder.add_packet().track_descriptor
        descriptor.uuid = uuid
        descriptor.name = name
        if description is not None:
            descriptor.description = description
        descriptor.child_ordering = TrackDescriptor.EXPLICIT
        descriptor.sibling_order_rank = rank
        if parent is not None:
            descriptor.parent_uuid = parent
        return uuid

    def event(self, timestamp_ns: int, kind: int, track_uuid: int,
              name: str | None = None, args: dict[str, Any] | None = None,
              flow_ids: tuple[int, ...] = (), depth: int = 0) -> None:
        self.events.append(PendingEvent(timestamp_ns, kind, track_uuid, depth,
                                        len(self.events), name, args, flow_ids))

    def span(self, track_uuid: int, start_ns: int, end_ns: int, name: str,
             args: dict[str, Any] | None = None,
             incoming: tuple[int, ...] = (), outgoing: tuple[int, ...] = (),
             depth: int = 0) -> None:
        if end_ns <= start_ns:
            raise TraceError(f"invalid span duration: {name}")
        self.event(start_ns, TrackEvent.TYPE_SLICE_BEGIN, track_uuid,
                   name, args, incoming, depth)
        self.event(end_ns, TrackEvent.TYPE_SLICE_END, track_uuid,
                   flow_ids=outgoing, depth=depth)

    def open_span(self, track_uuid: int, start_ns: int, name: str,
                  args: dict[str, Any] | None = None,
                  incoming: tuple[int, ...] = (), depth: int = 0) -> None:
        self.event(start_ns, TrackEvent.TYPE_SLICE_BEGIN, track_uuid,
                   name, args, incoming, depth)

    def instant(self, track_uuid: int, timestamp_ns: int, name: str,
                args: dict[str, Any] | None = None, depth: int = 0) -> None:
        self.event(timestamp_ns, TrackEvent.TYPE_INSTANT, track_uuid,
                   name, args, depth=depth)

    def serialize(self) -> bytes:
        def sort_key(event: PendingEvent) -> tuple[int, int, int, int]:
            if event.kind == TrackEvent.TYPE_SLICE_END:
                return (event.timestamp_ns, 0, -event.depth, event.order)
            if event.kind == TrackEvent.TYPE_SLICE_BEGIN:
                return (event.timestamp_ns, 1, event.depth, event.order)
            return (event.timestamp_ns, 2, event.depth, event.order)

        for event in sorted(self.events, key=sort_key):
            packet = self.builder.add_packet()
            packet.timestamp = event.timestamp_ns
            packet.trusted_packet_sequence_id = 1
            track_event = packet.track_event
            track_event.type = event.kind
            track_event.track_uuid = event.track_uuid
            if event.name is not None:
                track_event.name = event.name
            track_event.flow_ids.extend(event.flow_ids)
            for key, value in (event.args or {}).items():
                if value is None:
                    continue
                annotation = track_event.debug_annotations.add()
                annotation.name = key
                if isinstance(value, bool):
                    annotation.bool_value = value
                elif isinstance(value, int):
                    annotation.int_value = value
                elif isinstance(value, float):
                    annotation.double_value = value
                elif isinstance(value, str):
                    annotation.string_value = value
                else:
                    annotation.string_value = json.dumps(value, sort_keys=True)
        return self.builder.serialize()


def timestamp(value: str | None) -> datetime | None:
    if not value:
        return None
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def parse_run(value: str, repository: str | None) -> tuple[str, int]:
    if value.isdecimal():
        if not repository or not re.fullmatch(r"[^/]+/[^/]+", repository):
            raise TraceError("--repo OWNER/REPO is required with a numeric run ID")
        return repository, int(value)
    url = urlparse(value)
    match = re.fullmatch(r"/([^/]+)/([^/]+)/actions/runs/(\d+)/?", url.path)
    if url.netloc != "github.com" or not match:
        raise TraceError("expected a GitHub Actions run URL or numeric run ID")
    found_repo = f"{match[1]}/{match[2]}"
    if repository and repository.lower() != found_repo.lower():
        raise TraceError("--repo does not match the run URL")
    return found_repo, int(match[3])


def parse_event_payload(
    source: dict[str, Any], repository: str | None
) -> tuple[str, str, list[str], datetime | None, str | None, int | None]:
    found_repo = source.get("repository", {}).get("full_name")
    if repository and found_repo and repository.lower() != found_repo.lower():
        raise TraceError("--repo does not match the event repository")
    selected_repo = repository or found_repo
    if not selected_repo or not re.fullmatch(r"[^/]+/[^/]+", selected_repo):
        raise TraceError("event has no repository; supply --repo OWNER/REPO")
    if isinstance(source.get("merge_group"), dict):
        group = source["merge_group"]
        event = "merge_group"
        shas = [group.get("head_sha")]
        occurred = timestamp(group.get("created_at"))
        branch = group.get("head_ref")
        number = None
    elif isinstance(source.get("pull_request"), dict):
        pull_request = source["pull_request"]
        event = "pull_request"
        shas = [pull_request.get("merge_commit_sha"),
                pull_request.get("head", {}).get("sha")]
        date_key = "created_at" if source.get("action") == "opened" else "updated_at"
        occurred = timestamp(pull_request.get(date_key))
        branch = pull_request.get("head", {}).get("ref")
        number = pull_request.get("number")
    else:
        raise TraceError("expected a pull_request or merge_group webhook payload")
    shas = list(dict.fromkeys(sha for sha in shas if sha))
    if not shas:
        raise TraceError(f"{event} payload has no head SHA")
    if branch:
        branch = branch.removeprefix("refs/heads/")
    return selected_repo, event, shas, occurred, branch, number


class GitHub:
    def __init__(self, repository: str, max_concurrency: int = 50):
        owner, repo = repository.split("/", 1)
        token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
        if not token:
            raise TraceError("set GITHUB_TOKEN or GH_TOKEN to access the GitHub API")
        self.repository = repository
        self.api = GhApi(owner=owner, repo=repo, token=token)
        self.limit = asyncio.Semaphore(max_concurrency)
        self.commit_pull_requests: dict[str, asyncio.Task[set[int]]] = {}

    async def request(self, operation: Any, **params: Any) -> dict[str, Any]:
        try:
            async with self.limit:
                data = await operation(**params)
        except Exception as error:
            raise TraceError(f"GitHub API {operation.__name__} failed: {error}") from error
        if not isinstance(data, dict):
            raise TraceError(f"GitHub API {operation.__name__} returned an unexpected response")
        return plain_json(data)

    async def pages(self, operation: Any, key: str, **params: Any) -> list[dict[str, Any]]:
        items: list[dict[str, Any]] = []
        for page in range(1, 11):  # GitHub caps filtered run searches at 1,000.
            response = await self.request(operation, per_page=100, page=page, **params)
            batch = response.get(key)
            if not isinstance(batch, list):
                raise TraceError(f"{operation.__name__} has no {key} list")
            items.extend(batch)
            if len(batch) < 100:
                return items
        raise TraceError(f"{operation.__name__} returned at least 1,000 entries; narrow the input")

    async def get_run(self, run_id: int) -> dict[str, Any]:
        return await self.request(self.api.actions.get_workflow_run, run_id=run_id)

    async def runs(self, **filters: Any) -> list[dict[str, Any]]:
        return await self.pages(self.api.actions.list_workflow_runs_for_repo,
                                "workflow_runs", **filters)

    async def jobs(self, run_id: int) -> list[dict[str, Any]]:
        return await self.pages(self.api.actions.list_jobs_for_workflow_run,
                                "jobs", run_id=run_id, filter="latest")

    async def workflow_text(self, path: str, sha: str, repository: str | None = None) -> str:
        if not path.startswith(".github/workflows/") or ".." in Path(path).parts:
            raise TraceError(f"invalid workflow path: {path}")
        params = {}
        if repository is not None:
            params["owner"], params["repo"] = repository.split("/", 1)
        try:
            response = await self.request(self.api.repos.get_content, path=path, ref=sha, **params)
        except TraceError as error:
            code = getattr(error.__cause__, "code", None)
            if code in (401, 403, 404):
                raise WorkflowUnavailable(
                    f"{repository or self.repository}/{path}@{sha} cannot be read (HTTP {code})"
                ) from error
            raise
        if response.get("encoding") != "base64":
            raise TraceError(f"workflow {path} was not returned as base64 content")
        return base64.b64decode(response["content"]).decode("utf-8")

    async def pull_requests_for_commit(self, sha: str) -> set[int]:
        async def fetch() -> set[int]:
            operation = self.api.repos.list_pull_requests_associated_with_commit
            try:
                async with self.limit:
                    data = await operation(commit_sha=sha, per_page=100)
            except Exception as error:
                raise TraceError(f"GitHub API {operation.__name__} failed: {error}") from error
            if not isinstance(data, (list, L)):
                raise TraceError(f"GitHub API {operation.__name__} returned an unexpected response")
            return {pr["number"] for pr in data}

        if sha not in self.commit_pull_requests:
            self.commit_pull_requests[sha] = asyncio.create_task(fetch())
        return await self.commit_pull_requests[sha]


PR_MERGE_REF = re.compile(r"refs/pull/(\d+)/merge$")


def same_commit_local_workflow(item: dict[str, Any], repository: str) -> bool:
    path = item.get("path") or ""
    sha = item.get("sha")
    # Relative local calls have the resolved commit SHA in their API path.
    return (isinstance(sha, str) and bool(sha)
            and path.lower().startswith(f"{repository}/.github/workflows/".lower())
            and path.rpartition("@")[2] == sha)


def referenced_pull_workflows(
    run: dict[str, Any], repository: str
) -> dict[str, str]:
    """Return immutable SHAs for local workflows at a PR merge commit."""
    workflows: dict[str, str] = {}
    for item in run.get("referenced_workflows") or []:
        if not (same_commit_local_workflow(item, repository)
                and PR_MERGE_REF.fullmatch(item.get("ref") or "")):
            continue
        local_path = item["path"][len(repository) + 1:]
        local_path = local_path.split("@", 1)[0]
        sha = item.get("sha")
        workflows[local_path] = sha
    return workflows


async def run_pull_request_number(api: GitHub, run: dict[str, Any]) -> int | None:
    refs = {
        int(match[1])
        for item in run.get("referenced_workflows") or []
        if same_commit_local_workflow(item, api.repository)
        and (match := PR_MERGE_REF.fullmatch(item.get("ref") or ""))
    }
    listed = {pr["number"] for pr in run.get("pull_requests") or []}
    if len(refs) == 1:
        number = next(iter(refs))
        return number if not listed or number in listed else None
    if refs:
        return None
    if len(listed) == 1:
        return next(iter(listed))
    if listed:
        return None
    associated = await api.pull_requests_for_commit(run["head_sha"])
    return next(iter(associated)) if len(associated) == 1 else None


async def triggered_runs(api: GitHub, anchor: dict[str, Any], window: int,
                         warnings: list[str] | None = None) -> list[dict[str, Any]]:
    anchor_time = timestamp(anchor.get("created_at"))
    if anchor_time is None:
        raise TraceError("anchor run has no created_at timestamp")
    event = anchor.get("event")
    sha = anchor.get("head_sha")
    if not event or not sha:
        raise TraceError("anchor run has no event or head_sha")
    lower = anchor_time - timedelta(seconds=window)
    upper = anchor_time + timedelta(seconds=window)
    candidates = await api.runs(event=event, head_sha=sha)
    selected = [
        run
        for run in candidates
        if (created := timestamp(run.get("created_at"))) is not None
        and lower <= created <= upper
        and run.get("head_branch") == anchor.get("head_branch")
    ]
    if event == "pull_request":
        anchor_number = await run_pull_request_number(api, anchor)
        if anchor_number is None:
            raise TraceError(f"run {anchor['id']}: cannot identify its pull request")
        numbers = await asyncio.gather(*(run_pull_request_number(api, run)
                                         for run in selected))
        unknown = [run["id"] for run, number in zip(selected, numbers)
                   if number is None]
        if unknown and warnings is not None:
            warnings.append(f"excluded runs with ambiguous PR identity: {unknown}")
        selected = [run for run, number in zip(selected, numbers)
                    if number == anchor_number]
    if not any(run["id"] == anchor["id"] for run in selected):
        selected.append(anchor)
    return sorted(selected, key=lambda run: (run["created_at"], run["id"]))


async def anchor_from_event(
    api: GitHub, event: str, shas: list[str], occurred: datetime | None,
    branch: str | None, number: int | None, window: int
) -> dict[str, Any]:
    by_id: dict[int, dict[str, Any]] = {}
    searches = await asyncio.gather(*(
        api.runs(event=event, head_sha=sha)
        for sha in shas
    ))
    for runs in searches:
        for run in runs:
            by_id[run["id"]] = run
    async def filter_candidates(runs: list[dict[str, Any]]) -> list[dict[str, Any]]:
        selected = [
            run for run in runs
            if timestamp(run.get("created_at")) is not None
            and (not branch or run.get("head_branch") == branch)
        ]
        if number is not None:
            numbers = await asyncio.gather(*(run_pull_request_number(api, run)
                                             for run in selected))
            selected = [run for run, found in zip(selected, numbers)
                        if found == number]
        if occurred is None:
            return selected
        # The webhook timestamp may precede workflow creation by several minutes.
        return [
            run for run in selected
            if (created := timestamp(run["created_at"])) is not None
            and -timedelta(seconds=window) <= created - occurred <= timedelta(minutes=10)
        ]

    candidates = await filter_candidates(list(by_id.values()))
    if not candidates and event == "pull_request" and branch:
        # pull_request runs commonly use a synthetic merge commit as head_sha.
        candidates = await filter_candidates(
            await api.runs(event=event, branch=branch)
        )
    if occurred is not None:
        candidates.sort(key=lambda run: (abs(timestamp(run["created_at"]) - occurred),
                                         run["created_at"], run["id"]))
    else:
        candidates.sort(key=lambda run: (run["created_at"], run["id"]), reverse=True)
    if not candidates:
        raise TraceError(f"no {event} workflow runs found for this event")
    return await api.get_run(candidates[0]["id"])


def needs_list(value: Any) -> tuple[str, ...]:
    if isinstance(value, str):
        return (value,)
    if isinstance(value, list) and all(isinstance(item, str) for item in value):
        return tuple(value)
    return ()


async def workflow_definitions(
    text: str, path: str, sha: str | None, api: GitHub,
    cache: dict[tuple[str, str, str], asyncio.Task[str]],
    child_shas: dict[str, str] | None = None,
    references: list[dict[str, Any]] | None = None,
) -> list[JobDefinition]:
    definitions: list[JobDefinition] = []
    child_shas = child_shas or {}
    recorded = {item["path"]: item["sha"] for item in references or []
                if item.get("path") and item.get("sha")}

    async def visit(source: str, source_path: str, prefix: tuple[str, ...],
                    seen: set[tuple[str, str, str | None]], source_sha: str | None,
                    repository: str) -> None:
        try:
            workflow = yaml.load(source, Loader=yaml.BaseLoader)
        except yaml.YAMLError as error:
            raise TraceError(f"cannot parse {source_path}: {error}") from error
        jobs = workflow.get("jobs") if isinstance(workflow, dict) else None
        if not isinstance(jobs, dict):
            raise TraceError(f"{source_path} has no jobs mapping")
        children = {}
        # Start sibling downloads together, then visit them in declaration order.
        for key, value in jobs.items():
            if not isinstance(value, dict) or not value.get("uses"):
                continue
            uses = str(value["uses"])
            if uses.startswith("./.github/workflows/"):
                child_repo, child_path = repository, uses[2:]
                child_sha = (child_shas.get(child_path, source_sha)
                             if repository == api.repository else source_sha)
            else:
                match = re.fullmatch(r"([^/]+/[^/]+)/(\.github/workflows/[^@]+)@(.+)", uses)
                if not match:
                    raise TraceError(f"unsupported reusable workflow reference: {uses}")
                child_repo, child_path, ref = match.groups()
                child_sha = recorded.get(uses)
                if child_sha is None and re.fullmatch(r"[0-9a-fA-F]{40}", ref):
                    child_sha = ref
            if child_sha is None:
                raise TraceError(f"cannot resolve executed revision of {uses}")
            identity = (child_repo, child_path, child_sha)
            if identity in seen:
                raise TraceError(f"recursive reusable workflow: {uses}")
            children[key] = identity
            if identity not in cache:
                cache[identity] = asyncio.create_task(
                    api.workflow_text(child_path, child_sha)
                    if child_repo == api.repository else
                    api.workflow_text(child_path, child_sha, child_repo)
                )
        for key, value in jobs.items():
            if not isinstance(value, dict):
                continue
            job_path = (*prefix, str(key))
            definitions.append(JobDefinition(
                job_path, str(value.get("name", key)), needs_list(value.get("needs")),
                isinstance(value.get("strategy"), dict) and "matrix" in value["strategy"],
                bool(value.get("uses")),
            ))
            if key in children:
                identity = children[key]
                child_repo, child_path, child_sha = identity
                try:
                    child_source = await cache[identity]
                except WorkflowUnavailable as error:
                    if child_repo == api.repository:
                        raise
                    definitions[-1] = replace(definitions[-1], unavailable=str(error))
                    continue
                await visit(child_source, child_path, job_path,
                            seen | {identity}, child_sha, child_repo)

    await visit(text, path, (), {(api.repository, path, sha)}, sha, api.repository)
    return definitions


EXPRESSION = re.compile(r"\$\{\{\s*(.*?)\s*\}\}")


def name_pattern(
    definition: JobDefinition, by_path: dict[tuple[str, ...], JobDefinition]
) -> tuple[re.Pattern[str], dict[str, str], int]:
    matrix_groups: dict[str, str] = {}
    def fragment(item: JobDefinition, capture_matrix: bool) -> str:
        pieces = []
        offset = 0
        for match in EXPRESSION.finditer(item.name):
            pieces.append(re.escape(item.name[offset : match.start()]))
            expression = match[1].strip()
            matrix_match = re.fullmatch(r"matrix\.([A-Za-z_][A-Za-z_0-9-]*)", expression)
            if matrix_match and capture_matrix:
                group = f"matrix_{len(matrix_groups)}"
                matrix_groups[group] = matrix_match[1]
                pieces.append(f"(?P<{group}>.*?)")
            else:
                pieces.append(".*?")
            offset = match.end()
        pieces.append(re.escape(item.name[offset:]))
        pattern = "".join(pieces)
        if item.matrix and not EXPRESSION.search(item.name):
            # GitHub truncates long rendered matrix names with an ellipsis.
            pattern += r"(?: \((?:.*\)|.*\.\.\.))?"
        return pattern

    parents = []
    parent_definitions = []
    for depth in range(1, len(definition.path)):
        parent = by_path[definition.path[:depth]]
        parent_definitions.append(parent)
        rendered = fragment(parent, False)
        if EXPRESSION.search(parent.name):
            # GitHub also uses the caller key when its display name is dynamic.
            rendered = f"(?:{rendered}|{re.escape(parent.path[-1])})"
        parents.append(f"(?P<caller_{depth}>{rendered})")
    rendered = fragment(definition, True)
    if definition.unavailable and EXPRESSION.search(definition.name):
        rendered = f"(?:{rendered}|{re.escape(definition.path[-1])})"
    pattern = r" / ".join([*parents, rendered])
    if definition.unavailable:
        # The API still reports child job names beneath this caller.
        pattern += r" / .+"
    specificity = sum(len(EXPRESSION.sub("", item.name)) for item in
                      [*parent_definitions, definition])
    return re.compile(f"^{pattern}$"), matrix_groups, specificity


def match_jobs(
    jobs: list[dict[str, Any]], definitions: list[JobDefinition]
) -> tuple[list[MatchedJob], list[str]]:
    by_path = {definition.path: definition for definition in definitions}
    patterns = [(definition, *name_pattern(definition, by_path))
                for definition in definitions]
    matched: list[MatchedJob] = []
    warnings: list[str] = []
    for job in jobs:
        candidates = [
            (definition, match, groups, specificity)
            for definition, pattern, groups, specificity in patterns
            if (match := pattern.fullmatch(job.get("name", ""))) is not None
        ]
        if candidates:
            best = max(candidate[3] for candidate in candidates)
            candidates = [candidate for candidate in candidates if candidate[3] == best]
        if len(candidates) == 1:
            definition, match, groups, _ = candidates[0]
            matrix = {name: match[group] for group, name in groups.items()
                      if match[group] is not None}
            invocation = tuple(match[f"caller_{depth}"]
                               for depth in range(1, len(definition.path)))
            matched.append(MatchedJob(job, definition, matrix, invocation))
        else:
            warnings.append(
                f"job {job.get('id')} ({job.get('name')}) matched {len(candidates)} workflow definitions"
            )
    return matched, warnings


def dependencies(matched: list[MatchedJob], definitions: list[JobDefinition]) -> set[tuple[int, int]]:
    by_path = {definition.path: definition for definition in definitions}
    edges: set[tuple[int, int]] = set()
    for target in matched:
        path = target.definition.path
        for depth in range(1, len(path) + 1):
            definition = by_path[path[:depth]]
            if not definition.needs:
                continue
            # A caller's prerequisites gate the entry jobs of its reusable workflow.
            descendants = [by_path[path[:index]] for index in range(depth + 1, len(path) + 1)]
            if any(child.needs for child in descendants):
                continue
            for needed in definition.needs:
                needed_path = (*path[: depth - 1], needed)
                for source in matched:
                    if (source.definition.path[:depth] == needed_path
                            and source.invocation[:depth - 1] == target.invocation[:depth - 1]):
                        edges.add((source.job["id"], target.job["id"]))
    return edges


def microseconds(value: datetime, origin: datetime) -> int:
    delta = value - origin
    return (delta.days * 86400 + delta.seconds) * 1_000_000 + delta.microseconds


def observed_critical_path(
    jobs: list[dict[str, Any]], edges: set[tuple[int, int]]
) -> list[int]:
    """Follow the predecessor that last finished before each job started."""
    by_id = {job["id"]: job for job in jobs}
    timed = [
        job for job in jobs
        if (started := job_execution_start(job)) is not None
        and (completed := timestamp(job.get("completed_at"))) is not None
        and completed > started and job.get("conclusion") != "skipped"
    ]
    if not timed:
        return []
    tail = max(timed, key=lambda job: (job["completed_at"], job["id"]))
    path = [tail["id"]]
    while True:
        target = by_id[path[-1]]
        target_start = job_execution_start(target)
        if target_start is None:
            break
        candidates = [
            by_id[source_id]
            for source_id, target_id in edges
            if target_id == target["id"] and source_id in by_id
            and source_id not in path
            and (finished := timestamp(by_id[source_id].get("completed_at"))) is not None
            and finished <= target_start
            and by_id[source_id].get("conclusion") != "skipped"
        ]
        if not candidates:
            break
        parent = max(candidates, key=lambda job: (job["completed_at"], job["id"]))
        path.append(parent["id"])
    return path[::-1]


def child_events_fit(job: dict[str, Any]) -> bool:
    """Only nest steps when their intervals fit without sibling overlap."""
    completed = timestamp(job.get("completed_at"))
    job_start = job_execution_start(job)
    if job_start is None or completed is None or completed <= job_start:
        return False
    intervals = []
    for step in job.get("steps") or []:
        if step.get("conclusion") == "skipped":
            continue
        start = timestamp(step.get("started_at"))
        end = timestamp(step.get("completed_at"))
        if start and end and end >= start:
            intervals.append((start, end))
    return all(
        job_start <= start <= end <= completed
        and (index == 0 or intervals[index - 1][1] <= start)
        for index, (start, end) in enumerate(intervals)
    )


def step_spans(job: dict[str, Any], origin: datetime,
               sequential: bool) -> list[StepSpan]:
    """Give same-second steps ordered, estimated spans within the job."""
    spans = []
    raw_starts = []
    for step in job.get("steps") or []:
        if step.get("conclusion") == "skipped":
            continue
        start = timestamp(step.get("started_at"))
        end = timestamp(step.get("completed_at"))
        if start is None or end is None or end < start:
            continue
        start_ns = microseconds(start, origin) * 1000
        end_ns = microseconds(end, origin) * 1000
        spans.append(StepSpan(step, start_ns, end_ns))
        raw_starts.append(start_ns)

    if not sequential:
        for span in spans:
            if span.start_ns == span.end_ns:
                span.end_ns += 1
                span.estimated = True
        return spans

    second_ns = 1_000_000_000
    completed = timestamp(job.get("completed_at"))
    job_end = microseconds(completed, origin) * 1000 if completed else None
    started = timestamp(job.get("started_at"))
    job_start = microseconds(started, origin) * 1000 if started else None

    index = 0
    while index < len(spans):
        at = raw_starts[index]
        following = index + 1
        while following < len(spans) and raw_starts[following] == at:
            following += 1
        group = spans[index:following]
        zero_count = sum(span.end_ns == at for span in group)
        if not zero_count:
            index = following
            continue

        # A timed step at this timestamp can give its first second to the
        # preceding same-second steps, while retaining its recorded end.
        if group[-1].end_ns > at:
            upper = min(at + second_ns, group[-1].end_ns)
            if upper - at >= len(group):
                for position, span in enumerate(group):
                    span.start_ns = at + (upper - at) * position // len(group)
                    if position < len(group) - 1:
                        span.end_ns = at + (upper - at) * (position + 1) // len(group)
                    span.estimated = True
                index = following
                continue

        # A group containing only same-second steps uses the gap to the next
        # step, bounded by one second and the job's recorded completion.
        if zero_count == len(group):
            bounds = [at + second_ns]
            if following < len(spans):
                bounds.append(raw_starts[following])
            if job_end is not None:
                bounds.append(job_end)
            upper = min(bounds)
            if upper - at >= len(group):
                for position, span in enumerate(group):
                    span.start_ns = at + (upper - at) * position // len(group)
                    span.end_ns = at + (upper - at) * (position + 1) // len(group)
                    span.estimated = True
                index = following
                continue

            # Terminal steps at the job completion need time from the
            # preceding step or the last second of an idle gap.
            previous = spans[index - 1] if index else None
            if previous and previous.end_ns == at and previous.start_ns < at:
                lower = max(at - second_ns, previous.start_ns)
                if at - lower >= len(group) + 1:
                    width = at - lower
                    previous.end_ns = lower + width // (len(group) + 1)
                    previous.estimated = True
                    for position, span in enumerate(group, start=1):
                        span.start_ns = lower + width * position // (len(group) + 1)
                        span.end_ns = lower + width * (position + 1) // (len(group) + 1)
                        span.estimated = True
                    index = following
                    continue
            lower_bound = previous.end_ns if previous else job_start
            lower = max(at - second_ns,
                        lower_bound if lower_bound is not None else at)
            if at - lower >= len(group):
                for position, span in enumerate(group):
                    span.start_ns = lower + (at - lower) * position // len(group)
                    span.end_ns = lower + (at - lower) * (position + 1) // len(group)
                    span.estimated = True
                index = following
                continue

        # No interval exists inside the recorded job bounds. Keep the steps
        # non-instant, but place them on child tracks rather than overlap.
        for position, span in enumerate(group):
            if span.start_ns == span.end_ns:
                span.start_ns = at + position
                span.end_ns = at + position + 1
                span.estimated = True
        index = following

    return spans


def step_spans_fit(job: dict[str, Any], spans: list[StepSpan],
                   origin: datetime) -> bool:
    completed = timestamp(job.get("completed_at"))
    job_start = job_execution_start(job)
    if job_start is None or completed is None or completed <= job_start:
        return False
    previous_end = microseconds(job_start, origin) * 1000
    job_end = microseconds(completed, origin) * 1000
    for span in spans:
        if (span.start_ns < previous_end or span.end_ns > job_end
                or span.end_ns <= span.start_ns):
            return False
        previous_end = span.end_ns
    return True


def open_job_steps_fit(job: dict[str, Any], spans: list[StepSpan],
                       origin: datetime) -> bool:
    """Keep completed and active steps nested when their starts are ordered."""
    job_start = job_execution_start(job)
    if job_start is None:
        return False
    previous_end = microseconds(job_start, origin) * 1000
    for span in spans:
        if span.start_ns < previous_end or span.end_ns <= span.start_ns:
            return False
        previous_end = span.end_ns

    steps = job.get("steps") or []
    open_steps = [
        (index, step) for index, step in enumerate(steps)
        if step.get("conclusion") != "skipped"
        and timestamp(step.get("started_at")) is not None
        and timestamp(step.get("completed_at")) is None
    ]
    if len(open_steps) > 1:
        return False
    if open_steps:
        index, open_step = open_steps[0]
        if any(step.get("completed_at") for step in steps[index + 1:]):
            return False
        open_start = timestamp(open_step["started_at"])
        assert open_start is not None
        if microseconds(open_start, origin) * 1000 < previous_end:
            return False
    return True


def append_trace(
    writer: TraceWriter,
    repository: str,
    anchor: dict[str, Any],
    runs: list[dict[str, Any]],
    jobs_by_run: dict[int, list[dict[str, Any]]],
    definitions_by_run: dict[int, list[JobDefinition] | None],
    root_name: str | None = None,
    root_rank: int = 0,
    captured_at: datetime | None = None,
) -> list[str]:
    origin = min(timestamp(run["created_at"]) for run in runs)
    assert origin is not None
    warnings: list[str] = []

    def ns(value: datetime) -> int:
        return microseconds(value, origin) * 1000

    def elapsed_span(track: int, name: str, start: datetime | None,
                     members: list[dict[str, Any]], partial: bool,
                     args: dict[str, Any] | None = None,
                     end: datetime | None = None) -> None:
        starts = [start] if start is not None else []
        for member in members:
            execution_start = job_execution_start(member)
            created = timestamp(member.get("created_at"))
            if execution_start is not None:
                starts.append(execution_start)
            if created is not None and not carried_forward_job(member):
                starts.append(created)
        start = min(starts, default=None)
        if start is None:
            return
        if any(carried_forward_job(member) for member in members):
            args = {**(args or {}), "timingNote":
                    "Elapsed interval includes jobs carried forward from earlier "
                    "attempts and any time between attempts."}
        if partial:
            writer.open_span(track, ns(start), name, args)
            return
        ends = [end] if end is not None else []
        ends.extend(completed for member in members
                    if (completed := timestamp(member.get("completed_at"))) is not None)
        end = max(ends, default=None)
        if end and end > start:
            writer.span(track, ns(start), ns(end), name, args)

    captured_at = captured_at or datetime.now(timezone.utc)
    incomplete_run_ids = [
        run["id"] for run in runs
        if run.get("status") != "completed"
        or any(job.get("status") != "completed"
               for job in jobs_by_run[run["id"]])
    ]
    unavailable_by_run = {
        run["id"]: [definition.unavailable
                    for definition in definitions_by_run.get(run["id"]) or []
                    if definition.unavailable]
        for run in runs
    }
    unverified_run_ids = [run["id"] for run in runs
                         if definitions_by_run.get(run["id"]) is None
                         or unavailable_by_run[run["id"]]]
    critical_path_available = not incomplete_run_ids and not unverified_run_ids
    critical_path_note = " ".join(
        note for condition, note in (
            (incomplete_run_ids,
             "Critical paths cannot be calculated while workflow runs are incomplete."),
            (unverified_run_ids,
             "Critical paths cannot be calculated without complete executed workflow definitions."),
        ) if condition
    )
    if incomplete_run_ids:
        warnings.append(
            f"critical paths unavailable: incomplete runs {incomplete_run_ids}"
        )
    if unverified_run_ids:
        for run_id in unverified_run_ids:
            if unavailable_by_run[run_id]:
                warnings.extend(
                    f"run {run_id}: workflow definition unavailable: {note}; "
                    "job and step timings retained, internal dependencies unavailable"
                    for note in unavailable_by_run[run_id]
                )
            else:
                warnings.append(
                    f"dependency data and critical paths unavailable: executed workflow "
                    f"revision unknown for runs [{run_id}]"
                )

    run_graphs: dict[int, tuple[dict[int, MatchedJob], set[tuple[int, int]], list[int]]] = {}
    for run in runs:
        run_id = run["id"]
        definitions = definitions_by_run.get(run_id)
        if definitions is None:
            matched, edges = [], set()
        else:
            matched, match_warnings = match_jobs(jobs_by_run[run_id], definitions)
            warnings.extend(f"run {run_id}: {warning}" for warning in match_warnings)
            edges = dependencies(matched, definitions)
        path = (observed_critical_path(jobs_by_run[run_id], edges)
                if critical_path_available else [])
        run_graphs[run_id] = ({item.job["id"]: item for item in matched}, edges, path)

    path_end_by_run = {
        run_id: next(job["completed_at"] for job in jobs_by_run[run_id]
                     if job["id"] == path[-1])
        for run_id, (_, _, path) in run_graphs.items() if path
    }
    critical_workflow = max(
        (run for run in runs if run_graphs[run["id"]][2]),
        key=lambda run: timestamp(path_end_by_run[run["id"]]),
        default=None,
    )
    trigger_args = {
        "event": anchor["event"],
        "headSha": anchor["head_sha"],
        "anchorRun": anchor["html_url"],
        "originUtc": origin.isoformat(),
        "workflows": len(runs),
        "criticalWorkflowRun": critical_workflow["id"] if critical_workflow else None,
        "criticalPathAvailable": critical_path_available,
        "incompleteRunIds": incomplete_run_ids,
        "unverifiedWorkflowRunIds": unverified_run_ids,
    }
    # Empty parent tracks get Perfetto's child-activity summary when collapsed.
    root = writer.track(root_name or f"GitHub Actions: {repository}", rank=root_rank,
                        description=json.dumps(trigger_args, sort_keys=True))
    critical_group = writer.track("[meta] Observed critical paths", root,
                                  rank=len(runs) + 1,
                                  description=json.dumps({
                                      "available": critical_path_available,
                                      "note": critical_path_note or None,
                                      "incompleteRunIds": incomplete_run_ids,
                                      "unverifiedWorkflowRunIds": unverified_run_ids,
                                  }, sort_keys=True))
    if not critical_path_available:
        marker_time = captured_at if incomplete_run_ids else max(
            [timestamp(run["created_at"]) for run in runs]
            + [timestamp(job.get("completed_at"))
               for jobs in jobs_by_run.values() for job in jobs
               if timestamp(job.get("completed_at")) is not None]
        )
        writer.instant(critical_group, ns(marker_time), "Critical path unavailable",
                       {"note": critical_path_note,
                        "incompleteRunIds": incomplete_run_ids,
                        "unverifiedWorkflowRunIds": unverified_run_ids,
                        "capturedAt": captured_at.isoformat() if incomplete_run_ids else None})

    for run_order, run in enumerate(runs, start=1):
        run_id = run["id"]
        jobs = jobs_by_run[run_id]
        job_by_id = {job["id"]: job for job in jobs}
        by_id, edges, path = run_graphs[run_id]
        definitions = definitions_by_run.get(run_id) or []
        definition_order = {definition.path: index
                            for index, definition in enumerate(definitions)}
        matrix_paths = {definition.path for definition in definitions if definition.matrix}
        matrix_group_paths: dict[int, tuple[str, ...]] = {}
        for job_id, item in by_id.items():
            definition_path = item.definition.path
            group = next((definition_path[:depth]
                          for depth in range(1, len(definition_path) + 1)
                          if definition_path[:depth] in matrix_paths), None)
            if group:
                matrix_group_paths[job_id] = group
        api_order = {job["id"]: index for index, job in enumerate(jobs)}

        def row_order(job: dict[str, Any]) -> tuple[int, int, tuple[str, ...], int]:
            job_id = job["id"]
            item = by_id.get(job_id)
            if item is None:
                return (len(definitions), len(definitions), (), api_order[job_id])
            definition_index = definition_order[item.definition.path]
            group_path = matrix_group_paths.get(job_id)
            group_index = definition_order[group_path] if group_path else definition_index
            return (group_index, definition_index, tuple(item.matrix.values()),
                    api_order[job_id])

        ordered_jobs = sorted(jobs, key=row_order)
        job_slots = {job["id"]: slot for slot, job in enumerate(ordered_jobs, start=1)}
        incoming = {job["id"]: [] for job in jobs}
        outgoing = {job["id"]: [] for job in jobs}
        incoming_flows: dict[int, list[int]] = {job["id"]: [] for job in jobs}
        outgoing_flows: dict[int, list[int]] = {job["id"]: [] for job in jobs}
        for source_id, target_id in sorted(edges):
            incoming[target_id].append(source_id)
            outgoing[source_id].append(target_id)
            source = job_by_id[source_id]
            target = job_by_id[target_id]
            source_start = job_execution_start(source)
            source_completed = timestamp(source.get("completed_at"))
            target_ready = (job_execution_start(target) if carried_forward_job(target)
                            else timestamp(target.get("created_at")))
            target_completed = timestamp(target.get("completed_at"))
            if (source_start is None or source_completed is None
                    or target_ready is None
                    or source_start >= source_completed
                    or (target_completed is not None
                        and target_ready >= target_completed)
                    or source_completed > target_ready):
                continue
            flow_id = writer.flow_id()
            outgoing_flows[source_id].append(flow_id)
            incoming_flows[target_id].append(flow_id)

        is_latest_finishing = (
            critical_workflow is not None and run_id == critical_workflow["id"]
            if critical_path_available else None
        )
        workflow_args = {
            "url": run["html_url"],
            "status": run.get("status"),
            "conclusion": run.get("conclusion"),
            "dependencyEdges": len(edges),
            "renderedDependencyFlows": sum(map(len, outgoing_flows.values())),
            "observedCriticalPath": path if critical_path_available else None,
            "isLatestFinishingWorkflow": is_latest_finishing,
            "workflowDefinitionsVerified": run_id not in unverified_run_ids,
            "unavailableWorkflowDefinitions": unavailable_by_run[run_id],
        }
        workflow_track = writer.track(
            f"{run['name']} ({run_id})", root, rank=run_order,
            description=json.dumps(workflow_args, sort_keys=True),
        )

        run_created = timestamp(run.get("created_at"))
        attempt_started = timestamp(run.get("run_started_at"))
        if run.get("run_attempt", 1) > 1:
            # A rerun retains the original run's created_at. Do not turn the
            # time between attempts into a workflow startup interval.
            interval_start = (attempt_started if attempt_started and run_created
                              and attempt_started > run_created else None)
        else:
            interval_start = run_created
        first_job_created = min(
            (created for job in jobs
             if (created := timestamp(job.get("created_at"))) is not None),
            default=None,
        )
        has_startup_gap = bool(interval_start and first_job_created
                               and first_job_created > interval_start)
        if len(jobs) + has_startup_gap > PERFETTO_SUMMARY_TRACK_LIMIT:
            # Perfetto's collapsed summary only reads its first eight slice
            # tracks. Put the full interval on the workflow's own track.
            elapsed_span(workflow_track, "Workflow elapsed",
                         interval_start or first_job_created, jobs,
                         run.get("status") != "completed",
                         {"url": run["html_url"]},
                         end=first_job_created if has_startup_gap else None)
        if has_startup_gap:
            startup_track = writer.track("Before first job", workflow_track, rank=0)
            writer.span(startup_track, ns(interval_start), ns(first_job_created),
                        "Waiting for first job record",
                        {"intervalStart": interval_start.isoformat(),
                         "firstJobCreatedAt": first_job_created.isoformat(),
                         "timingNote": "No job record exists in this interval; "
                                       "the cause is not available from the Actions API.",
                         "url": run.get("html_url")})

        matrix_tracks: dict[tuple[str, ...], int] = {}
        job_tracks: dict[int, int] = {}
        for job in ordered_jobs:
            job_id = job["id"]
            slot = job_slots[job_id]
            group_path = matrix_group_paths.get(job_id)
            parent = workflow_track
            if group_path:
                if group_path not in matrix_tracks:
                    group_name = "/".join(group_path)
                    leg_count = sum(matrix_group_paths.get(member["id"]) == group_path
                                    for member in jobs)
                    group_track = writer.track(f"Matrix: {group_name}", workflow_track,
                                               rank=slot,
                                               description=json.dumps(
                                                   {"jobKey": group_name,
                                                    "legs": leg_count},
                                                   sort_keys=True))
                    matrix_tracks[group_path] = group_track
                    if leg_count > PERFETTO_SUMMARY_TRACK_LIMIT:
                        legs = [member for member in jobs
                                if matrix_group_paths.get(member["id"]) == group_path]
                        first_created = min(
                            (created for member in legs
                             if (created := timestamp(member.get("created_at")))
                             is not None), default=None)
                        elapsed_span(group_track, "Matrix elapsed", first_created,
                                     legs, any(member.get("status") != "completed"
                                               for member in legs))
                parent = matrix_tracks[group_path]
            job_tracks[job_id] = writer.track(job["name"], parent, rank=slot)

        open_jobs = {
            job["id"]: (timestamp(job.get("started_at")) is not None
                        and timestamp(job.get("completed_at")) is None
                        and job.get("status") != "completed")
            for job in jobs
        }
        timed_steps_by_job = {
            job["id"]: step_spans(
                job, origin, child_events_fit(job) or open_jobs[job["id"]]
            ) for job in jobs
        }
        nested_by_job = {
            job["id"]: (
                open_job_steps_fit(job, timed_steps_by_job[job["id"]], origin)
                if open_jobs[job["id"]] else
                child_events_fit(job)
                and step_spans_fit(job, timed_steps_by_job[job["id"]], origin)
            )
            for job in jobs
        }
        path_ids = set(path)
        for job in jobs:
            job_id = job["id"]
            job_track_uuid = job_tracks[job_id]
            created = timestamp(job.get("created_at"))
            started = timestamp(job.get("started_at"))
            completed = timestamp(job.get("completed_at"))
            job_start = job_execution_start(job)
            queued = bool(created and started and started > created
                          and (completed is None or started <= completed))
            nested = nested_by_job[job_id]
            timed_steps = timed_steps_by_job[job_id]
            if not nested and timed_steps:
                warnings.append(f"run {run_id}: job {job_id} steps cannot be nested in job timing")
            item = by_id.get(job_id)
            definition_details = {}
            if item and item.definition.unavailable:
                definition_details = {
                    "workflowDefinitionAvailable": False,
                    "unavailableWorkflowDefinition": item.definition.unavailable,
                    "dependencyNote": "Job and step timings are from the Actions API; "
                                      "internal dependencies are unavailable. "
                                      "Only known caller dependencies are shown.",
                }
            job_args: dict[str, Any] = {
                **definition_details,
                "jobId": job_id,
                "url": job.get("html_url"),
                "status": job.get("status"),
                "conclusion": job.get("conclusion"),
                "runner": job.get("runner_name"),
                "started_at": job.get("started_at"),
                "completed_at": job.get("completed_at"),
                "needs": [job_by_id[needed]["name"] for needed in incoming[job_id]],
                "requiredBy": [job_by_id[dependent]["name"]
                               for dependent in outgoing[job_id]],
                "onObservedCriticalPath": (job_id in path_ids
                                           if critical_path_available else None),
            }
            if carried_forward_job(job):
                job_args["created_at"] = job.get("created_at")
                job_args["carriedForwardFromEarlierAttempt"] = True
                job_args["timingNote"] = (
                    "This job was not rerun. GitHub copied its earlier start, "
                    "completion and steps into this attempt with a new created_at."
                )
            elif started and job_start != started:
                job_args["created_at"] = job.get("created_at")
                job_args["timingNote"] = (
                    "GitHub reports started_at before created_at; "
                    "the displayed job starts at created_at."
                )
            skipped_steps = [
                {"number": step.get("number"), "name": step["name"]}
                for step in job.get("steps") or []
                if step.get("conclusion") == "skipped"
            ]
            if skipped_steps:
                job_args["skippedSteps"] = skipped_steps
            if item:
                key = "workflowCallerKey" if item.definition.unavailable else "jobKey"
                job_args[key] = "/".join(item.definition.path)
                if job_id in matrix_group_paths:
                    job_args["matrixGroup"] = "/".join(matrix_group_paths[job_id])
                if item.matrix:
                    job_args["matrix"] = item.matrix
            if queued:
                assert created is not None and started is not None
                writer.span(job_track_uuid, ns(created), ns(started), "Queued",
                            {"created_at": job.get("created_at"),
                             "started_at": job.get("started_at"), **definition_details},
                            incoming=tuple(incoming_flows[job_id]))
            elif created and started is None and job.get("status") != "completed":
                writer.open_span(job_track_uuid, ns(created), "Queued",
                                 {"created_at": job.get("created_at"),
                                  "status": job.get("status"), **definition_details},
                                 incoming=tuple(incoming_flows[job_id]))
            if job_start and completed and completed > job_start:
                writer.span(job_track_uuid, ns(job_start), ns(completed), job["name"],
                            job_args, () if queued else tuple(incoming_flows[job_id]),
                            tuple(outgoing_flows[job_id]))
            elif open_jobs[job_id]:
                assert job_start is not None
                writer.open_span(job_track_uuid, ns(job_start), job["name"], job_args,
                                 () if queued else tuple(incoming_flows[job_id]))
            elif started or completed or (created and job.get("status") == "completed"):
                marker = job_start or completed
                assert marker is not None
                writer.instant(job_track_uuid, ns(marker), job["name"], job_args)

            child_rank = 1
            for timed_step in timed_steps:
                step = timed_step.step
                child_track = job_track_uuid
                depth = 1
                if not nested:
                    child_track = writer.track(step["name"], job_track_uuid,
                                               rank=child_rank)
                    depth = 0
                step_args = {**definition_details, "number": step.get("number"),
                             "url": step_url(job, step),
                             "status": step.get("status"),
                             "conclusion": step.get("conclusion"),
                             "onObservedCriticalPath": (job_id in path_ids
                                                       if critical_path_available else None)}
                step_args.update(step_timing_details(timed_step))
                writer.span(child_track, timed_step.start_ns, timed_step.end_ns,
                            step["name"], step_args, depth=depth)
                child_rank += 1

            if open_jobs[job_id]:
                for step in job.get("steps") or []:
                    step_start = timestamp(step.get("started_at"))
                    if (step.get("conclusion") == "skipped" or step_start is None
                            or timestamp(step.get("completed_at")) is not None):
                        continue
                    child_track = job_track_uuid
                    depth = 1
                    if not nested:
                        child_track = writer.track(step["name"], job_track_uuid,
                                                   rank=child_rank)
                        depth = 0
                    writer.open_span(child_track, ns(step_start), step["name"],
                                     {"number": step.get("number"),
                                      "url": step_url(job, step),
                                      "status": step.get("status"),
                                      **definition_details}, depth=depth)
                    child_rank += 1

        if path:
            critical_track = writer.track(f"{run['name']} ({run_id})",
                                          critical_group, rank=run_order)
            previous_end = timestamp(run.get("created_at"))
            for path_id in path:
                critical_job = job_by_id[path_id]
                critical_start = job_execution_start(critical_job)
                critical_end = timestamp(critical_job.get("completed_at"))
                if critical_start is None or critical_end is None or critical_end <= critical_start:
                    continue
                if previous_end and critical_start > previous_end:
                    writer.span(critical_track, ns(previous_end), ns(critical_start),
                                "Waiting for job start")
                writer.span(critical_track, ns(critical_start), ns(critical_end),
                            critical_job["name"],
                            {"jobId": path_id, "url": critical_job.get("html_url")})
                if nested_by_job[path_id]:
                    for timed_step in timed_steps_by_job[path_id]:
                        if (ns(critical_start) <= timed_step.start_ns
                                and timed_step.end_ns <= ns(critical_end)):
                            step = timed_step.step
                            step_args = {"jobId": path_id,
                                         "number": step.get("number"),
                                         "url": step_url(critical_job, step)}
                            step_args.update(step_timing_details(timed_step))
                            writer.span(critical_track, timed_step.start_ns,
                                        timed_step.end_ns, step["name"],
                                        step_args, depth=1)
                previous_end = critical_end

    return warnings


def build_trace(
    repository: str,
    anchor: dict[str, Any],
    runs: list[dict[str, Any]],
    jobs_by_run: dict[int, list[dict[str, Any]]],
    definitions_by_run: dict[int, list[JobDefinition] | None],
    captured_at: datetime | None = None,
) -> TraceOutput:
    writer = TraceWriter()
    warnings = append_trace(writer, repository, anchor, runs,
                            jobs_by_run, definitions_by_run,
                            captured_at=captured_at)
    return TraceOutput(writer.serialize(), warnings)


async def load_source(
    source_arg: str, args: argparse.Namespace, writer: TraceWriter, source_index: int
) -> tuple[int, int, list[str]]:
    event_path = Path(source_arg)
    if event_path.is_file():
        try:
            payload = json.loads(event_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as error:
            raise TraceError(f"cannot parse event JSON: {error}") from error
        if not isinstance(payload, dict):
            raise TraceError("event JSON must be an object")
        repository, event, shas, occurred, branch, number = parse_event_payload(
            payload, args.repo
        )
        api = GitHub(repository, args.max_concurrency)
        anchor = await anchor_from_event(api, event, shas, occurred, branch, number,
                                         args.event_window_seconds)
        run_id = anchor["id"]
    else:
        repository, run_id = parse_run(source_arg, args.repo)
        api = GitHub(repository, args.max_concurrency)
        anchor = await api.get_run(run_id)
    source_warnings: list[str] = []
    runs = [anchor] if args.single_run else await triggered_runs(
        api, anchor, args.event_window_seconds, source_warnings
    )
    sources: dict[tuple[str, str, str], asyncio.Task[str]] = {}
    referenced_shas = {run["id"]: referenced_pull_workflows(run, repository)
                       for run in runs}
    known_shas = {sha for workflows in referenced_shas.values()
                  for sha in workflows.values()}
    if anchor.get("event") == "pull_request" and len(known_shas) > 1:
        raise TraceError("grouped pull request runs reference different merge commits")
    explicit_sha = getattr(args, "workflow_sha", None)
    if explicit_sha and known_shas and explicit_sha not in known_shas:
        raise TraceError("--workflow-sha disagrees with the run's recorded workflow SHA")
    pull_request_sha = explicit_sha or (next(iter(known_shas)) if known_shas else None)

    async def load_run(current: dict[str, Any]) -> tuple[list[dict[str, Any]],
                                                         list[JobDefinition] | None]:
        current_id = current["id"]
        jobs_task = asyncio.create_task(api.jobs(current_id))
        workflow_path, _, path_ref = str(current.get("path", "")).partition("@")
        workflow_sha = (pull_request_sha if current.get("event") == "pull_request"
                        else explicit_sha or current.get("head_sha"))
        if workflow_sha is None and re.fullmatch(r"[0-9a-fA-F]{40}", path_ref):
            workflow_sha = path_ref
        manual_source = current_id == run_id and args.workflow_file
        try:
            if workflow_sha is None and not manual_source:
                return await jobs_task, None
            if manual_source:
                source = args.workflow_file.read_text(encoding="utf-8")
            else:
                source = await api.workflow_text(workflow_path, workflow_sha)
            definitions = await workflow_definitions(
                source, workflow_path, workflow_sha, api,
                sources, referenced_shas[current_id], current.get("referenced_workflows")
            )
            return await jobs_task, definitions
        except (TraceError, OSError) as error:
            if not jobs_task.done():
                jobs_task.cancel()
            await asyncio.gather(jobs_task, return_exceptions=True)
            raise TraceError(f"run {current_id}: {error}") from error

    loaded = await asyncio.gather(*(load_run(current) for current in runs))
    jobs_by_run = {current["id"]: jobs for current, (jobs, _) in zip(runs, loaded)}
    definitions_by_run = {
        current["id"]: definitions
        for current, (_, definitions) in zip(runs, loaded)
    }
    root_name = None
    if len(args.sources) > 1:
        root_name = f"Source {source_index}: GitHub Actions: {repository} (run {run_id})"
    warnings = append_trace(writer, repository, anchor, runs, jobs_by_run,
                            definitions_by_run, root_name=root_name,
                            root_rank=source_index)
    return len(runs), sum(map(len, jobs_by_run.values())), source_warnings + warnings


async def run(args: argparse.Namespace) -> int:
    if args.output.suffix not in {".pftrace", ".perfetto-trace"}:
        raise TraceError("--output must end in .pftrace or .perfetto-trace")
    if args.event_window_seconds < 0:
        raise TraceError("--event-window-seconds must be nonnegative")
    if args.max_concurrency < 1:
        raise TraceError("--max-concurrency must be positive")
    if args.workflow_file and len(args.sources) > 1:
        raise TraceError("--workflow-file requires a single source")
    if args.workflow_sha and not re.fullmatch(r"[0-9a-fA-F]{40}", args.workflow_sha):
        raise TraceError("--workflow-sha must be a 40-character commit SHA")
    writer = TraceWriter()
    workflow_count = 0
    job_count = 0
    warnings: list[str] = []
    for index, source in enumerate(args.sources, start=1):
        try:
            workflows, jobs, source_warnings = await load_source(
                source, args, writer, index
            )
        except (TraceError, OSError) as error:
            raise TraceError(f"source {index} ({source}): {error}") from error
        workflow_count += workflows
        job_count += jobs
        prefix = f"source {index}: " if len(args.sources) > 1 else ""
        warnings.extend(prefix + warning for warning in source_warnings)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(writer.serialize())
    source_count = f"{len(args.sources)} sources, " if len(args.sources) > 1 else ""
    print(f"Wrote {args.output} ({source_count}{workflow_count} workflows, "
          f"{job_count} jobs)")
    for warning in warnings:
        print(f"warning: {warning}", file=sys.stderr)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sources", nargs="+",
                        help="Actions run URLs, run IDs, or webhook JSON files")
    parser.add_argument("--repo", help="OWNER/REPO (if absent from the source)")
    parser.add_argument("--output", type=Path, required=True,
                        help="Perfetto protobuf trace path (.pftrace)")
    parser.add_argument("--single-run", action="store_true", help="trace only the anchor run")
    parser.add_argument("--event-window-seconds", type=int, default=120,
                        help="maximum creation-time distance from the anchor (default: 120)")
    parser.add_argument("--max-concurrency", type=int, default=50,
                        help="maximum simultaneous GitHub API requests (default: 50)")
    parser.add_argument("--workflow-file", type=Path,
                        help="local YAML override for the anchor run's workflow")
    parser.add_argument("--workflow-sha",
                        help="exact executed workflow commit when GitHub omits it")
    args = parser.parse_args()
    try:
        return asyncio.run(run(args))
    except (TraceError, OSError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
