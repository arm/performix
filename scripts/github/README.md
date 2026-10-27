<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# GitHub Actions Perfetto trace

`actions_perfetto_trace.py` turns one or more GitHub Actions run URLs into a native protobuf trace that can be opened with [Perfetto](https://ui.perfetto.dev/). Set `GITHUB_TOKEN` (or `GH_TOKEN`) to a token with Actions, contents, and pull-request read access, then install the Python dependencies in a virtual environment. An existing `gh` login can supply the token with `export GITHUB_TOKEN="$(gh auth token)"`.

```sh
python -m pip install -r scripts/github/requirements.txt
python scripts/github/actions_perfetto_trace.py \
  https://github.com/Arm-Debug/performix/actions/runs/37640407122 \
  --output actions-trace.pftrace
```

To generate a trace without setting up Python locally, run **Generate Actions Perfetto Trace** from the repository's Actions tab. Enter one or more space-separated Performix run URLs. The workflow uploads one trace artifact and links to it from its run summary. The equivalent CLI invocation is:

```sh
gh workflow run actions-perfetto-trace.yml \
  -f run_urls='https://github.com/Arm-Debug/performix/actions/runs/37640407122 https://github.com/Arm-Debug/performix/actions/runs/37779115331'
```

Each run URL anchors a trigger. By default, the tool includes workflow runs with the same event, commit SHA, and branch that were created within 120 seconds of the anchor. Pull-request runs must also belong to the same PR. GitHub may omit the PR number from older run records, so the tool checks same-commit local reusable-workflow refs and looks up PRs associated with the commit when needed. Runs whose PR identity remains ambiguous are excluded with a warning. Use `--single-run` to inspect only the anchor workflow for each source, or `--event-window-seconds` to change the grouping window. A numeric run ID can be supplied with `--repo OWNER/REPO`.

Pass several sources to compare them in one trace:

```sh
python scripts/github/actions_perfetto_trace.py \
  https://github.com/Arm-Debug/performix/actions/runs/37640407122 \
  https://github.com/Arm-Debug/performix/actions/runs/37779115331 \
  --output comparison.pftrace
```

Each source has a separate top-level track, including its workflows, jobs, and `[meta] Observed critical paths` group. Sources are aligned at their own first workflow creation time so their durations can be compared; each top-level track's `originUtc` description retains its absolute start time.

The tool fetches independent job lists and workflow files concurrently, with at most 50 GitHub API requests in flight by default. Use `--max-concurrency N` to adjust this limit. GitHub's [documented secondary limit](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits) permits no more than 100 concurrent requests across REST and GraphQL; other rate limits may still apply.

A saved GitHub `pull_request` or `merge_group` webhook payload can be supplied in place of the run URL:

```sh
python scripts/github/actions_perfetto_trace.py event.json \
  --output actions-trace.pftrace
```

The payload needs `repository.full_name` (or `--repo`) and a head SHA. The tool locates an Actions run for that event and groups the other runs from the same trigger. For a pull request, GitHub may run workflows on a synthetic merge commit, so the tool falls back to the PR branch and event time when its SHA does not find a run. A run URL is more precise when several events occur on the same branch close together.

For the unit-test workflow alone:

```sh
python scripts/github/actions_perfetto_trace.py \
  https://github.com/Arm-Debug/performix/actions/runs/37640407122 \
  --single-run --output unit-test-trace.pftrace
```

The tool fetches each workflow file at the revision executed by its run. For pull-request runs, `head_sha` may identify the PR branch while GitHub executed the workflow from a merge commit. The converter uses immutable SHAs recorded for local reusable workflows to identify that merge commit, including across workflows from the same trigger. If no exact revision can be established, it still displays measured job and step spans, but omits matrix and dependency data and the derived critical paths; the trace marks this as unavailable. Supply `--workflow-sha SHA` when the executed commit is known from another source. With a single source, `--workflow-file PATH` overrides the anchor workflow when its source is unavailable. If that file calls a local reusable workflow and GitHub does not report the executed commit, `--workflow-sha` is required to load the called file. Jobs in local and external reusable workflows are parsed recursively. External references use the executed SHA recorded by GitHub, or an explicitly pinned commit SHA; mutable refs are never resolved against their current contents. Full dependency information requires contents read access to each referenced repository. If an external workflow returns HTTP 401, 403 or 404, its job and step names, timings and known caller dependencies remain visible. Those spans identify the unavailable definition and explain that its internal dependencies are unknown; critical paths are marked unavailable. Missing access is not assumed to mean the workflow is private. Unresolved external revisions, invalid workflow definitions and other API failures remain errors. Internal `needs` edges remain within the same reusable-workflow matrix invocation; caller prerequisites and downstream jobs retain their matrix barriers. The trace records observed matrix job instances and `needs` edges; runtime expressions are not evaluated to invent jobs that did not appear in the Actions API.

The root track contains each workflow as a child. A workflow contains its jobs, with each matrix represented by a [collapsible parent track](https://perfetto.dev/docs/getting-started/converting#python-example-track-hierarchies) and one child track per observed leg. Small workflow and matrix groups, and the root track, contain no slices, so Perfetto shows [thin summaries of child slice activity](https://github.com/google/perfetto/blob/main/ui/src/plugins/dev.perfetto.TrackEvent/index.ts) when they are collapsed. Perfetto currently samples at most eight child slice tracks in each summary. Larger workflow and matrix groups instead have an elapsed span covering their child timings, including jobs carried forward from earlier attempts. This interval includes any waiting between attempts and does not imply continuous execution; expand the group to inspect individual jobs. Trigger, workflow, and matrix metadata is stored in the parent track descriptions. Each job track shows `Queued` from `created_at` to `started_at`, followed by the job execution slice from `started_at` to `completed_at`. Each slice has its corresponding reported GitHub timestamps as debug annotations. Sequential steps are nested within the execution slice. When several executed steps share second-resolution timestamps, the converter divides up to one second of available time among them in their reported order. These inferred boundaries are marked `timingEstimated`, with a `timingNote` explaining the estimate and the original timestamps retained in the step details. Identical reported start and completion seconds establish only that the actual duration was under one second. Skipped steps consume no estimated time and are listed in the job slice's `skippedSteps` details. If the job has no room for a step span, the step uses a separate child track and the reason is printed as a warning. Skipped jobs with reversed timestamps appear as instant markers.

If GitHub creates the first job after the workflow run, a `Before first job` track shows that interval as `Waiting for first job record`. Its annotation gives both reported timestamps and states that the Actions API does not identify the cause. The workflow track remains empty so its collapsed view still summarises the child tracks. For reruns, the interval starts at the current attempt's `run_started_at` when available.

On a partial rerun, GitHub may copy an earlier successful job into the new attempt with its original start, completion, and steps but a new `created_at`. The converter recognises a copied job when its completion precedes its creation in an attempt after the first. It shows the job at its original execution time, omits a false queue slice, and marks its execution slice `carriedForwardFromEarlierAttempt`.

Jobs follow workflow declaration order. Matrix legs are ordered by any matrix values visible in their names; unmatched jobs follow the known definitions in GitHub API order. Job details include the workflow job key, matrix values, runner, URL, conclusion, `needs`, and `requiredBy`.

Each numbered step slice has a `url` annotation linking to the first line of that step's GitHub Actions log (`#step:<number>:1`). The same link appears on step slices in the derived critical-path view. GitHub supplies the step number in the job-list response, so this needs no additional API requests.

Select a job slice to see its outgoing dependency arrows, or its `Queued` slice to see incoming arrows when it waited to start; Perfetto [shows flow arrows when an endpoint is selected](https://perfetto.dev/docs/instrumentation/track-events#flows). A flow connects the prerequisite job's completion to the dependent job's creation.

The **[meta] Observed critical paths** group has one track per workflow. These are derived views of the job data, distinguished from the recorded job tracks by the `[meta]` label. Each track shows the latest finishing job, then follows the `needs` predecessor that finished last before that job started, back to an entry job. Its job slices contain the reported or estimated step spans; the gaps between jobs are labelled **Waiting for job start**. This is the path that gated completion in the recorded run. It does not predict which branch would become critical after a change, and GitHub does not provide timing within a step. The root track description identifies the latest finishing workflow, and each workflow track description records its observed critical path.

Dependency arrows are drawn only when both jobs have usable timing intervals and the prerequisite completed before the dependent job was created. Each workflow track description reports its total `dependencyEdges` and `renderedDependencyFlows`; the complete observed `needs` relationships remain in job details. Cross-workflow jobs share a trigger but have no `needs` edges between them.
