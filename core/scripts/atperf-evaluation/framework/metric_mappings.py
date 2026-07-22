# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
===============================================================================
metric_mappings.py

Purpose:
    Define mappings between topdown_tool and atperf metric names.
    topdown_tool uses snake_case naming (e.g., "branch_mpki")
    atperf uses Title Case naming (e.g., "Branch MPKI")

Usage:
    Use get_atperf_metric_name() to convert topdown_tool metric names to atperf format
    for comparison purposes.

===============================================================================
"""

# Mapping from topdown_tool metric names to atperf metric names
# Key: topdown_tool metric name (snake_case)
# Value: atperf metric name (Title Case)
TOPDOWN_TOOL_TO_ATPERF_MAPPING = {
    # Branch Effectiveness
    "branch_misprediction_percentage": "Branch Misprediction Percentage",
    "branch_mpki": "Branch MPKI",

    # DTLB Effectiveness
    "dtlb_mpki": "DTLB MPKI",
    "dtlb_walk_ratio": "DTLB Walk Ratio",
    "dtlb_walk_percentage": "DTLB Walk Percentage",
    "l1d_tlb_miss_ratio": "L1 Data TLB Miss Ratio",
    "l1d_tlb_miss_percentage": "L1 Data TLB Miss Percentage",
    "l1d_tlb_mpki": "L1 Data TLB MPKI",
    "l2_tlb_miss_ratio": "L2 TLB Miss Ratio",
    "l2_tlb_miss_percentage": "L2 Unified TLB Miss Percentage",
    "l2_tlb_mpki": "L2 Unified TLB MPKI",

    # ITLB Effectiveness
    "itlb_mpki": "ITLB MPKI",
    "itlb_walk_ratio": "ITLB Walk Ratio",
    "itlb_walk_percentage": "ITLB Walk Percentage",
    "l1i_tlb_miss_ratio": "L1 Instruction TLB Miss Ratio",
    "l1i_tlb_miss_percentage": "L1 Instruction TLB Miss Percentage",
    "l1i_tlb_mpki": "L1 Instruction TLB MPKI",

    # L1D Cache Effectiveness
    "l1d_cache_miss_ratio": "L1D Cache Miss Ratio",
    "l1d_cache_miss_percentage": "L1D Cache Miss Percentage",
    "l1d_cache_mpki": "L1D Cache MPKI",

    # L1I Cache Effectiveness
    "l1i_cache_miss_ratio": "L1I Cache Miss Ratio",
    "l1i_cache_miss_percentage": "L1I Cache Miss Percentage",
    "l1i_cache_mpki": "L1I Cache MPKI",

    # L2 Cache Effectiveness
    "l2_cache_miss_ratio": "L2 Cache Miss Ratio",
    "l2_cache_miss_percentage": "L2 Cache Miss Percentage",
    "l2_cache_mpki": "L2 Cache MPKI",

    # LL Cache Effectiveness
    "ll_cache_read_hit_ratio": "LL Cache Read Hit Ratio",
    "ll_cache_read_hit_percentage": "LL Cache Read Hit Percentage",
    "ll_cache_read_miss_ratio": "LL Cache Read Miss Ratio",
    "ll_cache_read_miss_percentage": "LL Cache Read Miss Percentage",
    "ll_cache_read_mpki": "LL Cache Read MPKI",

    # Operation Mix
    "barrier_percentage": "Barrier Operations Percentage",
    "branch_percentage": "Branch Operations Percentage",
    "crypto_percentage": "Crypto Operations Percentage",
    "integer_dp_percentage": "Integer Operations Percentage",
    "load_percentage": "Load Operations Percentage",
    "scalar_fp_percentage": "Floating Point Operations Percentage",
    "simd_percentage": "Advanced SIMD Operations Percentage",
    "store_percentage": "Store Operations Percentage",
    "sve_all_percentage": "SVE Operations (Load/Store Inclusive) Percentage",

    # Topdown L1
    "backend_bound": "Backend Bound",
    "bad_speculation": "Bad Speculation",
    "frontend_bound": "Frontend Bound",
    "retiring": "Retiring",
}


def get_atperf_metric_name(topdown_tool_metric: str) -> str:
    """
    Convert a topdown_tool metric name to its corresponding atperf metric name.

    Args:
        topdown_tool_metric: The metric name from topdown_tool (snake_case)

    Returns:
        The corresponding atperf metric name (Title Case), or the original name
        if no mapping exists.
    """
    return TOPDOWN_TOOL_TO_ATPERF_MAPPING.get(topdown_tool_metric, topdown_tool_metric)


def normalize_metric_name(metric_name: str) -> str:
    """
    Normalize a metric name for comparison purposes.
    Converts to lowercase and removes special characters.

    Args:
        metric_name: The metric name to normalize

    Returns:
        Normalized metric name for case-insensitive comparison
    """
    return metric_name.lower().replace(" ", "_").replace("-", "_")
