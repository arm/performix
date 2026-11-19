# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Check the pass rate without changing individual attempt reports."""

from collections import defaultdict
from dataclasses import dataclass


@dataclass(frozen=True)
class AttemptGroupResult:
    passes: int
    total: int
    min_pass_rate: float
    passed: bool

    @property
    def summary(self) -> str:
        status = "Passed" if self.passed else "Failed"
        return f"{status}: {self.passes}/{self.total} attempts; minimum {self.min_pass_rate:g}"


def attempt_group_result(attempts: list[dict], min_pass_rate: float) -> AttemptGroupResult:
    """Calculate a case result from live or saved attempt properties."""
    total = int(attempts[0]["ai_attempts_total"])
    numbers = sorted(int(props["ai_attempt"]) for props in attempts)
    complete = numbers == list(range(1, total + 1)) and all(
        int(props["ai_attempts_total"]) == total for props in attempts)
    valid = all(
        props.get("ai_scores") in ("pass", "fail")
        and props.get("pytest_outcome") in ("passed", "failed", "xfailed")
        and not any(name.startswith("ai_performance_") and name.endswith("_quality")
                    and value == "INDETERMINABLE" for name, value in props.items())
        for props in attempts)
    passes = sum(props.get("pytest_outcome") in ("passed", "xfailed") for props in attempts)
    return AttemptGroupResult(passes, total, min_pass_rate,
                              bool(complete and valid and total and passes / total >= min_pass_rate))


def passing_attempt_groups(reports: list, min_pass_rate: float) -> dict[tuple, AttemptGroupResult] | None:
    """Return results when every complete case/mode/model group meets the pass rate.

    Setup, teardown, infrastructure, and unrelated test failures always block.
    Require complete groups so missing or duplicate attempts cannot hide a failure.
    """
    groups = defaultdict(list)
    for report in reports:
        if getattr(report, "when", None) != "call":
            if report.failed:
                return None
            continue
        if report.skipped and not getattr(report, "wasxfail", False):
            return None
        props = dict(report.user_properties)
        if "ai_test_id" not in props:
            if report.failed:
                return None
            continue
        key = tuple(props[name] for name in ("ai_test_id", "ai_mode", "ai_model_config"))
        groups[key].append({**props, "pytest_outcome":
                            "xfailed" if getattr(report, "wasxfail", False) else report.outcome})

    results = {key: attempt_group_result(attempts, min_pass_rate)
               for key, attempts in sorted(groups.items())}
    return results if results and all(result.passed for result in results.values()) else None
