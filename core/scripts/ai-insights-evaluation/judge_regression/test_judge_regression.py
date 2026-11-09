# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0
"""Evaluate judge verdicts against saved responses without running an agent."""
from pathlib import Path

import pytest

from tests.evaluation.test_ai_insights_evaluation import judge_response

ROOT = Path(__file__).resolve().parent.parent


def discover_regressions(directory, rubrics):
    cases = []
    for test_case in sorted(directory.iterdir()):
        if not test_case.is_dir() or test_case.name.startswith((".", "__")):
            continue
        rubric = rubrics / f"{test_case.name}.md"
        if not rubric.is_file():
            raise pytest.UsageError(f"Missing scoring rules for {test_case}: {rubric}")
        regressions = [path for path in sorted(test_case.iterdir()) if path.is_dir()]
        if not regressions:
            raise pytest.UsageError(f"No regression directories in {test_case}")
        for regression in regressions:
            for filename in ("response.md", "negative_control.md"):
                if not (regression / filename).is_file():
                    raise pytest.UsageError(f"Malformed regression directory {regression}: missing {filename}")
            for filename, expected in (("response.md", "pass"), ("negative_control.md", "fail")):
                cases.append((test_case.name, regression.name, filename, expected))
    if not cases:
        raise pytest.UsageError(f"No regressions found in {directory}")
    return cases


@pytest.fixture(scope="session", autouse=True)
def require_openai_key(pytestconfig):
    api_key = (pytestconfig.getoption("--ai-openai-api-key") or "").strip()
    if not api_key:
        pytest.fail("Set OPENAI_API_KEY or --ai-openai-api-key", pytrace=False)


@pytest.mark.parametrize(
    "test_id,regression,response,expected",
    discover_regressions(ROOT / "judge_regression", ROOT / "rubrics"),
)
def test_saved_response(pytestconfig, test_id, regression, response, expected):
    verdict = judge_response(
        test_id,
        (ROOT / "rubrics" / f"{test_id}.md").read_text(encoding="utf-8"),
        (ROOT / "judge_regression" / test_id / regression / response).read_text(encoding="utf-8"),
        {
            "openai_api_key": pytestconfig.getoption("--ai-openai-api-key").strip(),
            "judge_model": pytestconfig.getoption("--ai-judge-model"),
            "judge_reasoning_effort": pytestconfig.getoption("--ai-judge-reasoning-effort"),
        },
    )
    assert verdict["label"] == expected, verdict
