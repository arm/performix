# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
localhost_workload_compatibility.py

Localhost variant of the workload-compatibility check.
Inherits all logic from WorkloadCompatibilityBenchmark and adds
localhost-specific environment preparation (perf_event_paranoid).
"""

import logging

from framework.constants import LOCALHOST_TARGET
from framework.runner import run_cmd, kill_atperf_engine, atperf_prepare_target
from workload_compatibility.workload_compatibility import WorkloadCompatibilityBenchmark


class LocalhostWorkloadCompatibilityBenchmark(WorkloadCompatibilityBenchmark):
    name = "localhost_workload_compatibility"
    description = "Verify atperf recipe execution on localhost succeeds for each workload"
    long_description = (
        "Runs a chosen atperf recipe for every specified workload on localhost, "
        "checking that the command completes with exit code 0.  Produces a JSON report with "
        "pass/fail status per workload, the command invoked, and any captured error output.  "
        "Prereqs: atperf and perf installed on the local machine."
    )

    def _prepare_environment(self, atperf_path: str, target: str) -> None:
        try:
            run_cmd("sudo sysctl -w kernel.perf_event_paranoid=-1")
        except Exception as e:
            logging.warning(f"Could not set perf_event_paranoid: {e}. Some recipes may require this.")

        kill_atperf_engine(atperf_path)
        atperf_prepare_target(LOCALHOST_TARGET, atperf_path=atperf_path)
