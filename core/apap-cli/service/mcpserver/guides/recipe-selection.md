# Choosing and Running a Recipe

Choose the recipe that best matches the user's profiling goal. Default to `code_hotspots` only for general CPU profiling, or when the user requests Dynamic Insights without specifying a more suitable type of measurement. Do not prefer `code_hotspots` solely because it supports Dynamic Insights when another recipe better matches the user's request.

Use the `list_recipes` tool to check which recipes are currently available. Recipe availability is controlled by the engine recipe catalogue and configuration, so a recipe that is disabled or failed to load will not be runnable via MCP.

After choosing a recipe, use the `recipe_info` tool before `run_recipe` to inspect its parameters, status, MCP guidance and target support. If you already know the target, pass it to `recipe_info` so target compatibility and target-specific parameter choices can be validated. Follow any returned `mcp_guidance` when choosing parameters and a timeout.

The following table provides guidance for common recipes. It is not an exhaustive catalogue; use `list_recipes` to discover the recipes that are currently available.

| Recipe | Usage guidance |
| --- | --- |
| `system_utilization` | Use this as the default for system-level profiling when the user has not identified a more specific measurement goal. It shows how CPU, memory, disk and network resources are used over time, helping identify saturated resources and correlate workload behaviour with broader system activity. |
| `code_hotspots` | Use this as the default for general CPU profiling when the user has not identified a more specific measurement goal. It is the fastest way to answer "what code is spending CPU time?" |
| `cpu_microarchitecture` | Use this when the user wants to understand microarchitectural bottlenecks. It is often a useful follow-up when hot code is underperforming. |
| `memory_access` | Use this when the workload looks memory-bound or when code hotspots suggest cache or latency issues. |
| `instruction_mix` | Use this when you need a breakdown of instruction categories, compiler output, or ISA usage. |
| `asct` | Use this for Arm system characterization scenarios rather than as a first-pass workload profiling recipe. |
| `cache_sharing` | Use this when you need to understand cache line sharing, cache-to-cache transfers, or false-sharing style effects. |
| `cmn_analysis` | Use this for CMN mesh and interconnect analysis rather than as a first-pass workload profiling recipe. |
| `syscall_trace_summary` | Use this when syscall tracing data is needed to summarize operating system call behaviour during a workload run. |

## Managing Targets

The `run_recipe` MCP tool requires you to specify which `target` to run against — it does not assume fallback to the default.
Use the `list_targets` tool first to discover the available targets, including which one is the default, then pass the chosen target's name explicitly. If several targets exist and the user has not indicated a preference, confirm which one to use before running. If the user has never added a target to Performix before, the pre-configured localhost target will be the default.
New targets can be added directly via MCP using the `add_target` tool, or alternatively the user can add MCP-visible targets via the CLI or GUI. For SSH targets, the MCP `add_target` tool enforces strict host key checking, so every SSH host key must already be present in the user's known_hosts file before connecting.

## Workloads & Running a Recipe

You can run a recipe live using the `run_recipe` tool, which generates a new run and returns the run's ID among other relevant details.
To find the PID of a running process on the target, call `search_target_processes` with the target name. Pass that PID to `run_recipe` using `attach_to_pid`.
Unless `recipe_info` returns different MCP guidance, omit the timeout for initial profiling runs to use the MCP default of 10 seconds. Use a longer timeout when the user or the recipe guidance requires it. Set timeout to 0 only when the user explicitly wants no collection timeout.
