# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import pytest

from recipe_support import (
    ASCT_RECIPE,
    CACHE_SHARING_RECIPE,
    CODE_HOTSPOTS_RECIPE,
    CPU_MICROARCHITECTURE_RECIPE,
    INSTRUCTION_MIX_RECIPE,
    MEMORY_ACCESS_RECIPE,
    SPE_RECIPES,
    instruction_mix_mode,
    mode_supports_recipe,
    requires_source_archive,
    resolve_prerecord_config,
    sampled_source_weight,
    source_files_query,
)


def test_asct_does_not_require_a_source_archive() -> None:
    assert not requires_source_archive(ASCT_RECIPE)


def test_cpu_microarchitecture_requires_source_archive() -> None:
    assert requires_source_archive(CPU_MICROARCHITECTURE_RECIPE)
    assert mode_supports_recipe("performix_mcp", CPU_MICROARCHITECTURE_RECIPE)
    assert not mode_supports_recipe("rest", CPU_MICROARCHITECTURE_RECIPE)
    assert not mode_supports_recipe("hackathon_mcp", CPU_MICROARCHITECTURE_RECIPE)


def test_cache_sharing_is_production_mcp_only() -> None:
    assert mode_supports_recipe("performix_mcp", CACHE_SHARING_RECIPE)
    assert not mode_supports_recipe("rest", CACHE_SHARING_RECIPE)
    assert not mode_supports_recipe("hackathon_mcp", CACHE_SHARING_RECIPE)


def test_memory_access_requires_spe_and_sampled_source() -> None:
    assert requires_source_archive(MEMORY_ACCESS_RECIPE)
    assert "spe.samples.count" in source_files_query(MEMORY_ACCESS_RECIPE)
    assert MEMORY_ACCESS_RECIPE in SPE_RECIPES
    assert mode_supports_recipe("performix_mcp", MEMORY_ACCESS_RECIPE)
    assert not mode_supports_recipe("rest", MEMORY_ACCESS_RECIPE)
    assert not mode_supports_recipe("hackathon_mcp", MEMORY_ACCESS_RECIPE)


def test_instruction_mix_source_archive_requirement_depends_on_mode() -> None:
    assert requires_source_archive(INSTRUCTION_MIX_RECIPE)
    assert requires_source_archive(INSTRUCTION_MIX_RECIPE, ["mode=dynamic"])
    assert requires_source_archive(INSTRUCTION_MIX_RECIPE, ["mode=both"])
    assert not requires_source_archive(INSTRUCTION_MIX_RECIPE, ["mode=static"])


def test_source_query_is_optional_for_other_recipes() -> None:
    assert source_files_query(CODE_HOTSPOTS_RECIPE) is not None
    assert source_files_query(INSTRUCTION_MIX_RECIPE, ["mode=static"]) is None
    assert source_files_query("custom_recipe") is None
    assert not requires_source_archive("custom_recipe")


def test_sampled_source_weight_uses_recipe_schema() -> None:
    row = {
        "periodic_samples": 100,
        "self_samples": 200,
        "cache_sharing_samples": 300,
        "memory_access_samples": 400,
    }

    assert sampled_source_weight(CODE_HOTSPOTS_RECIPE, row) == 100
    assert sampled_source_weight(CPU_MICROARCHITECTURE_RECIPE, row) == 100
    assert sampled_source_weight(INSTRUCTION_MIX_RECIPE, row) == 200
    assert sampled_source_weight(CACHE_SHARING_RECIPE, row) == 300
    assert sampled_source_weight(MEMORY_ACCESS_RECIPE, row) == 400
    with pytest.raises(ValueError, match="source weights are not supported"):
        sampled_source_weight("system_utilization", row)


def test_instruction_mix_mode_rejects_unknown_values() -> None:
    with pytest.raises(ValueError, match="unsupported instruction_mix mode"):
        instruction_mix_mode(["mode=statc"])


def test_instruction_mix_mode_rejects_duplicate_values() -> None:
    with pytest.raises(ValueError, match="duplicate recipe param 'mode'"):
        instruction_mix_mode(["mode=static", "mode=dynamic"])


@pytest.mark.parametrize("value", ["invalid", True, None])
def test_prerecord_mode_must_be_supported(value: object) -> None:
    with pytest.raises(ValueError, match="unsupported prerecord mode"):
        resolve_prerecord_config(
            {"id": "test_case_32", "prerecord": {"mode": value}}
        )


def test_prerecord_mode_is_required() -> None:
    with pytest.raises(ValueError, match="unsupported prerecord mode"):
        resolve_prerecord_config({"id": "test_case_32"})


def test_prerecord_mode_supports_system_wide() -> None:
    test_case = {"id": "test_case_32", "prerecord": {"mode": "system-wide"}}
    assert resolve_prerecord_config(test_case)["mode"] == "system-wide"
