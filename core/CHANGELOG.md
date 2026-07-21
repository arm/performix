<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

Features
--------

- Add target-side source file support to the source window summarizer. (#2174)
- Make System Utilization timeline line chart y-axis display range recipe-configurable (#2191)
- Update the ASCT recipe and bundled tool integration to ASCT 0.6.0, including the new report-based system information command. (#2232)
- Support SSH password auth via keyboard-interactive method for Performix-managed targets (#2266)
- Add support for pending manifest entries, during background transfers. (#2271)
- Produce and retrieve `sl-analyze` timeline outputs in `neoprof` tool integration when `EnableNeoprofTimeline` is on (#2281)
- Show run working directory and environment in `run info` CLI output (#2290)
- Android target discovery now lists only devices found during discovery, instead of also including saved targets. (#2295)
- Adds a package list and activities list for Android targets in the gRPC API's (#2298)
- Implement target agent `GetOSDescription` API for Android, and enhance unsupported method errors (#2303)
- Change status of Syscall Trace recipe to preview (#2305)
- Update working dir in a dedicated method as part of WorkloadOptionsStage.
  - this replaces the previous behaviour of updating working dir as part of UpdateRunResult, which required us to
  carry around the WorkingDir in StageContext. (#2310)
- Rework AI insights' source-window summarizer payload. (#2322)
- Set run end time at end of phase 2 transfers, rather than end of phase 1 (#2323)
- Rework hot windows algorithm in AI insights' disassembly summarizer. (#2333)
- Add verify-and-launch script for the target agent on Darwin platforms (#2349)
- Sort `recipe list` output in the CLI so repeated invocations show recipes in a stable order. (#2390)


Bugfixes
--------

- Remove duplicate grid in syscall trace recipe (#2304)
- Recover target agent lock acquisition when the target lock directory is deleted. (#2313)
- Disallow multiple pid flags (#2317)
- Disabled CLI release related robot tests (#2332)
- Progress bars for tool deployment are now more reliable (#2351)
- AACR GitHub reviews now use the SXE runtime so incremental reviews avoid reporting changes merged from main. (#2362)
- Show sysacll heatmap values as rates instead of counts. (#2370)


Misc
----

- [BREAKING] Adds more detailed options fields to the recipe single and multi select parameters (#2170)
- Use specific control widget for time range filtering. (#2229)
- Allow early termination of the syscall-trace tool (emit artefacts captured so far back from target) and add a 'Parsing' stage for better UX. (#2246)
- Add Performix MCP mode and summary tables to the AI Insights evaluation harness. (#2291)
- Fail the newsfile workflow when a PR adds a news fragment with the wrong PR number. (#2300)
- Hardened the MCP server tools to classify each failure as an input or internal
  error and to surface the full catalog error detail (code, severity, explanation,
  and advice) in their structured results. Every tool also now publishes a complete
  output schema so agents receive consistent, machine-readable responses. (#2302)
- GUI always builds against latest engine code. Local builds, CI and release workflows no longer download a published CLI package. The CLI is now released alongside the GUI. (#2306)
- Unify tpip generation pipelines under tpip.yml (#2307)
- Bump transfer manager test timeout from 1 to 2s to reduce CI instability (#2309)
- Update the AACR review workflow to use the incremental review runtime. (#2314)
- Send transfer phase 1 completion message only once manifest has been updated. (#2318)
- Bump version of terminate-hanging-targets action (#2319)
- Add fallback AWS target cleanup for Robot workflow and bump action version (#2324)
- Rename `in_progress_phase1_transfer_complete` recipe result to `in_progress_phase1_complete` (#2325)
- Allow Hackathon MCP tool-output truncation to be reported as a warning in AI Insights evaluation summaries. (#2327)
- Run AI Insights act 2 during weekly benchmark reporting without making the result voting while the evaluation remains under stabilisation. (#2328)
- Run AI Insights evaluation tests in parallel in CI. (#2330)
- Add support package diagnostic skill (#2342)
- Update Slack channel used for instance termination alerts (#2343)
- Add MCP prompt injection resilience test to AI insights evaluation (#2346)
- Add robot tests for time range filtering. (#2352)
- Remove redundant skill files. (#2358)
- Tidy Taskfile structure and documentation (#2364)
- Add a manual AACR review file pattern input for scoped retry runs. (#2369)
- Fix discrepancies between local and CI protobuf generation (#2375)
- Bump go toolchain to 1.26.5 (#2384)
