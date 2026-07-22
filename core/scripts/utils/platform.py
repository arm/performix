# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

from dataclasses import dataclass

# Canonical OS identifiers used across tooling.
ATPERF_TOOL_OS_LINUX = "Linux"
ATPERF_TOOL_OS_WINDOWS = "Windows"

# Canonical architecture identifiers used across tooling.
ATPERF_TOOL_ARCH_X86_64 = "x86_64"
ATPERF_TOOL_ARCH_AARCH64 = "aarch64"

# Common input -> canonical mappings.
ATPERF_TOOL_OS_MAPPINGS: dict[str, str] = {
    "linux": ATPERF_TOOL_OS_LINUX,
    "windows": ATPERF_TOOL_OS_WINDOWS,
}

ATPERF_TOOL_ARCH_MAPPINGS: dict[str, str] = {
    "x86_64": ATPERF_TOOL_ARCH_X86_64,
    "amd64": ATPERF_TOOL_ARCH_X86_64,
    "x86": ATPERF_TOOL_ARCH_X86_64,
    "aarch64": ATPERF_TOOL_ARCH_AARCH64,
    "arm64": ATPERF_TOOL_ARCH_AARCH64,
}


def format_os(os_input: str) -> str:
    """
    Normalize an OS string into the canonical OS identifier.

    Accepted inputs are keys in `ATPERF_TOOL_OS_MAPPINGS` (case-insensitive).
    """
    mapped = ATPERF_TOOL_OS_MAPPINGS.get(os_input.lower())
    if not mapped:
        raise ValueError(f"Unsupported OS: {os_input}")
    return mapped


def format_arch(arch_input: str) -> str:
    """
    Normalize an architecture string into the canonical arch identifier.

    Accepted inputs are keys in `ATPERF_TOOL_ARCH_MAPPINGS` (case-insensitive).
    """
    mapped = ATPERF_TOOL_ARCH_MAPPINGS.get(arch_input.lower())
    if not mapped:
        raise ValueError(f"Unsupported architecture: {arch_input}")
    return mapped


@dataclass()
class Platform:
    os: str
    arch: str
