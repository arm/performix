# Analysing Runs

## Existing Runs

Do not assume that you always need to do a new live recipe run, as running a recipe may take several minutes, depending on the user's workload and the chosen settings (e.g. timeout).
Take into account the following:
- Whether the user has explicitly requested a new run
- If necessary, you can use the `list_runs` tool to see pre-existing runs that are already available on the user's system

Use `import_run` to import a run archive. Use `export_run` to create a run archive from an existing run.

## Generating Insights

Dynamic Insights are available only for successful runs produced by a supported recipe. Supported recipes are:

- `asct`
- `cache_sharing`
- `code_hotspots`
- `cpu_microarchitecture`
- `instruction_mix`
- `memory_access`
- `syscall_trace_summary`
- `system_utilization`

This limitation applies to Dynamic Insights, not to `run_recipe`; continue to use other recipes when they better match the user's profiling goal.

Use `list_runs` to find a suitable existing successful run when the user has not supplied a run ID. Call `generate_ai_insights` with that run ID. If the user specifies a PID, TID, time range or another render filter, use `recipe_info` to check whether the recipe exposes a matching render parameter and obtain its exact name and type. Pass matching values in `render_parameters`. `generate_ai_insights` accepts render parameters for every supported AI Insights recipe; recipes with no listed parameters use their default render.

The `run_query` tool provides advanced SELECT access to rendered run data. Use it when recipe-specific guidance requests direct querying, or when the user explicitly asks to query a run.
