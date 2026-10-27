<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# AI Insights Evaluation

This harness evaluates two suites: [Dynamic Insights](#dynamic-insights) analyzes
pre-recorded Performix runs; [Pathways](#pathways) investigates a live workload,
changes its source, and verifies an improvement. A separate judge LLM assesses
the results against private [rubrics](rubrics/).

Start with [shared setup](#shared-setup), then follow the section for your suite.
[Results](#results) explains reports and saved evidence; [reference](#reference)
covers configuration and input preparation.

## Shared setup

Use Linux, macOS, or WSL2. Native Windows, including MSYS2 and Cygwin, is
unsupported because agent isolation has not been sufficiently validated there.
The harness does not prevent execution on Windows, but a passing test does not
imply adequate host-side sandboxing and therefore the results may be invalid.

Before running either suite:

- Complete the [core development setup](../../../DEVELOPMENT.md#core-workflow).
- Install the shared Codex CLI with `mise -E mcp-testing install aqua:openai/codex`.
  The version is pinned in [the Mise configuration](../../../.mise/config.mcp-testing.toml).
- Set `OPENAI_API_KEY` to a token for the Arm OpenAI proxy. The harness defaults
  to that proxy; use `OPENAI_API_BASE` to change the endpoint.
- Set `ARTIFACTORY_API_TOKEN` to download any missing pre-recorded inputs.

From the repository root, prepare Performix:

```bash
task core:stop
task core:install
```

The evaluation task installs its Python dependencies, downloads missing inputs,
runs the selected tests with the `mcp-testing` Codex environment, and generates
reports. See [Python dependencies](#python-dependencies) for environment-only setup.

For MCP modes, the harness supplies the API key to Codex as both `OPENAI_API_KEY`
and `OPENAI_PROXY_TOKEN`. Agent shell commands and MCP servers do not receive
either variable. The harness does not read or copy `~/.codex/auth.json`.
Codex agents are sandboxed on the host to block access to private rubrics and
other attempts' results.

## Dynamic Insights

These tests use pre-recorded runs and do not require a live target. They cover
Performix recipes with C++, Java, C#, and Python workloads, including known
performance problems and controls where the agent should avoid claiming a problem.

### Insights execution

1. Download missing run inputs, import the selected run, and associate source files where required.
2. Generate insights using the selected MCP mode.
3. Judge the response against the case's rubric using `judge_ai_insights.md`, then save the verdict and evidence.

Production Performix MCP mode validates that Codex completed the
`generate_ai_insights` tool call. It may also use `read_ai_insights_payload_details`
for paginated evidence. Failed auxiliary MCP calls appear as summary warnings,
but do not override the judge's assessment of the final response.

### Run Insights locally

After [shared setup](#shared-setup), run from the repository root:

```bash
task core:test:eval:ai-insights -- \
  --ai-modes performix_mcp -k test_case_03 --ai-attempts 1 --log-cli-level=INFO
```

Select groups with `--ai-act`; combine them with commas, for example
`--ai-act act1,act2`. Use `--ai-attempts N` or `AI_INSIGHTS_ATTEMPTS=N` for
repetitions. Each attempt is a separate pytest item.

| Selection | Coverage |
| --- | --- |
| `act1` (default) | A small smoke suite across supported recipes. |
| `act2` | A larger stable set covering CPU, memory, cache sharing, system behaviour and prompt-injection resistance. |
| `act3` | Experimental, external, expensive or mode-limited cases for manual investigation. |

See [pre-recorded run inputs](#pre-recorded-run-inputs) for caching and overrides.

### Evaluation modes

Use `--ai-modes` to select modes. If omitted, local execution selects
`performix_mcp`:

| Mode | Purpose |
| --- | --- |
| `performix_mcp` | Codex uses the production Performix MCP server. |
| `performix_mcp_run_query` | SQL comparison mode for Code Hotspots; opt-in locally and included in CI defaults. |

`performix_mcp_run_query` supports only Code Hotspots; other recipes are skipped.
An optional per-test `evaluation_modes` list further restricts the selected modes.
To compare curated and SQL Code Hotspots analysis on the same inputs:

```bash
task core:test:eval:ai-insights -- \
  --ai-modes performix_mcp,performix_mcp_run_query \
  --ai-act act1,act2 --ai-recipe code_hotspots
```

### Insights CI

[AI Insights Evaluation](../../../.github/workflows/ai-insights-evaluation.yaml)
runs Acts 1–3 with parallel workers and publishes dashboard results. Select the
upstream APX build, acts, cases, modes and attempts in the workflow inputs.
The default modes are `performix_mcp` and `performix_mcp_run_query`.

## Pathways

Pathways cases belong to `act4` and require `performix_mcp` mode. Each attempt
uses a fresh, disposable ARM64 Linux target on which the agent can edit, build
and profile the workload with passwordless sudo access.

### Pathways execution

1. Prepare fresh host source, agent state and private Performix storage; clone the workload source onto the target.
2. Optionally import a pre-recorded run as starting evidence.
3. Let the agent choose recipes, profile, analyze, edit, rebuild and verify the outcome, subject to the case's agent timeout (600 seconds by default).
4. Judge the final response and session transcript using `judge_pathways.md` and the case's rubric, then retain evidence and clean up temporary host state.

Set `pathways_execution.agent_timeout_seconds` in the manifest to override the
agent timeout. Target cleanup is owned by CI or the local developer, as below.

### Run Pathways locally

After [shared setup](#shared-setup), prepare a disposable target for each attempt.
It needs SSH/SFTP, Python 3, Git, workload build tools, Performix profiling
dependencies, and an SSH account with passwordless sudo.

Verify its SSH host key first. The host needs OpenSSH and Git access to the
[workloads repository](https://github.com/Arm-Debug/performix-workloads).
The agent uses the supplied account and a copy of its key.

Run one case and one attempt at a time, with `-n 0` to disable parallel workers:

```bash
task core:test:eval:ai-insights -- \
  --ai-act act4 --ai-modes performix_mcp -k pathways_01 \
  --ai-attempts 1 -n 0 --log-cli-level=INFO \
  --ai-target-host 192.0.2.10 \
  --ai-target-user ubuntu \
  --ai-target-port 22 \
  --ai-target-private-key-path "$HOME/.ssh/pathways_target"
```

Replace the connection details with your target's values. The harness places
workload source in `/workspace` and refuses to overwrite an existing directory.
Reserve the whole target for the attempt, then discard or clean it before another
attempt. The harness does not provision, reset or destroy local targets.
Use CI for repetitions, or run once per fresh local target.

### Optional baseline run

Pathways cases may start with a pre-recorded run or gather all evidence live.
To define a case without one, omit `run_artifact` and provide an `mcp_prompt`
without `{run_id}`. The agent then starts with an empty Performix store and must
collect its own baseline. When a run is supplied, the agent first assesses its
suitability as a baseline. See [input preparation](#pre-recorded-run-inputs) for
pre-recorded run requirements.

### Pathways CI

[Pathways Evaluation](../../../.github/workflows/pathways-evaluation.yaml)
runs attempts one at a time and provisions a fresh bare-metal target for each.
Choose the upstream APX build, cases and number of attempts.

The workflow terminates the target even when evaluation fails, and requires
confirmed termination before publishing evidence. If the runner disappears,
the existing scheduled sweeper is the cleanup fallback.

## Results

Start with `score.md` for the judge's explanation, `score.json` for its structured
verdict, and `llm_response.md` for the agent's answer. The terminal and GitHub
Actions summaries group results by testcase and mode, showing outcomes, duration
and token use. Failed assertions identify the judge's material gaps or failed
performance thresholds and link to the attempt's score artifact.

An expected model failure is reported as `xfail`, and an unexpected pass as
`xpass`. Harness, transport and judge errors still fail the test. See
[expected failures](#expected-failures) for manifest configuration.

### Reports and metrics

Local reports are under `core/scripts/ai-insights-evaluation/results/reporting/`:

| Output | Contents |
| --- | --- |
| `ai-insights-evaluation.xml` | JUnit test report and recorded metrics. |
| `payload/metadata.json` | Reporting metadata. |
| `payload/reports/ai_insights_performance.json` | Benchmark report data. |
| `ai-insights-performance-dashboard.json` | Dashboard data. |
| `graphs/index.html` | Quality, runtime, token and MCP graphs. |
| `graphs/observations.csv` | Plotted observations. |

Each evaluation run replaces these reports; collection and help leave them intact.
Reporting runs even after test failures,
provided pytest produced JUnit XML. A reporting failure fails an otherwise
successful run without replacing an existing pytest failure status.

MCP invocation runtime covers the `codex exec` process. Where a `performix_mcp` case
has a complete set of [performance thresholds](#performance-thresholds), the
harness checks runtime, input tokens and non-reasoning output tokens. POOR or
INDETERMINABLE performance fails the test; cases without thresholds skip this
assessment. `ai_output_tokens` excludes reasoning tokens, which are recorded
separately as `ai_reasoning_output_tokens`.

JUnit properties record case, mode, attempt, pass rate, imported run ID, archive
hash, agent duration, tokens, MCP call counts and durations, judge scores and
artifact paths, plus applicable thresholds and performance classifications.
MCP calls count as failed for either an MCP `Err` result or `isError` response.
Suite properties record the model, reasoning effort, judge model, manifest and
results directory. These use pytest's `record_property` and
`record_testsuite_property`; JUnit properties require `junit_family=legacy` or
`junit_family=xunit1`.

Regenerate graphs from an existing complete report:

```bash
cd core/scripts/ai-insights-evaluation
.venv/bin/python plot_ai_insights_junit.py \
  results/reporting/ai-insights-evaluation.xml \
  --output-dir results/reporting/graphs
```

The command includes every mode present, writes SVG and PNG figures and a CSV,
and creates an HTML index. Cost figures connect matching modes and show repeated
attempts as ranges. When `performix_mcp` is present, relative-cost panels compare
other modes with the matching Performix MCP attempt.

### Dynamic Insights evidence

Attempt artifacts under `results/` include `run_meta.json`,
`invoke_metadata.json` and `aggregate.json`, alongside the response and verdict.
Pytest properties identify their paths.

Production Performix MCP mode also writes `codex_prompt.txt`, `codex_exec.jsonl`,
`codex_last_message.txt` and local `codex_home/config.toml`.

### Pathways evidence

Evidence is retained under `results/invocations/<invocation-id>/`, with the
location printed at startup. In addition to the response and verdict, it includes
`codex_session.jsonl`, diagnostic output (`codex_exec.jsonl` and
`codex_stderr.txt`), `codex_last_message.txt`, and recorded recipe calls in
`performix_runs.json`. Source changes are described in the agent response and
transcript; the harness does not capture a separate source patch.
Pathways does not retain `run_meta.json`, `invoke_metadata.json` or `aggregate.json`;
summary metadata is recorded in JUnit.

### CI artifacts

Both workflows publish staged evidence to GitHub Actions and Artifactory at
`its.performix-ai-insights/test-artifacts/`. Staging excludes temporary workspaces
and agent state. It selects evidence files; it does not sanitize their contents.

Use `--ai-results-dir` or `AI_INSIGHTS_RESULTS_DIR` to change the local base
results directory. Raw working directories can contain credentials; use the
curated CI artifacts when sharing results.

### Re-judging saved responses

`judge_ai_insights_response.py` evaluates saved evidence without rerunning the
agent. It uses `OPENAI_API_KEY` and the suite's judge operation, defaults the test
ID to the rubric filename, and honours `AI_INSIGHTS_JUDGE_MODEL`. Override these
with `--test-id` or `--judge-model`. It exits non-zero if any response fails.

Replay uses `--judge-reasoning-effort`; evaluation uses
`--ai-judge-reasoning-effort`. Both default to `medium`, honour
`AI_INSIGHTS_JUDGE_REASONING_EFFORT`, and let the command-line option override it.

#### Re-judge Dynamic Insights

Pass the rubric followed by one or more response files:

```bash
cd core/scripts/ai-insights-evaluation
.venv/bin/python judge_ai_insights_response.py \
  rubrics/test_case_28.md \
  results/test_case_28/performix_mcp/attempts/001/llm_response.md
```

#### Re-judge Pathways

Pass one response and its session transcript:

```bash
cd core/scripts/ai-insights-evaluation
attempt="results/invocations/<invocation-id>/pathways_01/performix_mcp/attempts/001"
.venv/bin/python judge_ai_insights_response.py \
  rubrics/pathways_01.md "$attempt/llm_response.md" \
  --pathways-session "$attempt/codex_session.jsonl" \
  --supplied-run-id "<pre-recorded-run-id>"
```

Replace the placeholders with the saved attempt and the original pre-recorded run ID
(from the session prompt). Omit `--supplied-run-id` if no pre-recorded run was supplied.

## Reference

### Test selection and logging

Pass options after `task core:test:eval:ai-insights --`:

| Option | Purpose |
| --- | --- |
| `--ai-act act1,act2` | Select groups; `act4` selects Pathways. |
| `--ai-recipe code_hotspots` | Filter by recipe; excludes live-only Pathways cases without a fixed recipe. |
| `-k test_case_03` | Filter by case name. |
| `--ai-model MODEL --ai-reasoning-effort high` | Choose the agent model and reasoning effort. |
| `--ai-judge-model MODEL` | Choose the separate judge model. |
| `--ai-judge-reasoning-effort medium` | Set judge reasoning (default: `medium`). |
| `--collect-only` | List selected attempts without running them. |
| `--log-cli-level=INFO` | Show live progress, including during passing tests. |

`--log-level=INFO` changes captured logging only. For live progress, use
`--log-cli-level=INFO` as above, or `-o log_cli=true --log-level=INFO`.

### Manifest and prompts

[ai_insights_evaluation.json](ai_insights_evaluation.json) defines test cases,
inputs and case-specific prompts. Shared agent and self-contained judge prompts
are in [prompts/](prompts/); private scoring criteria are in [rubrics/](rubrics/).

Each case needs a non-empty `summary`, used only for readable pytest names.
Artifact paths, cache keys, result directories and model prompts use the opaque
case ID so the summary is not exposed to the model. Use the form
`<language> <description>`, for example `Cpp missing crc32c specialization`.

Set `mcp_prompt` to override the default MCP prompt:

```json
{
  "id": "test_case_01",
  "mcp_prompt": "Investigate why run {run_id} is not vectorising. Use Markdown."
}
```

`{run_id}` is replaced with the imported run ID. The resolved prompt is saved as
`codex_prompt.txt` during execution; see suite-specific evidence above for retention.

### Expected failures

Expected failures describe known analysis failures within a supported mode and
recipe combination. Declare them for a whole case:

```json
{
  "id": "test_case_14",
  "expected_failures": {
    "expected_failure": true,
    "reason": "Known profiling-context limitation."
  }
}
```

Or limit them to a specific mode:

```json
{
  "id": "test_case_01",
  "expected_failures": {
    "modes": {
      "performix_mcp": {
        "expected_failure": true,
        "reason": "Known profiling-context limitation."
      }
    }
  }
}
```

### Performance thresholds

Specify all three thresholds on a case to enable its performance assessment:

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

### Pre-recorded run inputs

The shared pre-record workflow has two catalogs: `ai_insights_evaluation.json`
generates run inputs for the AI Insights evaluation suite, while
`robot_prerecorded_runs.json` generates run inputs for Robot tests and is not
used by the evaluation suite.

The cache defaults to
`$HOME/.cache/performix/ai-insights-evaluation/pre-recorded-runs`. Override it with
`--ai-prerecorded-run-cache` or `AI_INSIGHTS_PRERECORDED_RUN_CACHE`; the older
`AI_INSIGHTS_RUN_ARTIFACT_BASE` name remains supported for existing scripts.
The cache must contain the subdirectories referenced by each case's `run_artifact`.
Multiple cases may use the same pre-recorded run directory.

Pytest downloads missing inputs from `its.apx-prerecorded-runs/ai-insights-evaluation`
using `ARTIFACTORY_API_TOKEN`. Override the remote location with
`--ai-artifactory-run-base` or `AI_INSIGHTS_ARTIFACTORY_RUN_BASE`.
Each pre-recorded run's directory contains:

- `latest.zip`: the exported Performix run archive.
- `metadata.json`: recording provenance, including the recipe checked by the harness.
- `test_src.zip`: required for Code Hotspots, CPU Microarchitecture, Cache Sharing,
  Memory Access and dynamic-enabled Instruction Mix. It contains source fetched
  from sampled source IDs through `load_source_content`.

The manifest recipe selects input handling. The harness extracts source under
the results directory and associates it with the imported run, avoiding dependence
on the recording machine's paths. ASCT, Syscall Trace, System Utilisation and
static-only Instruction Mix import only the run archive.

The manifest also defines recording recipe parameters (such as managed-runtime
stack collection flags) and optional `prerecord` settings. The recording workflow
resolves them before invoking the workload-agnostic `prerecord-run.py` helper.

For JFR cases, `prerecord.enable_jfr_capture` sets `APXD_ENABLE_JFR_CAPTURE`
during recording and is saved in `metadata.json`. The harness rejects pre-recorded runs
with a mismatched setting; JFR table contents still need checking in the run.

Some recipes support `run_modification` to alter an exported archive. ASCT supports
`core_to_core_latency_asymmetry` and `low_peak_bandwidth`, for example:

```json
{
  "id": "test_case_01",
  "recipe": "asct",
  "run_modification": "low_peak_bandwidth"
}
```

The modification produces the final `latest.zip` after export and before external
metadata is written. Unknown modifications and incompatible recipes are rejected
before recording. Modified pre-recorded runs include `run-modification-report.json`,
a record of the changes uploaded with the run but not shown to the evaluation model.

### Python dependencies

The harness uses its own [requirements.txt](requirements.txt), separate from Robot
Framework tooling. To prepare it without running the suite:

```bash
task core:deps:ai-insights-eval
```

Dependencies are installed into `core/scripts/ai-insights-evaluation/.venv`.
Remove that directory first to rebuild with a different Python interpreter or
refreshed dependencies.

### Unit tests

For unit tests of the harness itself, see the
[core unit-test commands](../../../DEVELOPMENT.md#core-workflow).
