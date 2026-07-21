# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

from pathlib import Path


def default_tools_dir() -> Path:
    """
    Return the default tools directory used by scripts.

    This is computed relative to this repository layout:
        scripts/../apap-cli/tools
    """
    return Path(__file__).resolve().parent.parent.parent / "apap-cli" / "tools"
