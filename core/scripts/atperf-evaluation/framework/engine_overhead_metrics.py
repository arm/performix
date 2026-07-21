# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from enum import Enum


class MetricType(Enum):
    # Timing categories (map to engine_overhead.TimingCategory values)
    CONNECTION_MS = "connection_ms"
    COLLECTION_MS = "collection_ms"
    ANALYSIS_MS = "analysis_ms"
    TARGET_PREPARATION_MS = "target_preparation_ms"
    FILE_TRANSMISSION_MS = "file_transmission_ms"
    COLLECTION_ANALYSIS_MS = "collection_analysis_ms"
    RECIPE_RUN_TOTAL_MS = "recipe_run_total_ms"
    RUN_SIZE_MB = "run_size_mb"
    RENDER_TIME_MS = "render_time_ms"
    RENDER_MEMORY_MB = "render_memory_mb"
    COMPARISON_RENDER_TIME_MS = "comparison_render_time_ms"
    COMPARISON_MEMORY_USAGE_MB = "comparison_memory_usage_mb"