# Performix MCP Usage

## Terminology & Core Concepts

Arm Performix is a performance analysis toolkit for profiling an application and turning captured performance data into actionable insights. Prefer accessing Performix via the MCP server wherever the relevant functionality is exposed via MCP.

| Term | Definition |
| --- | --- |
| Recipe | A performance analysis pathway that defines tools, parameters, execution stages and data processing. A recipe is selected by you or the user and is run against a target and workload to produce a run. |
| Target | The system on which the workload to be profiled is already running, or will be run. A target may be localhost or remote, is usually saved with a friendly name, and must be specified when running a recipe via MCP. |
| Run | The recorded result of executing a recipe against a target and workload. A run has a run ID and stores the recipe metadata, captured performance data, and artifacts needed for data rendering or producing AI-generated insights. Performix persists an archive of previous runs. |


## Capabilities

Through MCP, you can:

1. Manage the targets available to Performix.
2. Select recipes and record workloads.
3. Inspect, import and export runs, and generate Dynamic Insights for supported runs. Runs can originate from MCP, CLI or GUI.
4. Follow performance investigation pathways to diagnose performance issues and validate optimizations.

Unless the user requests a new recording, check whether a suitable existing run can answer their question.

## Task Guides

Read each relevant guide before performing the task. More than one may apply; follow their links as needed.

Before reading a guide, discover the available MCP resources. Use the server identifier returned by discovery; do not infer it from a tool namespace.

| Guide | Read when |
| --- | --- |
| [Recipe selection and recording](apx://instructions/recipe-selection) | Choosing an initial or follow-up recipe, selecting a target, attaching to a process or launching a workload, and choosing collection settings. |
| [Analysing runs](apx://instructions/analysing-runs) | Discovering, importing or exporting runs, generating Dynamic Insights or applying filters. |
| [Performance investigation](apx://instructions/performance-investigation) | Guidance for multi-step performance investigations or validating an optimization. Not required reading for single step operations, such as listing runs or analyzing data in a run. |
