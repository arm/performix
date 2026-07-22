# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Custom Robocop rules for Robot file SPDX headers."""

from __future__ import annotations

from robocop.linter.rules import RawFileChecker, Rule, RuleSeverity


class MissingSpdxHeaderRule(Rule):
    """Robot file is missing the required SPDX header."""

    name = "missing-spdx-header"
    rule_id = "LIC01"
    message = "Robot file must start with SPDX copyright and license header"
    severity = RuleSeverity.WARNING
    added_in_version = "1.0.0"


class UnexpectedIgnoredDataBeforeSectionRule(Rule):
    """Unexpected ignored data before the first Robot section."""

    name = "unexpected-ignored-data-before-section"
    rule_id = "LIC02"
    message = "Unexpected ignored data before first Robot section"
    severity = RuleSeverity.WARNING
    added_in_version = "1.0.0"


class RobotSpdxHeaderChecker(RawFileChecker):
    """Allow only the repository SPDX header before the first Robot section."""

    missing_spdx_header: MissingSpdxHeaderRule
    unexpected_ignored_data_before_section: UnexpectedIgnoredDataBeforeSectionRule

    COPYRIGHT_PREFIX = "# SPDX" "-FileCopyrightText:"
    LICENSE_PREFIX = "# SPDX" "-License-Identifier:"
    SECTION_HEADER_PREFIX = "***"

    def parse_file(self) -> None:
        lines = self.source_file.source_lines
        if not lines:
            self.report(self.missing_spdx_header, lineno=1, col=1)
            return

        first_section_index = next(
            (index for index, line in enumerate(lines) if line.startswith(self.SECTION_HEADER_PREFIX)),
            len(lines),
        )
        lines_before_first_section = lines[:first_section_index]

        if not self.has_spdx_header(lines_before_first_section):
            self.report(self.missing_spdx_header, lineno=1, col=1)
            return

        for index, line in enumerate(lines_before_first_section[2:], start=3):
            if not line.strip():
                continue
            self.report(
                self.unexpected_ignored_data_before_section,
                lineno=index,
                col=1,
                end_col=len(line.rstrip()) + 1,
            )
            return

    def has_spdx_header(self, lines: list[str]) -> bool:
        return (
            len(lines) >= 2
            and lines[0].startswith(self.COPYRIGHT_PREFIX)
            and lines[1].startswith(self.LICENSE_PREFIX)
        )

    def check_line(self, line: str, lineno: int) -> None:
        return None
