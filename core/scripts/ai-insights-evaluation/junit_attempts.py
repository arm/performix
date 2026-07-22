# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Read AI Insights evaluation attempts from pytest JUnit XML."""

from __future__ import annotations

from pathlib import Path
import xml.etree.ElementTree as ET


AI_PROPERTY_PREFIX = "ai_"


def attempts_from_junit(junit_xml: Path) -> list[dict[str, str]]:
    """Read AI Insights recorded properties from pytest JUnit XML."""
    root = ET.parse(junit_xml).getroot()
    attempts = []
    for testcase in root.iter():
        if _local_name(testcase.tag) != "testcase":
            continue
        properties = _junit_properties(testcase)
        if properties:
            properties["pytest_outcome"] = _junit_outcome(testcase)
            attempts.append(properties)
    return attempts


def _junit_properties(testcase: ET.Element) -> dict[str, str]:
    properties: dict[str, str] = {}
    for child in testcase:
        if _local_name(child.tag) != "properties":
            continue
        for prop in child:
            if _local_name(prop.tag) != "property":
                continue
            name = prop.attrib.get("name")
            if name and name.startswith(AI_PROPERTY_PREFIX):
                properties[name] = prop.attrib.get("value", "")
    return properties


def _junit_outcome(testcase: ET.Element) -> str:
    for child in testcase:
        name = _local_name(child.tag)
        if name == "failure":
            return "failed"
        if name == "error":
            return "error"
        if name == "skipped":
            return "skipped"
    return "passed"


def _local_name(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]
