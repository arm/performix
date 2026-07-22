# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import os
import sys

import pytest


MODULE_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
if MODULE_DIR not in sys.path:
    sys.path.insert(0, MODULE_DIR)


@pytest.fixture()
def fixtures_dir() -> str:
    return os.path.join(os.path.dirname(__file__), "fixtures")
