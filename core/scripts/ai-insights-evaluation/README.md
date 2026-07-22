<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# AI Insights Evaluation

This directory contains the AI Insights evaluation harness. It uses
pre-recorded Performix runs as fixed input, invokes AI Insights through
the configured mode, and judges the generated response against private
rubrics.

## Python Environment

Prepare the core checkout, then run the suite through the repository Taskfile
entrypoint from the repository root (stopping any existing daemons first):

```bash
task core:stop
task core:install
task core:test:eval:ai-insights
```

The evaluation task prepares its Python virtual environment, invokes the AI
Insights pytest suite with the CLI at `core/apap-cli/apx`, and generates local
performance reports. The pytest suite defaults to
`$HOME/.cache/performix/ai-insights-evaluation/pre-recorded-runs` as
the local pre-recorded run cache, and downloads missing inputs from
`its.apx-prerecorded-runs/ai-insights-evaluation` using
`ARTIFACTORY_API_TOKEN`. The default modes are
`rest,hackathon_mcp,performix_mcp`.
The Hackathon MCP server checkout must be supplied with
`AI_INSIGHTS_HACKATHON_MCP_SERVER_DIR` or
`--ai-hackathon-mcp-server-dir` when `hackathon_mcp` is selected.
Production Performix MCP uses the built `apx` binary as the MCP server
via `apx mcp start`.
Override the cache with `--ai-prerecorded-run-cache` or
`AI_INSIGHTS_PRERECORDED_RUN_CACHE` if needed. Pass pytest arguments
after `--`, for example:

```bash
task core:test:eval:ai-insights -- -k test_case_03 --log-cli-level=INFO
```

Use `--ai-act` to select an act from `ai_insights_evaluation.json`.
The default is `act1`:

```bash
task core:test:eval:ai-insights -- --ai-act act1
```

Act 2 contains the larger stable local-source set beyond the Act 1
smoke cases. Act 3 contains the remaining unstable, external, expensive,
or mode-limited prototype cases for manual investigation. They can be
run separately or combined:

```bash
task core:test:eval:ai-insights -- --ai-act act2
task core:test:eval:ai-insights -- --ai-act act1,act2
task core:test:eval:ai-insights -- --ai-act act3
task core:test:eval:ai-insights -- --ai-act act1,act2,act3
```

The Act 2 set contains stable recurring signal. Act 3 contains tests
that are useful for comparison but may have known failures, unsupported
mode coverage, or higher runtime/cost.

Use `--ai-modes` to select invocation modes. The default runs REST as
the direct Responses API baseline, Hackathon MCP as the MCP control
path, and production Performix MCP as the product path:

```bash
task core:test:eval:ai-insights -- --ai-modes rest,hackathon_mcp,performix_mcp
```

To run only the production Performix MCP path:

```bash
task core:test:eval:ai-insights -- --ai-modes performix_mcp
```

Requested modes are run for every testcase selected by `--ai-act`.
Known mode gaps should be represented with manifest expected-failure
metadata rather than by hiding that mode from collection. Expected
failures may be declared for a whole testcase:

```json
{
  "id": "test_case_14",
  "expected_failures": {
    "expected_failure": true,
    "reason": "Known profiling-context limitation."
  }
}
```

They may also be limited to a specific mode:

```json
{
  "id": "test_case_01",
  "expected_failures": {
    "modes": {
      "rest": {
        "expected_failure": true,
        "reason": "REST path does not expose this evidence yet."
      }
    }
  }
}
```

When an expected failure produces a judged model failure, pytest reports
it as `xfail`. Harness, transport, and judge errors still fail the test.
If an expected failure unexpectedly passes, the AI Insights summary
records `xpass`.

The AI Insights evaluation virtual environment uses the dedicated
`core/scripts/ai-insights-evaluation/requirements.txt` file. It is
separate from the broader Robot Framework tooling environment.
REST mode invokes the OpenAI-compatible Responses API from Python using
the suite virtual environment.

If you need to set up the environment without running the suite:

```bash
task core:deps:ai-insights-eval
```

The task installs dependencies into
`core/scripts/ai-insights-evaluation/.venv`. Remove that directory first
if you need to rebuild the environment with a different Python
interpreter or refreshed dependencies.

## Pre-recorded Run Inputs

The suite defaults `--ai-prerecorded-run-cache` to
`$HOME/.cache/performix/ai-insights-evaluation/pre-recorded-runs`. If
you use a different directory, it must contain one subdirectory per
testcase. The cache can also be set with
`AI_INSIGHTS_PRERECORDED_RUN_CACHE`; the older
`AI_INSIGHTS_RUN_ARTIFACT_BASE` name remains supported for existing
scripts. Each testcase directory must contain:

- `latest.zip`: the exported Performix run archive.
- `test_src.zip`: source files fetched from sampled source IDs in the
  run through `load_source_content`.
- `metadata.json`: provenance for the pre-recorded run input.

Per-test recipe parameters needed when creating `latest.zip`, such as
managed-runtime stack collection flags, are defined in
`ai_insights_evaluation.json`. `prerecord-run.py` applies those
parameters automatically for the selected testcase.

Optional MCP performance thresholds are also defined per test. A testcase must
specify all three thresholds in `ai_insights_evaluation.json` to enable its
performance assessment:

```json
{
  "id": "test_case_01",
  "performance_thresholds": {
    "duration_seconds": 90,
    "input_tokens": 1500,
    "output_tokens": 1200
  }
}
```

Each manifest entry must define a non-empty `summary` field. Pytest
uses this only when generating readable test item names; run artifact
paths, imported run cache keys, result directories, and model prompts
continue to use the opaque `test_case_XX` id so the summary is not
exposed to the model under test. Summaries should use the form
`<language> <description>`, for example `Cpp missing crc32c
specialization`.

The evaluation suite imports `latest.zip`, extracts `test_src.zip`
under the results directory, and updates the imported run to use that
extracted source tree. This avoids depending on source paths from the
machine that runs pytest.

Pytest downloads missing `latest.zip`, `test_src.zip`, and metadata
files for the selected testcases from
`its.apx-prerecorded-runs/ai-insights-evaluation` before checking the
local input directory. Override that Artifactory path with
`--ai-artifactory-run-base` or `AI_INSIGHTS_ARTIFACTORY_RUN_BASE` if
needed. The download uses the same `ARTIFACTORY_API_TOKEN` environment
variable as the other local Performix tooling. This keeps local and CI
runs on the same input-resolution path.

## Authentication

Set `OPENAI_API_KEY` before running the suite. REST mode uses it for the
direct Responses API call, and the judge uses it to score every
response. For MCP modes, the harness creates a clean per-attempt
`codex_home/auth.json` in API-key mode using that environment variable,
and deliberately does not read or copy `~/.codex/auth.json`.

Raw result directories contain the generated `codex_home`, including
the per-attempt auth file. Remove that directory before sharing raw
artefacts outside the local test environment.

## Running With Live Progress Logs

Pytest separates captured log level from live terminal logging.
`--log-level=INFO` controls captured logs, but does not enable live log
output during a passing test. To see progress while the evaluation is
running, use `--log-cli-level=INFO`:

```bash
task core:test:eval:ai-insights -- -k test_case_03 --verbose --log-cli-level=INFO
```

The equivalent explicit form is:

```bash
task core:test:eval:ai-insights -- -k test_case_03 --verbose -o log_cli=true --log-level=INFO
```

## Test Metrics And Artefacts

The harness uses pytest's `record_property` mechanism for concise
per-test metrics. The normal pytest terminal output includes an
`AI Insights evaluation` summary section derived from those recorded
properties. It groups results by testcase and mode, with each mode cell
showing the pytest outcome and mode invocation runtime. For REST mode,
this runtime covers summary payload generation and the REST LLM request.
For MCP modes, it covers the `codex exec` process.

Pytest renders the summary through Rich for the terminal. The GitHub
Actions workflow appends a GitHub-flavoured Markdown summary using an
HTML table so related mode columns can be grouped under one heading. For
example, the GitHub summary groups `Result`, `Runtime`, and `Details`
under `Hackathon MCP` and `Performix MCP`, with result cells rendered as
`✅ **PASS** (high)` or `❌ **FAIL** (high)`.

For `performix_mcp` attempts, the summary also reports the performance
quality checks used by benchmark reporting. The checks compare runtime,
input tokens, and output tokens against the testcase's `performance_thresholds`
in `ai_insights_evaluation.json`. A POOR or INDETERMINABLE result fails the
pytest testcase. Testcases without a complete threshold set skip performance
assessment.

Use `--ai-attempts N` or `AI_INSIGHTS_ATTEMPTS=N` to run multiple
attempts. Attempts are pytest parameters, so each attempt is collected,
reported, and recorded as a separate pytest item.

REST mode sends the configured `--ai-reasoning-effort` value to the
OpenAI Responses API. The per-test summary reports the value sent as
`rest_reasoning=<value>`. If `--ai-reasoning-effort` is omitted, the
manifest model config supplies the value.

Pytest records the thresholds and resulting performance quality alongside the
observed metrics in JUnit XML. The task uses `ai_insights_performance_report.py`
to generate the same benchmark-reporting and dashboard data formats used in CI.
Reports are generated even when pytest fails, provided pytest produced JUnit XML.
The task preserves pytest's failure status after generating the reports.

Local reporting outputs are written to:

- `results/reporting/ai-insights-evaluation.xml`
- `results/reporting/payload/metadata.json`
- `results/reporting/payload/reports/ai_insights_performance.json`
- `results/reporting/ai-insights-performance-dashboard.json`

The reporting directory is replaced at the start of each invocation so stale
JUnit data cannot be mistaken for the latest run.

Recorded properties include the testcase id, mode, attempt number,
attempt count, pass rate, imported run id, archive SHA256, agent
duration, token counts, MCP call count, scores, judge labels, and paths
to the attempt artefacts such as `llm_response.md`, `score.md`,
`score.json`, and `invoke_metadata.json`. For performance-evaluated attempts
(i.e. in performix_mcp mode), they also include the performance thresholds
and GOOD/POOR/INDETERMINABLE classification for runtime, input tokens, and
output tokens. Suite-level properties record the model, reasoning effort,
judge model, manifest path, and results directory once per pytest run.
`record_property` and `record_testsuite_property` are pytest's standard metadata
mechanisms, but JUnit XML properties require `junit_family=legacy` or
`junit_family=xunit1`.

Failed evaluations use pytest assertion messages to report the failed
threshold, observed pass count and pass rate, judge label, material gaps,
verdict rationale, and the `score.md` artefact path for each attempt.

Detailed debugging and provenance data remains in the JSON artefacts
under `results/`. The pytest properties are a reporting index over the
same in-memory attempt results, not a replacement for:

- `run_meta.json`
- `invoke_metadata.json`
- `score.json`
- `aggregate.json`

REST mode writes the pathfinding-compatible request and response
artefacts alongside the shared files:

- `pathfinding_run/modes/rest/input.json`
- `pathfinding_run/modes/rest/manifest.json`
- `openai_request.json`
- `openai_request_meta.json`
- `openai_response_raw.json`
- `openai_response_headers.txt`
- `openai_http_status.txt`

Production Performix MCP mode writes the shared MCP artefacts:

- `codex_prompt.txt`
- `codex_exec.jsonl`
- `codex_last_message.txt`
- `codex_home/config.toml`
- `invoke_metadata.json`

It validates that Codex completed the production `generate_ai_insights`
tool call. If the generated evidence bundle is paginated, Codex may also
call `read_ai_insights_payload_details`; those calls are counted in the
MCP call metrics.
