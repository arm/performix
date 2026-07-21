<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Pathfinding REST Source

This directory started as a snapshot of the REST-mode implementation
from the pathfinding repo:

```text
https://github.com/Arm-Debug/arm-performix-ai-insights-experiments
```

Local source checkout:

```text
/Users/davrig01/Library/CloudStorage/OneDrive-Arm/Documents/AI_Insights/ai_insights_tests
```

Source commit:

```text
1c95dff4088bc37adf4da3519833fd2734b735a4
```

The retained copied files were clean relative to that source commit when
the snapshot was taken:

- `private/scripts/build_llm_payload.py`
- `private/scripts/build_openai_request.py`
- `private/scripts/extract_call_tree_hot_paths.py`
- `private/scripts/extract_source_hot_windows.py`
- `public/prompts/analysis_prompt.md`

The retained `build_llm_payload.py` and `build_openai_request.py` files
have since been simplified for the Performix pytest harness by removing
unused source-only and command-line wrapper code. The retained
`extract_call_tree_hot_paths.py` and `extract_source_hot_windows.py`
files are copied verbatim from the source commit above.

The initial snapshot also included:

- `dodo.py`
- `private/scripts/common.sh`
- `private/scripts/invoke_openai_isolated.sh`

These files have been removed from this repository because the Performix
pytest harness does not use the pathfinding repo's `doit` task graph,
and REST invocation now happens in `rest_mode.py`.
