# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from types import SimpleNamespace
import unittest

from pass_rate import passing_attempt_groups as evaluate_groups


def passing_attempt_groups(reports):
    return evaluate_groups(reports, 0.66)


def attempt(number, outcome="passed", *, total=3, score="pass", model="model-high", mode="performix_mcp", case="case"):
    return SimpleNamespace(
        when="call", outcome=outcome, passed=outcome == "passed",
        failed=outcome == "failed", skipped=outcome == "skipped",
        user_properties=list(dict(ai_test_id=case, ai_mode=mode, ai_model_config=model,
                                  ai_attempt=number, ai_attempts_total=total, ai_scores=score).items()),
    )


class PassRateTests(unittest.TestCase):
    def test_configured_pass_rate(self):
        for passes in range(4):
            with self.subTest(passes=passes):
                reports = [attempt(i, "passed" if i <= passes else "failed",
                                   score="pass" if i <= passes else "fail") for i in range(1, 4)]
                self.assertEqual(passes >= 2, passing_attempt_groups(reports) is not None)

    def test_result_contains_counts_and_policy(self):
        reports = [attempt(1), attempt(2, "failed", score="fail"), attempt(3)]
        results = passing_attempt_groups(reports)
        result = results[("case", "performix_mcp", "model-high")]
        self.assertEqual((2, 3, 0.66, True),
                         (result.passes, result.total, result.min_pass_rate, result.passed))
        self.assertEqual("Passed: 2/3 attempts; minimum 0.66", result.summary)

    def test_strict_pass_rate_rejects_a_failed_attempt(self):
        reports = [attempt(1), attempt(2), attempt(3, "failed", score="fail")]
        self.assertIsNone(evaluate_groups(reports, 1.0))

    def test_one_attempt_and_even_tie(self):
        self.assertIsNotNone(passing_attempt_groups([attempt(1, total=1)]))
        self.assertIsNone(passing_attempt_groups([attempt(1, "failed", total=1, score="fail")]))
        self.assertIsNone(passing_attempt_groups([attempt(1, total=2), attempt(2, "failed", total=2)]))

    def test_performance_failure_does_not_count_as_pass(self):
        reports = [attempt(1), attempt(2, "failed"), attempt(3, "failed")]
        self.assertIsNone(passing_attempt_groups(reports))
        reports[2] = attempt(3)
        self.assertIsNotNone(passing_attempt_groups(reports))

    def test_infrastructure_error_blocks_majority(self):
        self.assertIsNone(passing_attempt_groups([attempt(1), attempt(2), attempt(3, "failed", score="error")]))

    def test_missing_performance_measurement_blocks_majority(self):
        report = attempt(3, "failed")
        report.user_properties.append(("ai_performance_input_tokens_quality", "INDETERMINABLE"))
        self.assertIsNone(passing_attempt_groups([attempt(1), attempt(2), report]))

    def test_incomplete_or_duplicate_group_blocks_majority(self):
        for reports in ([attempt(1), attempt(2)], [attempt(1), attempt(2), attempt(2)],
                        [attempt(1), attempt(2), attempt(3, total=4)]):
            self.assertIsNone(passing_attempt_groups(reports))

    def test_cases_models_and_modes_are_not_pooled(self):
        for field in ("case", "model", "mode"):
            reports = [attempt(i) for i in range(1, 4)]
            reports += [attempt(i, "failed" if i > 1 else "passed", **{field: "other"}) for i in range(1, 4)]
            self.assertIsNone(passing_attempt_groups(reports))

    def test_unrelated_and_lifecycle_failures_block_majority(self):
        for when in ("setup", "call", "teardown"):
            report = attempt(1, "failed")
            report.when = when
            report.user_properties = []
            self.assertIsNone(passing_attempt_groups([attempt(1), attempt(2), attempt(3), report]))

    def test_expected_model_failure_is_accepted_but_error_is_not(self):
        report = attempt(3, "skipped", score="fail")
        report.wasxfail = "known model failure"
        self.assertIsNotNone(passing_attempt_groups([attempt(1), attempt(2), report]))
        report.user_properties.append(("ai_scores", "error"))
        self.assertIsNone(passing_attempt_groups([attempt(1), attempt(2), report]))
