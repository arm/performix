# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

from .platform import format_arch, format_os


def generate_tool_tarball_name(tool_name: str, os_input: str, arch_input: str) -> str:
    """
    Return the standard tarball filename for a tool for the given platform.

    This normalizes OS and arch using the same mappings as the rest of the scripts,
    then formats the canonical tarball name:

        {tool_name}-{FormattedOS}-{FormattedArch}.tar.gz

    Example:
        generate_tool_tarball_name("dotnet-agent", "linux", "arm64")
        -> "dotnet-agent-Linux-aarch64.tar.gz"
    """
    tool_os = format_os(os_input)
    tool_arch = format_arch(arch_input)
    return f"{tool_name}-{tool_os}-{tool_arch}.tar.gz"
