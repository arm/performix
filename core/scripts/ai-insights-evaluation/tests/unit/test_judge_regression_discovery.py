# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0
"""Check saved-answer discovery without making judge calls."""
import pytest

from judge_regression.test_judge_regression import discover_regressions


@pytest.fixture
def directories(tmp_path):
    cases = tmp_path / "cases"
    cases.mkdir()
    rubrics = tmp_path / "rubrics"
    rubrics.mkdir()
    (rubrics / "test_case_72.md").write_text("scoring rules")
    return cases, rubrics


def add_regression(cases, name):
    regression = cases / "test_case_72" / name
    regression.mkdir(parents=True)
    (regression / "response.md").write_text("reference answer")
    (regression / "negative_control.md").write_text("incorrect answer")
    return regression


def test_discovers_multiple_regressions(directories):
    cases, rubrics = directories
    add_regression(cases, "second")
    add_regression(cases, "first")
    (cases / "__pycache__").mkdir()
    assert discover_regressions(cases, rubrics) == [
        ("test_case_72", name, filename, expected)
        for name in ("first", "second")
        for filename, expected in (("response.md", "pass"), ("negative_control.md", "fail"))
    ]


@pytest.mark.parametrize("missing", ["response.md", "negative_control.md"])
def test_missing_answer_is_an_error(directories, missing):
    cases, rubrics = directories
    regression = add_regression(cases, "broken")
    (regression / missing).unlink()
    with pytest.raises(pytest.UsageError, match=f"Malformed regression directory .*missing {missing}"):
        discover_regressions(cases, rubrics)


def test_missing_scoring_rules_is_an_error(directories):
    cases, rubrics = directories
    add_regression(cases, "example")
    (rubrics / "test_case_72.md").unlink()
    with pytest.raises(pytest.UsageError, match="Missing scoring rules"):
        discover_regressions(cases, rubrics)


def test_empty_case_is_an_error(directories):
    cases, rubrics = directories
    (cases / "test_case_72").mkdir()
    with pytest.raises(pytest.UsageError, match="No regression directories"):
        discover_regressions(cases, rubrics)


def test_empty_suite_is_an_error(directories):
    with pytest.raises(pytest.UsageError, match="No regressions found"):
        discover_regressions(*directories)
