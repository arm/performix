# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
remote_workload_compatibility.py

Remote-target variant of the workload-compatibility check.
Inherits all logic from WorkloadCompatibilityBenchmark and adds
remote-specific environment preparation (agent connection) and a
``--target`` CLI argument.
"""

from framework.args import add_common_benchmark_args
from framework.runner import kill_atperf_engine, atperf_prepare_target, create_agent_connection
from workload_compatibility.workload_compatibility import WorkloadCompatibilityBenchmark


class RemoteWorkloadCompatibilityBenchmark(WorkloadCompatibilityBenchmark):
    name = "remote_workload_compatibility"
    description = "Verify atperf recipe execution against a remote target succeeds for each workload"
    long_description = (
        "Runs a chosen atperf recipe for every specified workload against a remote target, "
        "checking that the command completes with exit code 0.  Produces a JSON report with "
        "pass/fail status per workload, the command invoked, and any captured error output.  "
        "Prereqs: atperf installed on host, remote target reachable and prepared."
    )

    @staticmethod
    def add_arguments(parser):
        add_common_benchmark_args(parser)
        parser.add_argument(
            "--recipe",
            type=str,
            required=True,
            help="Specify the atperf recipe to use",
        )
        parser.add_argument(
            "-t", "--target",
            type=str,
            required=True,
            help="Specify the remote target to connect to",
        )

    def _get_target(self, args) -> str:
        return args.target

    def _prepare_environment(self, atperf_path: str, target: str) -> None:
        kill_atperf_engine(atperf_path)
        atperf_prepare_target(target, atperf_path=atperf_path)
        create_agent_connection(target, atperf_path)
