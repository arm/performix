# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import ast
from pathlib import Path


def test_collector_uses_python_38_compatible_syntax():
    package_dir = Path(__file__).parents[1] / "pytorch_collect"

    for source_path in package_dir.glob("*.py"):
        ast.parse(
            source_path.read_text(encoding="utf-8"),
            filename=str(source_path),
            feature_version=8,
        )
