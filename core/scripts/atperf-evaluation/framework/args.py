# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
===============================================================================
args.py

Purpose:
    Define common command-line arguments shared across multiple benchmarks.
    This reduces duplication and ensures consistency in argument definitions.

===============================================================================
"""
from framework.constants import DEFAULT_ATPERF_PATH


def add_common_benchmark_args(parser):
    """
    Add common benchmark arguments that are shared across multiple benchmarks.

    Args:
        parser: ArgumentParser instance to add arguments to
    """
    parser.add_argument(
        '-r', '--n-runs',
        type=int,
        default=1,
        help='Number of runs for the benchmark (default: 1)'
    )
    parser.add_argument(
        '-a', '--atperf-path',
        type=str,
        default=DEFAULT_ATPERF_PATH,
        help=f'Path to the atperf binary to use (default: {DEFAULT_ATPERF_PATH})'
    )
    parser.add_argument(
        '-w', '--workload',
        dest='workloads',
        action='append',
        metavar='WORKLOAD',
        help='Workload command (repeatable, specify multiple -w)'
    )
    parser.add_argument(
        '--atperf-git-sha', 
        dest='atperf_git_sha',
        type=str,
        default="", 
        help="Git SHA of the source used to build the atperf binary"
    )
    parser.add_argument(
        '--atperf-git-ref', 
        dest='atperf_git_ref',
        type=str, 
        default="", 
        help="Git ref (branch/tag) of the source used to build the atperf binary"
    )
