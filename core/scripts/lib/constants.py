# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Constants and shared filesystem locations for the build/tooling scripts.

This is the Python replacement for ``constants.sh`` (Artifactory URLs) and
``get-project-root.sh`` (the ``core/`` root). Keep the values here in sync with
those scripts until the remaining Bash callers are ported.
"""

from __future__ import annotations

from pathlib import Path

# Artifactory URL constants
ARTIFACTORY_BASE_URL = "https://artifactory.arm.com/artifactory"
ARTIFACTORY_TOOLS_BASE_URL = "https://artifacts.tools.arm.com"
ARTIFACTORY_INTERNAL_TOOLS_BASE_URL = "https://artifacts.internal.tools.arm.com"


def get_core_root() -> Path:
    """
    Return the project root directory (the ``core/`` directory).

    This is the Python replacement for ``get-project-root.sh``. The root is
    resolved from this file's location, independent of the current working
    directory: ``core/scripts/lib/constants.py`` -> ``core/``.
    """
    return Path(__file__).resolve().parents[2]
