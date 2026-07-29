# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Custom Robocop rules for Performix Robot tests."""

from __future__ import annotations

import re

from robot.api import Token
from robot.parsing.model.blocks import Keyword, SettingSection, TestCase, VariableSection
from robot.parsing.model.statements import KeywordCall

from robocop.linter.rules import Rule, RuleSeverity, VisitorChecker


BDD_PREFIXES = ("Given", "When", "Then")
BDD_CONTINUATION = "And"
BDD_KEYWORD_PREFIXES = BDD_PREFIXES + (BDD_CONTINUATION,)
TAG_PATTERN = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")


class HyphenatedTagRule(Rule):
    """Tag names must use lower-case hyphenated words."""

    name = "hyphenated-tag"
    rule_id = "PFX01"
    message = "Tag '{tag}' must be lower-case and hyphenated"
    severity = RuleSeverity.WARNING


class TestCaseMustUseBddRule(Rule):
    """Test case keyword calls must use Given/When/Then syntax."""

    name = "test-case-must-use-bdd"
    rule_id = "PFX02"
    message = "Test case keyword calls must start with Given, When, Then, or And"
    severity = RuleSeverity.WARNING


class TestCaseBddStepCountRule(Rule):
    """Test cases must contain exactly one Given, one When, and one Then step."""

    name = "test-case-bdd-step-count"
    rule_id = "PFX03"
    message = "Test case must contain exactly 1 {prefix} step, found {count}"
    severity = RuleSeverity.WARNING


class TestCaseBddStepOrderRule(Rule):
    """Given, When, and Then steps must appear in that order."""

    name = "test-case-bdd-step-order"
    rule_id = "PFX04"
    message = "Given, When, and Then steps must appear in that order"
    severity = RuleSeverity.WARNING


class NoBddPrefixInKeywordRule(Rule):
    """Keyword definitions and keyword bodies must not use BDD prefixes."""

    name = "no-bdd-prefix-in-keywords"
    rule_id = "PFX05"
    message = "Keyword definitions and bodies must not use the {prefix} BDD prefix"
    severity = RuleSeverity.WARNING


class TwoSpaceSeparatorRule(Rule):
    """Robot cell separators must be exactly two spaces."""

    name = "two-space-separator"
    rule_id = "PFX06"
    message = "{message}"
    severity = RuleSeverity.WARNING


def bdd_prefix(keyword_name: str | None, prefixes: tuple[str, ...] = BDD_PREFIXES) -> str | None:
    if not keyword_name:
        return None
    for prefix in prefixes:
        if keyword_name == prefix or keyword_name.startswith(f"{prefix} "):
            return prefix
    return None


def token_col(token: Token) -> int:
    return token.col_offset + 1


def is_runtime_keyword_call(node) -> bool:
    return isinstance(node, KeywordCall) and node.keyword is not None


class PerformixTagChecker(VisitorChecker):
    hyphenated_tag: HyphenatedTagRule

    def visit_DefaultTags(self, node) -> None:  # noqa: N802
        self.check_tags(node)

    def visit_ForceTags(self, node) -> None:  # noqa: N802
        self.check_tags(node)

    def visit_KeywordTags(self, node) -> None:  # noqa: N802
        self.check_tags(node)

    def visit_Tags(self, node) -> None:  # noqa: N802
        self.check_tags(node)

    def visit_TestTags(self, node) -> None:  # noqa: N802
        self.check_tags(node)

    def check_tags(self, node) -> None:
        for tag in node.get_tokens(Token.ARGUMENT):
            if "$" in tag.value:
                continue
            if not TAG_PATTERN.fullmatch(tag.value):
                self.report(
                    self.hyphenated_tag,
                    tag=tag.value,
                    node=node,
                    lineno=tag.lineno,
                    col=token_col(tag),
                    end_col=tag.end_col_offset + 1,
                )


class PerformixBddChecker(VisitorChecker):
    test_case_must_use_bdd: TestCaseMustUseBddRule
    test_case_bdd_step_count: TestCaseBddStepCountRule
    test_case_bdd_step_order: TestCaseBddStepOrderRule
    no_bdd_prefix_in_keywords: NoBddPrefixInKeywordRule

    def __init__(self) -> None:
        self.in_keyword = False
        super().__init__()

    def visit_TestCase(self, node: TestCase) -> None:  # noqa: N802
        bdd_steps: list[str] = []
        previous_bdd_step: str | None = None

        for statement in node.body:
            if not is_runtime_keyword_call(statement):
                continue

            prefix = bdd_prefix(statement.keyword)
            if prefix:
                bdd_steps.append(prefix)
                previous_bdd_step = prefix
                continue

            if bdd_prefix(statement.keyword, (BDD_CONTINUATION,)):
                if previous_bdd_step is None:
                    self.report(self.test_case_must_use_bdd, node=statement)
                continue

            self.report(self.test_case_must_use_bdd, node=statement)

        counts = {prefix: 0 for prefix in BDD_PREFIXES}
        for prefix in bdd_steps:
            counts[prefix] += 1

        for prefix, count in counts.items():
            if count != 1:
                self.report(self.test_case_bdd_step_count, prefix=prefix, count=count, node=node)

        if bdd_steps != sorted(bdd_steps, key=BDD_PREFIXES.index):
            self.report(self.test_case_bdd_step_order, node=node)

        self.generic_visit(node)

    def visit_Keyword(self, node: Keyword) -> None:  # noqa: N802
        prefix = bdd_prefix(node.name)
        if prefix:
            self.report(self.no_bdd_prefix_in_keywords, prefix=prefix, node=node)

        was_in_keyword = self.in_keyword
        self.in_keyword = True
        self.generic_visit(node)
        self.in_keyword = was_in_keyword

    def visit_KeywordCall(self, node: KeywordCall) -> None:  # noqa: N802
        if self.in_keyword:
            prefix = bdd_prefix(node.keyword, BDD_KEYWORD_PREFIXES)
            if prefix:
                self.report(self.no_bdd_prefix_in_keywords, prefix=prefix, node=node)
        self.generic_visit(node)


class PerformixSeparatorChecker(VisitorChecker):
    two_space_separator: TwoSpaceSeparatorRule

    def __init__(self) -> None:
        self.in_alignment_exempt_section = False
        super().__init__()

    def visit_SettingSection(self, node: SettingSection) -> None:  # noqa: N802
        self.visit_alignment_exempt_section(node)

    def visit_VariableSection(self, node: VariableSection) -> None:  # noqa: N802
        self.visit_alignment_exempt_section(node)

    def visit_alignment_exempt_section(self, node: SettingSection | VariableSection) -> None:
        was_in_alignment_exempt_section = self.in_alignment_exempt_section
        self.in_alignment_exempt_section = True
        self.generic_visit(node)
        self.in_alignment_exempt_section = was_in_alignment_exempt_section

    def visit_Statement(self, node) -> None:  # noqa: N802
        if self.in_alignment_exempt_section:
            return

        if not hasattr(node, "get_tokens"):
            return

        for separator in node.get_tokens(Token.SEPARATOR):
            value = separator.value
            if "\t" in value:
                self.report(
                    self.two_space_separator,
                    message="Use spaces, not tabs, in Robot separators",
                    node=node,
                    lineno=separator.lineno,
                    col=token_col(separator),
                    end_col=separator.end_col_offset + 1,
                )
                continue

            if separator.col_offset == 0:
                if len(value) % 2 != 0:
                    self.report(
                        self.two_space_separator,
                        message="Robot indentation must use multiples of 2 spaces",
                        node=node,
                        lineno=separator.lineno,
                        col=token_col(separator),
                        end_col=separator.end_col_offset + 1,
                    )
                continue

            if value != "  ":
                self.report(
                    self.two_space_separator,
                    message="Robot cell separators must be exactly 2 spaces",
                    node=node,
                    lineno=separator.lineno,
                    col=token_col(separator),
                    end_col=separator.end_col_offset + 1,
                )
