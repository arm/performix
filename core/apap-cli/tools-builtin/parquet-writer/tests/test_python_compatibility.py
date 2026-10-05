# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import ast
from pathlib import Path


def test_wrapper_uses_python_38_compatible_syntax():
    source_path = Path(__file__).parents[1] / "apx_parquet_writer.py"
    ast.parse(
        source_path.read_text(encoding="utf-8"),
        filename=str(source_path),
        feature_version=8,
    )
