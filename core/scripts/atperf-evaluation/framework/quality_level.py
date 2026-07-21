# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from enum import Enum
from typing import Dict

class QualityLevel(Enum):
    GOOD = "GOOD"
    MODERATE = "MODERATE"
    POOR = "POOR"
    # Quality level is expected, but cannot be determined - this indicates a problem
    INDETERMINABLE = "INDETERMINABLE"

    @property
    def index(self) -> int:
        """lower index -> higher priority"""
        return {
            # Problematic quality levels are higher priority, GOOD is lowest
            QualityLevel.GOOD: 3,
            QualityLevel.MODERATE: 2,
            QualityLevel.POOR: 1,
            QualityLevel.INDETERMINABLE: 0,
        }[self]

def _quality_distribution(*, good: float = 0.0, moderate: float = 0.0, poor: float = 0.0,
                          indeterminable: float = 0.0) -> Dict["QualityLevel", float]:
    """Return a fresh quality distribution mapping keyed by QualityLevel."""
    return {
        QualityLevel.GOOD: float(good),
        QualityLevel.MODERATE: float(moderate),
        QualityLevel.POOR: float(poor),
        QualityLevel.INDETERMINABLE: float(indeterminable),
    }

def indeterminable_distribution() -> Dict["QualityLevel", float]:
    """Return a fresh distribution with 100% indeterminable and 0% elsewhere."""
    return _quality_distribution(indeterminable=100.0)
