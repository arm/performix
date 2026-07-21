# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from framework.engine_overhead_metrics import MetricType


# topdown_accuracy

# Maximum % delta between atperf mean and topdown mean to classify as GOOD quality
TOPDOWN_ACCURACY_GOOD_QUALITY_THRESHOLD = 5.0
# Maximum % delta between atperf mean and topdown mean to classify as MODERATE quality
TOPDOWN_ACCURACY_MODERATE_QUALITY_THRESHOLD = 10.0
# If atperf and topdown_tool MPKI means for a metric are both below this threshold, the metric will be filtered out
TOPDOWN_ACCURACY_MPKI_IGNORE_THRESHOLD = 0.001
# If atperf and topdown_tool percentage means for a metric are both below this threshold, the metric will be filtered out
TOPDOWN_ACCURACY_PERCENT_IGNORE_THRESHOLD = 0.001


# engine_overhead

# Multiplier for calculations that determine quality level assignment
ENGINE_OVERHEAD_MODERATE_MULTIPLIER = 0.5

# Workload-independent quality assessment targets (the smaller the time or disk space overhead measured, the better)
ENGINE_OVERHEAD_TARGETS = {
    MetricType.CONNECTION_MS: 50.0,
    MetricType.TARGET_PREPARATION_MS: 1000.0,
    # MetricType.COLLECTION_MS is workload-dependent, see get_workload_engine_overhead_targets() below
    # MetricType.ANALYSIS_MS is calculated as 2x COLLECTION_MS, see get_workload_engine_overhead_targets() below
    # MetricType.COLLECTION_ANALYSIS_MS is calculated as COLLECTION_MS + ANALYSIS_MS, see get_workload_engine_overhead_targets() below
    MetricType.FILE_TRANSMISSION_MS: 1000.0,
    # MetricType.RECIPE_RUN_TOTAL_MS is calculated as the sum of the other recipe run timing targets, see get_workload_engine_overhead_targets() below
    MetricType.RUN_SIZE_MB: 10.0,
    MetricType.RENDER_TIME_MS: 1000.0,
    MetricType.COMPARISON_RENDER_TIME_MS: 1000.0,
    MetricType.RENDER_MEMORY_MB: 30.0,
    MetricType.COMPARISON_MEMORY_USAGE_MB: 40.0,
}

# Dictionary to map workload name fragments to their workload-dependent COLLECTION_MS target.
# Both "stress-ng" and "stress_ng" are listed to handle path/name variations.
_WORKLOAD_COLLECTION_TARGETS_MS: dict[str, float] = {
    "megabench": 7000,
    "stress-ng": 33000,
    "stress_ng": 33000,
}

def is_recognised_workload(workload_name: str) -> bool:
    """Return True if workload_name matches a known workload with defined collection targets."""
    return any(fragment in workload_name for fragment in _WORKLOAD_COLLECTION_TARGETS_MS)

def get_workload_engine_overhead_targets(workload_name: str) -> dict[MetricType, float]:
    """Return a dict of MetricType keys with RAG target values for workload_name.

    Raises ValueError for unrecognised workloads. Callers should check is_recognised_workload()
    before calling this function, or handle the ValueError explicitly.
    """
    collection_ms = next(
        (target for fragment, target in _WORKLOAD_COLLECTION_TARGETS_MS.items() if fragment in workload_name),
        None,
    )
    if collection_ms is None:
        raise ValueError(
            f"No collection target defined for workload '{workload_name}'. "
            "Check is_recognised_workload() before calling get_workload_engine_overhead_targets()."
        )

    workload_quality_targets = ENGINE_OVERHEAD_TARGETS.copy()
    workload_quality_targets[MetricType.COLLECTION_MS] = collection_ms

    # Calculate ANALYSIS target
    workload_quality_targets[MetricType.ANALYSIS_MS] = 2.0 * workload_quality_targets[MetricType.COLLECTION_MS]

    # Calculate COLLECTION_ANALYSIS target
    workload_quality_targets[MetricType.COLLECTION_ANALYSIS_MS] = workload_quality_targets[MetricType.COLLECTION_MS] + workload_quality_targets[MetricType.ANALYSIS_MS]

    # Override RECIPE_RUN_TOTAL target
    workload_quality_targets[MetricType.RECIPE_RUN_TOTAL_MS] = sum([
        workload_quality_targets[MetricType.CONNECTION_MS],
        workload_quality_targets[MetricType.TARGET_PREPARATION_MS],
        workload_quality_targets[MetricType.COLLECTION_MS],
        workload_quality_targets[MetricType.ANALYSIS_MS],
        workload_quality_targets[MetricType.FILE_TRANSMISSION_MS],
    ])

    return workload_quality_targets
