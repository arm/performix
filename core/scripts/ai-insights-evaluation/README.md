<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# AI Insights Evaluation

This harness evaluates two suites: [Dynamic Insights](#dynamic-insights) analyzes
pre-recorded Performix runs; [Pathways](#pathways) evaluates performance
investigations, including clarification, optimization and stopping decisions.
A separate judge LLM assesses the results against private [rubrics](rubrics/).

Start with [shared setup](#shared-setup), then follow the section for your suite.
[Results](#results) explains reports and saved evidence; [reference](#reference)
covers configuration and input preparation.

## Test layout

`tests/unit/` contains the unit tests for the harness and run modifications.
`tests/evaluation/` contains the live AI evaluations and their pytest hooks.
`judge_regression/` contains the saved-answer judge evaluations.
The unit-test task selects `tests/unit/`; the evaluation runner selects
`tests/evaluation/`. Neither command needs filename exclusions or markers.
Plain `pytest` from this directory defaults to the unit tests. See the
[development guide](../../../DEVELOPMENT.md#core-workflow) for the unit-test task
and the suite sections below for evaluation commands.

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
`generate_ai_insights` tool call. Failed auxiliary MCP calls appear as summary warnings,
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

All supported recipes, including Code Hotspots, use SQL through `run_query`.
An optional per-test `evaluation_modes` list further restricts the selected modes.

### Insights CI

[AI Insights Evaluation](../../../.github/workflows/ai-insights-evaluation.yaml)
runs Acts 1–3 with parallel workers and publishes dashboard results. Select the
upstream APX build, acts, cases, modes, model configurations and attempts in the workflow inputs.
The default mode is `performix_mcp`.

## Pathways

Pathways cases belong to `act4` and require `performix_mcp` mode. Each attempt
uses a fresh, disposable ARM64 Linux target on which the agent can edit, build
and profile the workload with passwordless sudo access.

### Pathways execution

1. Export the manifest's `pathways_execution.workload_source.directory` (`.` for the whole repository) into a fresh Git baseline without upstream history or remotes, remove the staging clone, and seed `/workspace` on the target. Prepare fresh agent state and private Performix storage.
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
collect its own baseline when the task requires profiling. When a run is
supplied, the agent first assesses its suitability as a baseline. See
[input preparation](#pre-recorded-run-inputs) for pre-recorded run requirements.

### Pathways CI

[Pathways Evaluation](../../../.github/workflows/pathways-evaluation.yaml)
runs attempts one at a time and provisions a fresh bare-metal target for each.
Choose the upstream APX build, cases and number of attempts. `workload_ref`
selects the workloads repository branch, tag or commit (default `main`). The
CI checkout is exposed to the harness through a local branch at that exact
commit. The resolved source commit is stored in temporary attempt metadata and
discarded during cleanup; published Pathways evidence does not retain it.
Locally, `--ai-workload-ref` overrides the manifest's source branch with a branch
or tag.

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
has [performance thresholds](#performance-thresholds) for the model/reasoning
configuration, the harness checks runtime, input tokens and non-reasoning output
tokens. POOR or INDETERMINABLE performance fails the test; missing thresholds skip this
assessment. `ai_output_tokens` excludes reasoning tokens, which are recorded
separately as `ai_reasoning_output_tokens`.

JUnit properties record case, mode, model configuration, attempt, pass rate, imported run ID, archive
hash, agent duration, tokens, MCP call counts and durations, judge scores and
artifact paths, plus applicable thresholds and performance classifications.
MCP calls count as failed for either an MCP `Err` result or `isError` response.
Suite properties record the model configurations, judge model, manifest and
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
| `--ai-model-config MODEL EFFORT` | Repeat for each model/reasoning pair. |
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

Exceeding a wall-clock threshold produces a warning in the console and CI
summaries without failing the test. Provider response times and model behaviour
can vary independently of MCP server performance. Input and output token
threshold breaches, and missing measurements for configured thresholds, still
fail otherwise-passing tests.

Wall-clock assessments remain `POOR` in JUnit and benchmark reports when the
target is exceeded, so performance trends remain visible. These thresholds are
checked after the conversation finishes; they do not impose execution timeouts.

Specify all three thresholds per model/reasoning configuration on a case.
Configurations without a complete threshold set still report usage but skip
performance assessment.

```json
{
  "id": "test_case_01",
  "performance_thresholds": {
    "gpt-5.5-high": {
      "duration_seconds": 90,
      "input_tokens": 1500,
      "output_tokens": 1200
    }
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

For runs that specify it, `prerecord.enable_jfr_capture` sets `APXD_ENABLE_JFR_CAPTURE`
during recording and is saved in `metadata.json`. The harness rejects pre-recorded runs
with a mismatched setting. Separately, `requirements: ["jfr_data"]` makes evaluation
check the complete JFR dataset before AI attempts: recordings, GC, heap, JVM
information, effective flags and initial properties. Configuration is part of
JFR availability, not a separate requirement. The check requires queryable
tables and a parsed recording with matching JVM identity/version, a positive
effective `MaxHeapSize` with origin/type, and initial properties.

JVM configuration coverage extends the rubrics for existing performance issues.
Case 20 checks that cache advice uses recorded heap settings; a larger heap is
one option, not a required recommendation. Case 72 checks that explicit GC leads
to an application fix, with configuration advice only when supported. Case 17 checks
that missing JFR data does not prevent useful application advice or lead to
invented JVM settings. These cases keep their existing recordings and generic
prompts. Run them in production MCP mode (`performix_mcp`) alongside JFR regression
cases 71 and 73:

```bash
task core:test:eval:ai-insights -- --ai-modes performix_mcp \
  --ai-act act1,act2 --ai-attempts 1 \
  -k 'test_case_17 or test_case_20 or test_case_71 or test_case_72 or test_case_73'
```

`generate_ai_insights` and `open_render_session` return a short pointer to the
MCP resource `apx://instructions/jvm-guidance` when the render manifest includes
`jfr_recordings`. It contains the JVM configuration
and heap/GC guidance, which is not included in every Code Hotspots response.
The guide discovers raw configuration relations through their schemas.
The `jvm_flags` renderer exposes `Recording`, `Event Start (epoch ns)`, `Flag`,
`Value`, `Origin` and `Type`; `Value` remains a string to preserve integer
precision. Queries select the latest captured value per recording/name. This
does not change the JVM Internals UI.

Case 74 is a dedicated JVM flag test. Its workload repeatedly calls
`java.util.zip.CRC32` with JVM CRC32 intrinsics disabled; the required primary
recommendation is to enable `UseCRC32Intrinsics` or remove the disabling override.
It uses SQL mode, captured JVM configuration and a new recording; it does not
reuse the scalar CRC32C workload from case 16.

Before accepting a case 74 recording on the Linux Arm64 target:

1. Build the workloads repository's `test_case_74` target. Its launcher supplies
   `-XX:+UnlockDiagnosticVMOptions -XX:-UseCRC32Intrinsics`.
2. Run a control using the same JDK and `test_case_74.jar` with
   `-XX:+UnlockDiagnosticVMOptions -XX:+UseCRC32Intrinsics`. Compare
   `processed_bytes / elapsed_ns` and require identical `checksum` values;
   `completed_passes` can differ because the workload is timed. Check repeated
   runs for a material improvement before using the fixture.
3. Record the disabled case with JFR and Java stacks. In the workload's selected
   recording, verify `jvm_flags` contains `UseCRC32Intrinsics=false` and its
   command-line origin. The shared JFR preflight checks data availability, not
   this case-specific value. Confirm that samples and source/call paths identify
   material CRC32 computation. The native fallback can itself be optimized;
   do not require it to lack all hardware CRC instructions.

Evaluate the new fixture with:

```bash
task core:test:eval:ai-insights -- --ai-modes performix_mcp \
  --ai-act act1 --ai-attempts 1 -k test_case_74
```

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

### Judge regression evaluation

These tests check whether the judge gives the expected `pass` or `fail` result
for a saved answer. They use the test case's scoring rules in `rubrics/`.
They do not run the workload or ask an agent to investigate it again.

Set `OPENAI_API_KEY` as described in [shared setup](#shared-setup), then run
from the repository root:

```sh
task core:test:eval:ai-insights-judge
```

This makes paid API calls. To choose a different judge model or reasoning effort,
pass the existing options after `--`:

```sh
task core:test:eval:ai-insights-judge -- \
  --ai-judge-model gpt-5.6-luna --ai-judge-reasoning-effort medium
```

To add a regression case:

1. Create a directory under `judge_regression/<test_case>/`. Give it a short name
   that describes the behaviour being checked. For example,
   `judge_regression/test_case_72/explicit_gc/`. The test case must have scoring
   rules in `rubrics/<test_case>.md`.
2. Save the reference answer as `response.md`. The judge should pass this answer.
3. Save a deliberately incorrect answer as `negative_control.md` in the same
   directory. The judge should fail this answer. Label any answer written by
   hand in the documentation so readers can distinguish it from a captured one.
4. Run the command above. pytest finds both files automatically. No Python test
   list needs updating. If either file is missing, pytest reports an error
   before making any judge calls. If a verdict differs from the expected result,
   the test fails and displays the judge's explanation.

Each test case can have several regression directories, each containing both
files. There is currently one, `test_case_72/explicit_gc/`. Its `response.md` is
a [saved CI answer](https://github.com/Arm-Debug/performix/actions/runs/39937492898/job/120368964294)
that should pass despite omitting pause durations.

The initial negative control was written by hand and incorrectly diagnoses a
memory leak. It is not another captured regression. These two answers provide
limited coverage; they do not prove that the judge will assess every answer
correctly.

AI Insights Evaluation runs these tests before the agent evaluation, using the
same Python environment, credentials and judge settings. Both evaluations run
even if one fails; either failure fails the job unless the workflow allows
failures. It saves the judge regression results as a separate JUnit test report. Ordinary unit-test commands select `tests/unit/` and do not run these tests
because they require API access. Run them when changing the relevant scoring
rules, judge prompt, judge code or model settings.
