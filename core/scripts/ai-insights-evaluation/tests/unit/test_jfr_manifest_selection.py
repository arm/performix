# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""JFR cases are included in the production MCP evaluation."""

import json
from pathlib import Path

import pytest

from tests.evaluation.test_ai_insights_evaluation import iter_manifest_parameters


MANIFEST = Path(__file__).resolve().parents[2] / "ai_insights_evaluation.json"
JFR_CASES = {"test_case_20", "test_case_71", "test_case_72", "test_case_73", "test_case_74"}


class Options:
    def __init__(self, modes: str):
        self.modes = modes

    def getoption(self, name: str) -> str:
        return {
            "--ai-modes": self.modes,
            "--ai-act": "act1,act2",
            "--ai-recipe": "",
        }[name]


def test_jfr_cases_are_included_by_default() -> None:
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    assert {
        test_case["id"] for test_case in manifest["tests"]
        if "jfr_data" in test_case.get("requirements", [])
    } == JFR_CASES
    for modes in ("", "performix_mcp"):
        selected = iter_manifest_parameters(Options(modes), manifest)
        selected_jfr = [param for param in selected if param["test_case"]["id"] in JFR_CASES]
        assert {param["test_case"]["id"] for param in selected_jfr} == JFR_CASES
        assert {param["mode"] for param in selected_jfr} == {"performix_mcp"}


def test_invalid_case_mode_allowlist_fails() -> None:
    manifest = {
        "defaults": {"recipe": "code_hotspots", "prerecord": {"mode": "launch"}},
        "tests": [{"id": "test_case_71", "acts": ["act1", "act2"], "evaluation_modes": ["unknown"]}],
    }
    with pytest.raises(ValueError, match="unknown or unsupported"):
        iter_manifest_parameters(Options("performix_mcp"), manifest)
