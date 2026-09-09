# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Recipe-specific input requirements for the AI Insights evaluation suite."""

from __future__ import annotations

from typing import Any, Iterable


CODE_HOTSPOTS_RECIPE = "code_hotspots"
CPU_MICROARCHITECTURE_RECIPE = "cpu_microarchitecture"
CACHE_SHARING_RECIPE = "cache_sharing"
MEMORY_ACCESS_RECIPE = "memory_access"
INSTRUCTION_MIX_RECIPE = "instruction_mix"
SYSTEM_UTILIZATION_RECIPE = "system_utilization"
SYSCALL_TRACE_SUMMARY_RECIPE = "syscall_trace_summary"
ASCT_RECIPE = "asct"
SPE_RECIPES = frozenset({CACHE_SHARING_RECIPE, MEMORY_ACCESS_RECIPE})
INSTRUCTION_MIX_SOURCE_MODES = frozenset({"static", "dynamic", "both"})
SUPPORTED_RECIPES = frozenset(
    {
        CODE_HOTSPOTS_RECIPE,
        CPU_MICROARCHITECTURE_RECIPE,
        CACHE_SHARING_RECIPE,
        MEMORY_ACCESS_RECIPE,
        INSTRUCTION_MIX_RECIPE,
        SYSTEM_UTILIZATION_RECIPE,
        SYSCALL_TRACE_SUMMARY_RECIPE,
        ASCT_RECIPE,
    }
)
RECIPE_RESTRICTED_MODES = {
    "rest": frozenset({CODE_HOTSPOTS_RECIPE}),
    "hackathon_mcp": frozenset({CODE_HOTSPOTS_RECIPE}),
}
PERFORMIX_MCP_MODE = "performix_mcp"
PRERECORD_MODES = frozenset({"launch", "attach", "system-wide"})

CODE_HOTSPOTS_SOURCE_FILES_QUERY = """
SELECT
  p.source_file_id,
  COALESCE(sf.target_location, '') AS target_location,
  COALESCE(sf.host_location, '') AS host_location,
  SUM(p.periodic_samples) AS periodic_samples
FROM periodic_samples p
LEFT JOIN source_files sf ON sf.source_file_id = p.source_file_id
WHERE p.source_file_id IS NOT NULL
  AND p.periodic_samples > 0
GROUP BY p.source_file_id, sf.target_location, sf.host_location
ORDER BY periodic_samples DESC, p.source_file_id
"""

INSTRUCTION_MIX_SOURCE_FILES_QUERY = """
SELECT
  s.source_file_id,
  COALESCE(sf.target_location, '') AS target_location,
  COALESCE(sf.host_location, '') AS host_location,
  SUM(CASE
    WHEN m.identifier = 'samples.count.self' THEN d.measurement_value
    ELSE 0
  END) AS self_samples
FROM drilldown AS d
JOIN drilldown_measurements AS m
  ON m.measurement_id = d.measurement_id
JOIN symbols AS s
  ON s.symbol_id = d.symbol_id
LEFT JOIN source_files AS sf
  ON sf.source_file_id = s.source_file_id
WHERE s.source_file_id IS NOT NULL
  AND m.identifier = 'samples.count.self'
GROUP BY s.source_file_id, sf.target_location, sf.host_location
HAVING COALESCE(SUM(CASE
  WHEN m.identifier = 'samples.count.self' THEN d.measurement_value
  ELSE 0
END), 0) > 0
ORDER BY self_samples DESC, s.source_file_id
"""

CACHE_SHARING_SOURCE_FILES_QUERY = """
SELECT
  s.source_file_id,
  COALESCE(sf.target_location, '') AS target_location,
  COALESCE(sf.host_location, '') AS host_location,
  SUM(d.measurement_value) AS cache_sharing_samples
FROM drilldown AS d
JOIN drilldown_measurements AS m
  ON m.measurement_id = d.measurement_id
JOIN symbols AS s
  ON s.symbol_id = d.symbol_id
JOIN source_files AS sf
  ON sf.source_file_id = s.source_file_id
WHERE m.identifier = 'perf.c2c.samples'
GROUP BY s.source_file_id, sf.target_location, sf.host_location
HAVING SUM(d.measurement_value) > 0
ORDER BY cache_sharing_samples DESC, s.source_file_id
"""

MEMORY_ACCESS_SOURCE_FILES_QUERY = """
SELECT
  s.source_file_id,
  COALESCE(sf.target_location, '') AS target_location,
  COALESCE(sf.host_location, '') AS host_location,
  SUM(d.measurement_value) AS memory_access_samples
FROM drilldown AS d
JOIN drilldown_measurements AS m
  ON m.measurement_id = d.measurement_id
JOIN symbols AS s
  ON s.symbol_id = d.symbol_id
JOIN source_files AS sf
  ON sf.source_file_id = s.source_file_id
WHERE m.identifier = 'spe.samples.count'
GROUP BY s.source_file_id, sf.target_location, sf.host_location
HAVING SUM(d.measurement_value) > 0
ORDER BY memory_access_samples DESC, s.source_file_id
"""


def resolve_prerecord_config(test_case: dict[str, Any], defaults: dict[str, Any] | None = None) -> dict[str, Any]:
    """Return the merged prerecord configuration with a validated profile mode."""

    defaults = defaults or {}
    default_config = defaults.get("prerecord", {})
    test_config = test_case.get("prerecord", {})
    if not isinstance(default_config, dict) or not isinstance(test_config, dict):
        raise ValueError(
            f"prerecord for testcase {test_case.get('id', '<missing>')!r} "
            "must be an object"
        )
    config = {**default_config, **test_config}
    mode = config.get("mode")
    if not isinstance(mode, str) or mode not in PRERECORD_MODES:
        raise ValueError(
            f"unsupported prerecord mode for testcase "
            f"{test_case.get('id', '<missing>')!r}: {mode!r}"
        )
    config["mode"] = mode
    return config


def resolve_recipe(test_case: dict[str, Any], defaults: dict[str, Any]) -> str:
    """Return and validate the manifest recipe for one testcase."""

    recipe = str(test_case.get("recipe", defaults.get("recipe", ""))).strip()
    if recipe not in SUPPORTED_RECIPES:
        raise ValueError(f"unknown or unsupported recipe type: {recipe or '<missing>'}")
    return recipe


def grouped_recipe_params(
    recipe_params: Iterable[str] | None,
) -> dict[str, list[str]]:
    """Return validated recipe params grouped by key in first-seen order."""

    grouped_params: dict[str, list[str]] = {}
    for param in recipe_params or []:
        if not isinstance(param, str):
            raise ValueError("recipe_params must be an iterable of strings")
        name, sep, value = param.partition("=")
        name = name.strip()
        if not sep or not name:
            raise ValueError(f"expected NAME=VALUE recipe param, got {param!r}")
        grouped_params.setdefault(name, []).append(value.strip())
    return grouped_params


def instruction_mix_mode(recipe_params: Iterable[str] | None) -> str:
    """Return the validated Instruction Mix mode, or an empty default."""

    modes = grouped_recipe_params(recipe_params).get("mode", [])
    if len(modes) > 1:
        raise ValueError("duplicate recipe param 'mode'")
    mode = modes[0].strip() if modes else ""
    if not mode:
        return ""
    if mode not in INSTRUCTION_MIX_SOURCE_MODES:
        raise ValueError(
            f"unsupported instruction_mix mode {mode!r}; "
            f"expected one of {sorted(INSTRUCTION_MIX_SOURCE_MODES)!r}"
        )
    return mode


def source_files_query(
    recipe: str, recipe_params: Iterable[str] | None = None
) -> str | None:
    """Return the source discovery query when one is available."""

    if recipe in {CODE_HOTSPOTS_RECIPE, CPU_MICROARCHITECTURE_RECIPE}:
        return CODE_HOTSPOTS_SOURCE_FILES_QUERY
    if recipe == INSTRUCTION_MIX_RECIPE:
        if instruction_mix_mode(recipe_params) != "static":
            return INSTRUCTION_MIX_SOURCE_FILES_QUERY
    if recipe == CACHE_SHARING_RECIPE:
        return CACHE_SHARING_SOURCE_FILES_QUERY
    if recipe == MEMORY_ACCESS_RECIPE:
        return MEMORY_ACCESS_SOURCE_FILES_QUERY
    return None


def sampled_source_weight(recipe: str, row: dict[str, Any]) -> Any:
    """Return the recorded source weight for a supported source query."""

    if recipe in {CODE_HOTSPOTS_RECIPE, CPU_MICROARCHITECTURE_RECIPE}:
        return row.get("periodic_samples")
    if recipe == INSTRUCTION_MIX_RECIPE:
        return row.get("self_samples")
    if recipe == CACHE_SHARING_RECIPE:
        return row.get("cache_sharing_samples")
    if recipe == MEMORY_ACCESS_RECIPE:
        return row.get("memory_access_samples")
    raise ValueError(f"source weights are not supported for recipe {recipe!r}")


def requires_source_archive(recipe: str, recipe_params: Iterable[str] | None = None) -> bool:
    """Return whether the recipe supports a source archive."""

    return source_files_query(recipe, recipe_params) is not None


def mode_supports_recipe(mode: str, recipe: str) -> bool:
    """Return whether an evaluation mode supports a recipe."""

    if mode == PERFORMIX_MCP_MODE:
        return True
    if mode in RECIPE_RESTRICTED_MODES:
        return recipe in RECIPE_RESTRICTED_MODES[mode]
    raise ValueError(f"unknown or unsupported AI Insights mode: {mode or '<missing>'}")
