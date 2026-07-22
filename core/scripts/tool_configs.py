# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from dataclasses import dataclass

from utils.platform import (
    ATPERF_TOOL_ARCH_AARCH64,
    ATPERF_TOOL_ARCH_X86_64,
    ATPERF_TOOL_OS_LINUX,
    Platform,
    format_arch,
    format_os,
)


@dataclass
class GitHubConfig:
    """Configuration for GitHub releases."""

    repo: str
    tag_template: str
    asset_template: str

    def tag_for(self, *, version: str) -> str:
        return self.tag_template.format(version=version)

    def asset_for(self, *, version: str, os: str, arch: str) -> str:
        return self.asset_template.format(version=version, os=os, arch=arch)


@dataclass
class ToolConfig:
    """Configuration for a tool available via GitHub releases."""

    tool_name: str
    binary_name: str
    version: str
    available_platforms: list[Platform]
    github_conf: GitHubConfig

    def supports(self, *, os: str, arch: str) -> bool:
        os = format_os(os)
        arch = format_arch(arch)
        for platform in self.available_platforms:
            if platform.os == os and platform.arch == arch:
                return True
        return False


@dataclass
class WheelFromSourceConfig:
    """Configuration for a wheel-based tool built from a source repository."""

    repo_url: str
    source_subdir: str


@dataclass
class WheelFromSourceToolConfig:
    """Configuration for a wheel-based tool packaged from source."""

    tool_name: str
    source_ref: str
    version: str
    available_platforms: list[Platform]
    wheel_from_source_conf: WheelFromSourceConfig


DOTNET_AGENT_CONFIG = ToolConfig(
    tool_name="dotnet-agent",
    binary_name="jitdump-dotnet",
    version="0.9.0",
    available_platforms=[
        Platform(os=ATPERF_TOOL_OS_LINUX, arch=ATPERF_TOOL_ARCH_AARCH64),
        Platform(os=ATPERF_TOOL_OS_LINUX, arch=ATPERF_TOOL_ARCH_X86_64),
    ],
    github_conf=GitHubConfig(
        repo="Arm-Debug/jitdump-dotnet",
        tag_template="v{version}",
        asset_template="jitdump-dotnet-{version}-{os}-{arch}.tar.gz",
    ),
)

JITDUMP_JVM_CONFIG = ToolConfig(
    tool_name="jitdump-jvm",
    binary_name="jitdump-jvm",
    version="0.9.0",
    available_platforms=[
        Platform(os=ATPERF_TOOL_OS_LINUX, arch=ATPERF_TOOL_ARCH_AARCH64),
        Platform(os=ATPERF_TOOL_OS_LINUX, arch=ATPERF_TOOL_ARCH_X86_64),
    ],
    github_conf=GitHubConfig(
        repo="Arm-Debug/jitdump-jvm",
        tag_template="v{version}",
        asset_template="jitdump-jvm-{version}-{os}-{arch}.tar.gz",
    ),
)

TOPDOWN_TOOL_CONFIG = WheelFromSourceToolConfig(
    tool_name="topdown_tool",
    source_ref="2025.08_cmn_preview_r2-RC3",
    # Use "-dev" suffix until the tool's source location is finalised.
    version="0.1.0-dev",
    available_platforms=[
        Platform(os=ATPERF_TOOL_OS_LINUX, arch=ATPERF_TOOL_ARCH_AARCH64),
    ],
    wheel_from_source_conf=WheelFromSourceConfig(
        repo_url="https://gitlab.geo.arm.com/software/upe/valetudo/telemetry-solution.git",
        source_subdir="tools/topdown_tool",
    ),
)

WPERF_CMN_VISUALIZER_CONFIG = WheelFromSourceToolConfig(
    tool_name="wperf_cmn_visualizer",
    source_ref="1.4.0",
    version="1.4.0",
    available_platforms=[
        Platform(os=ATPERF_TOOL_OS_LINUX, arch=ATPERF_TOOL_ARCH_AARCH64),
    ],
    wheel_from_source_conf=WheelFromSourceConfig(
        repo_url="https://gitlab.com/Linaro/WindowsPerf/cmn-mesh-visualizer.git",
        source_subdir=".",
    ),
)

CMN_TOOLS_CONFIG = WheelFromSourceToolConfig(
    tool_name="cmn_tools",
    source_ref="v0.1.0-dev",
    # Use "-dev" suffix until the tool's source location is finalised.
    version="0.1.0-dev",
    available_platforms=[
        Platform(os=ATPERF_TOOL_OS_LINUX, arch=ATPERF_TOOL_ARCH_AARCH64),
    ],
    wheel_from_source_conf=WheelFromSourceConfig(
        repo_url="https://github.com/JamesRFArm/cmn-tools.git",
        source_subdir=".",
    ),
)

GITHUB_TOOL_REGISTRY = {
    "dotnet-agent": DOTNET_AGENT_CONFIG,
    "jitdump-jvm": JITDUMP_JVM_CONFIG,
}

WHEEL_FROM_SOURCE_TOOL_REGISTRY = {
    "cmn_tools": CMN_TOOLS_CONFIG,
    "topdown_tool": TOPDOWN_TOOL_CONFIG,
    "wperf_cmn_visualizer": WPERF_CMN_VISUALIZER_CONFIG,
}
