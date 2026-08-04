<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Performix MCP Usage

## Terminology & Core Concepts

Arm Performix is a performance analysis toolkit for profiling an application and turning captured performance data into actionable insights. Through MCP, you can manage targets, run recipes against workloads, inspect existing runs, and generate Dynamic Insights for supported runs without requiring the user to switch to the CLI or GUI. Prefer accessing Performix via the MCP server wherever the relevant functionality is exposed via MCP.

| Term | Definition |
| --- | --- |
| Recipe | A performance analysis pathway that defines tools, parameters, execution stages and data processing. A recipe is selected by you or the user and is run against a target and workload to produce a run. |
| Target | The system on which the workload to be profiled is already running, or will be run. A target may be localhost or remote, is usually saved with a friendly name, and must be specified when running a recipe via MCP. |
| Run | The recorded result of executing a recipe against a target and workload. A run has a run ID and stores the recipe metadata, captured performance data, and artifacts needed for data rendering or producing AI-generated insights. Performix persists an archive of previous runs. |


## Overall Goals & Capabilities

This MCP server exposes Performix functionality, with the following overall goals:
1. Managing the set of targets available to Performix
2. Enabling users to generate new Performix runs against their application directly via the MCP — see the [Recipe Run Playbook](#recipe-run-playbook) section below.
3. Providing users with Dynamic Insights into their application's performance, based on supported Performix runs which may have originated from the MCP, CLI or GUI.


## Recipe Run Playbook

### Existing Runs
Do not assume that you always need to do a new live recipe run, as running a recipe may take several minutes, depending on the user's workload and the chosen settings (e.g. timeout).
Take into account the following:
- Whether the user has explicitly requested a new run
- If necessary, you can use the `list_runs` tool to see pre-existing runs that are already available on the user's system


### Choosing a Recipe
Choose the recipe that best matches the user's profiling goal. Default to `code_hotspots` only for general CPU profiling, or when the user requests Dynamic Insights without specifying a more suitable type of measurement. Do not prefer `code_hotspots` solely because it supports Dynamic Insights when another recipe better matches the user's request.

Use the `list_recipes` tool to check which recipes are currently available. Recipe availability is controlled by the engine recipe catalogue and configuration, so a recipe that is disabled or failed to load will not be runnable via MCP.

After choosing a recipe, use the `recipe_info` tool before `run_recipe` to inspect its parameters, status, MCP guidance and target support. If you already know the target, pass it to `recipe_info` so target compatibility and target-specific parameter choices can be validated. Follow any returned `mcp_guidance` when choosing parameters and a timeout.

The following table provides guidance for common recipes. It is not an exhaustive catalogue; use `list_recipes` to discover the recipes that are currently available.

| Recipe | Usage guidance |
| --- | --- |
| `system_utilization` | Use this as the default for system-level profiling when the user has not identified a more specific measurement goal. It shows how CPU, memory, disk and network resources are used over time, helping identify saturated resources and correlate workload behaviour with broader system activity. |
| `code_hotspots` | Use this as the default for general CPU profiling when the user has not identified a more specific measurement goal. It is the fastest way to answer "what code is spending CPU time?" and is currently the only recipe that supports Dynamic Insights. |
| `cpu_microarchitecture` | Use this when the user wants to understand microarchitectural bottlenecks. It is often a useful follow-up when hot code is underperforming. |
| `memory_access` | Use this when the workload looks memory-bound or when code hotspots suggest cache or latency issues. |
| `instruction_mix` | Use this when you need a breakdown of instruction categories, compiler output, or ISA usage. |
| `asct` | Use this for Arm system characterization scenarios rather than as a first-pass workload profiling recipe. |
| `cache_sharing` | Use this when you need to understand cache line sharing, cache-to-cache transfers, or false-sharing style effects. |
| `cmn_analysis` | Use this for CMN mesh and interconnect analysis rather than as a first-pass workload profiling recipe. |
| `syscall_trace_summary` | Use this when syscall tracing data is needed to summarize operating system call behaviour during a workload run. |


### Managing Targets
The `run_recipe` MCP tool requires you to specify which `target` to run against — it does not assume fallback to the default.
Use the `list_targets` tool first to discover the available targets, including which one is the default, then pass the chosen target's name explicitly. If several targets exist and the user has not indicated a preference, confirm which one to use before running. If the user has never added a target to Performix before, the pre-configured localhost target will be the default.
New targets can be added directly via MCP using the `add_target` tool, or alternatively the user can add MCP-visible targets via the CLI or GUI. The MCP `add_target` tool enforces strict host key checking, so the target's SSH host key must already be present in the user's known_hosts file before connecting to the target.


### Workloads & Running a Recipe
You can run a recipe live using the `run_recipe` tool, which generates a new run and returns the run's ID among other relevant details.
Unless `recipe_info` returns different MCP guidance, omit the timeout for initial profiling runs to use the MCP default of 10 seconds. Use a longer timeout when the user or the recipe guidance requires it. Set timeout to 0 only when the user explicitly wants no collection timeout.


### Generating Insights
Dynamic Insights are available only for successful runs produced by a supported recipe, currently `code_hotspots`. This limitation applies to Dynamic Insights, not to `run_recipe`; continue to use other recipes when they better match the user's profiling goal.

Use `list_runs` to find a suitable existing successful run when the user has not supplied a run ID. Call `generate_ai_insights` with that run ID. If any returned payload is incomplete, use its bundle ID, payload name and `next_offset` with `read_ai_insights_payload_details`, repeating as needed until the relevant evidence is complete.

The `run_query` tool provides advanced SELECT access to rendered run data. Use it when recipe-specific guidance requests direct querying, or when the user explicitly asks to query a run.
