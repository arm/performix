# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
===============================================================================
workload_compatibility.py

Purpose:
    Shared implementation for workload-compatibility checks (localhost and
    remote).  Both variants run an atperf recipe for every specified workload,
    verify the command exits with rc == 0, and produce a JSON report with
    pass/fail status, the command invoked, and any captured error output.

    Localhost- and remote-specific setup live in thin subclasses defined in
    localhost_workload_compatibility.py and remote_workload_compatibility.py.

Workflow:
    For each workload × run:
    1. Invoke: atperf recipe run <recipe> --workload "<workload>"
                --target <target> --deploy-tools
    2. If the command exits with rc == 0, the workload is marked PASS.
         Otherwise it is marked FAIL and stdout/stderr are captured.
    3. Produce a JSON report listing every workload, the command used,
       pass/fail status, and any error output.
    4. Write companion metadata (system info, quality distribution, errors).

Assumptions / Requirements:
    - atperf installed and accessible.
    - Workloads are safe / idempotent to run multiple times.

See Also:
    workload_compatibility/localhost_workload_compatibility.py
    workload_compatibility/remote_workload_compatibility.py
===============================================================================
"""

import logging
import subprocess
from dataclasses import dataclass, field
from pathlib import Path
from typing import List

from benchmarks.benchmark import Benchmark, BenchmarkResult
from framework.args import add_common_benchmark_args
from framework.constants import (
    ATPERF_DEPLOY_TOOLS_FLAG,
    ATPERF_TARGET_FLAG,
    ATPERF_WORKLOAD_FLAG,
    JSON_EXTENSION,
    LOCALHOST_TARGET,
    REPORTS,
)
from framework.metadata import BenchmarkMetadata
from framework.quality_level import QualityLevel
from framework.runner import invoke_atperf, kill_atperf_engine, atperf_prepare_target
from framework.utils import consolidate_column_quality


@dataclass
class WorkloadRunResult:
    """Result of a single workload run."""
    workload: str
    command: str
    passed: bool
    stdout: str = ""
    stderr: str = ""
    error_message: str = ""
    run_index: int = 0


@dataclass
class WorkloadSummary:
    """Aggregated results for one workload across all runs."""
    workload: str
    total_runs: int = 0
    passed_runs: int = 0
    failed_runs: int = 0
    runs: List[WorkloadRunResult] = field(default_factory=list)

    @property
    def all_passed(self) -> bool:
        return self.failed_runs == 0 and self.total_runs > 0


def run_workload(
    workload: str,
    recipe: str,
    target: str,
    atperf_path: str,
    run_index: int = 0,
) -> WorkloadRunResult:
    """
    Execute a single atperf recipe run and return a pass/fail result.

    A non-zero exit code or an unhandled exception is treated as a failure.
    """
    args = (
        f"recipe run {recipe} "
        f"{ATPERF_WORKLOAD_FLAG} \"{workload}\" "
        f"{ATPERF_TARGET_FLAG} {target} "
        f"{ATPERF_DEPLOY_TOOLS_FLAG}"
    )
    full_command = f"{atperf_path} {args}"
    logging.info(f"  Running: {full_command}")

    try:
        stdout = invoke_atperf(args, path=atperf_path)
        return WorkloadRunResult(
            workload=workload,
            command=full_command,
            passed=True,
            stdout=stdout,
            run_index=run_index,
        )
    except subprocess.CalledProcessError as e:
        return WorkloadRunResult(
            workload=workload,
            command=full_command,
            passed=False,
            stdout=getattr(e, "output", "") or "",
            stderr=getattr(e, "stderr", "") or "",
            error_message=f"atperf exited with return code {e.returncode}",
            run_index=run_index,
        )
    except Exception as e:
        return WorkloadRunResult(
            workload=workload,
            command=full_command,
            passed=False,
            error_message=f"Unexpected error ({type(e).__name__}): {e}",
            run_index=run_index,
        )


class WorkloadCompatibilityBenchmark(Benchmark):
    """
    Base class for localhost and remote workload-compatibility benchmarks.

    Subclasses must set ``name`` and ``description`` and override
    ``_prepare_environment`` to perform target-specific setup.  They may also
    override ``add_arguments`` to expose a ``--target`` flag (remote) or omit
    it (localhost).
    """

    name = "workload_compatibility"
    description = "Base workload compatibility benchmark"

    def __init__(self):
        super().__init__()
        self._summaries: dict[str, WorkloadSummary] = {}

    def _prepare_environment(self, atperf_path: str, target: str) -> None:
        """
        Perform target-specific environment preparation.

        Default implementation kills any running atperf engine and prepares
        the target. May be extended by subclasses to add localhost (e.g.
        sysctl) or remote (e.g. agent connection) setup steps.
        """
        kill_atperf_engine(atperf_path)
        atperf_prepare_target(target, atperf_path=atperf_path)

    def _get_target(self, args) -> str:
        """
        Return the target string.
        
        Default implementation returns localhost. May be overridden by
        subclasses to support remote targets.
        """
        return LOCALHOST_TARGET


    @staticmethod
    def add_arguments(parser):
        add_common_benchmark_args(parser)
        parser.add_argument(
            "--recipe",
            type=str,
            required=True,
            help="Specify the atperf recipe to use",
        )


    def run(self, args) -> bool:
        atperf_path: str = args.atperf_path
        n_runs: int = int(args.n_runs)
        recipe: str = args.recipe
        target: str = self._get_target(args)
        workloads: list = args.workloads or []

        if not workloads:
            logging.error("No workloads specified. Provide at least one -w WORKLOAD.")
            return False

        self._prepare_environment(atperf_path, target)

        all_passed = True

        for workload in workloads:
            summary = WorkloadSummary(workload=workload)
            logging.info(f"Testing workload: {workload}")

            for i in range(n_runs):
                logging.info(f"  Run {i + 1}/{n_runs}")
                result = run_workload(
                    workload=workload,
                    recipe=recipe,
                    target=target,
                    atperf_path=atperf_path,
                    run_index=i,
                )

                summary.total_runs += 1
                if result.passed:
                    summary.passed_runs += 1
                    logging.info("    PASS")
                else:
                    summary.failed_runs += 1
                    all_passed = False
                    logging.error(f"    FAIL: {result.error_message}")
                    if result.stdout:
                        logging.debug(f"    stdout: {result.stdout[:500]}")
                    if result.stderr:
                        logging.debug(f"    stderr: {result.stderr[:500]}")

                summary.runs.append(result)

            self._summaries[workload] = summary

        return all_passed


    def generate_report(self) -> BenchmarkResult:
        """Build a BenchmarkResult table from collected run summaries."""
        headers = [
            "workload",
            "command",
            "total_runs",
            "passed_runs",
            "failed_runs",
            "status",
            "quality",
            "error_message",
            "stdout",
        ]

        rows: List[list] = []
        for workload, summary in self._summaries.items():
            first_failure = next((r for r in summary.runs if not r.passed), None)
            error_msg = first_failure.error_message if first_failure else ""
            stdout_snippet = first_failure.stdout if first_failure else ""
            command = summary.runs[0].command if summary.runs else ""

            status = "PASS" if summary.all_passed else "FAIL"
            quality = QualityLevel.GOOD.value if summary.all_passed else QualityLevel.POOR.value

            rows.append([
                workload,
                command,
                str(summary.total_runs),
                str(summary.passed_runs),
                str(summary.failed_runs),
                status,
                quality,
                error_msg,
                stdout_snippet,
            ])

        return BenchmarkResult(headers=headers, rows=rows)

    def report(self, args) -> BenchmarkMetadata:
        if not self._summaries:
            error_msg = (
                "No benchmark results available. "
                "Run phase must complete successfully before generating report."
            )
            logging.error(error_msg)
            self.metadata.add_error(error_msg)
            return self.metadata

        report_result = self.generate_report()
        logging.info(f"Generated report with {len(report_result.rows)} workload results")

        reports_dir = Path(REPORTS)
        reports_dir.mkdir(parents=True, exist_ok=True)
        json_file = reports_dir / f"{self.name}_report{JSON_EXTENSION}"
        report_result.to_json(json_file)
        logging.info(f"Report saved to: {json_file}")

        dist = consolidate_column_quality(report_result, quality_column_name="quality")
        self.metadata.set_quality_distribution(dist)

        return self.metadata
