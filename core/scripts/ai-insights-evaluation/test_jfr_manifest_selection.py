# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""The JFR cases are available only through the query path."""

import json
from pathlib import Path

import pytest

from test_ai_insights_evaluation import iter_manifest_parameters


MANIFEST = Path(__file__).with_name("ai_insights_evaluation.json")
JFR_CASES = {"test_case_20", "test_case_71", "test_case_72", "test_case_73"}


class Options:
    def __init__(self, modes: str):
        self.modes = modes

    def getoption(self, name: str) -> str:
        return {
            "--ai-modes": self.modes,
            "--ai-act": "act1,act2",
            "--ai-recipe": "",
        }[name]


def test_jfr_cases_are_query_only() -> None:
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    default = iter_manifest_parameters(Options(""), manifest)
    assert not JFR_CASES.intersection(param["test_case"]["id"] for param in default)

    selected = iter_manifest_parameters(Options("performix_mcp_run_query"), manifest)
    selected_jfr = [param for param in selected if param["test_case"]["id"] in JFR_CASES]
    assert {param["test_case"]["id"] for param in selected_jfr} == JFR_CASES
    assert {param["mode"] for param in selected_jfr} == {"performix_mcp_run_query"}


def test_invalid_case_mode_allowlist_fails() -> None:
    manifest = {
        "defaults": {"recipe": "code_hotspots", "prerecord": {"mode": "launch"}},
        "tests": [{"id": "test_case_71", "acts": ["act1", "act2"], "evaluation_modes": ["unknown"]}],
    }
    with pytest.raises(ValueError, match="unknown or unsupported"):
        iter_manifest_parameters(Options("performix_mcp_run_query"), manifest)
