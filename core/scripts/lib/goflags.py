# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Centralized handling of the mandatory ``duckdb_arrow`` Go build tag.

Every build, test, and lint of the Go modules must enable this tag (the engine
links DuckDB's Arrow interface behind it). Defining the tag name in exactly one
place keeps the build/test/lint scripts consistent across platforms.
"""

from __future__ import annotations

import os
from collections.abc import Mapping

# The single source of truth for the required build tag name.
DUCKDB_ARROW_BUILD_TAG = "duckdb_arrow"

# Value used for the GOFLAGS environment variable, e.g. ``-tags=duckdb_arrow``.
GOFLAGS_TAGS = f"-tags={DUCKDB_ARROW_BUILD_TAG}"


def go_build_tag_flag() -> str:
    """Return the ``-tags=duckdb_arrow`` flag for direct ``go`` invocations."""
    return GOFLAGS_TAGS


def golangci_build_tags_flag() -> str:
    """Return the ``--build-tags=duckdb_arrow`` flag for ``golangci-lint``."""
    return f"--build-tags={DUCKDB_ARROW_BUILD_TAG}"


def go_env(base_env: Mapping[str, str] | None = None) -> dict[str, str]:
    """
    Return an environment mapping with ``GOFLAGS`` set to enable the
    ``duckdb_arrow`` build tag.

    This mirrors the Bash scripts which prefix Go commands with
    ``GOFLAGS='-tags=duckdb_arrow'``. ``base_env`` defaults to the current
    process environment; only the ``GOFLAGS`` key is set/overridden so the rest
    of the environment is preserved.
    """
    env = dict(base_env if base_env is not None else os.environ)
    env["GOFLAGS"] = GOFLAGS_TAGS
    return env
