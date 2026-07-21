#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
===============================================================================
atperf-eval.py

See Also:
    benchmarks/cpu_microarchitecture_accuracy_benchmark.py
    benchmarks/engine_overhead.py
    benchmarks/code_hotspots_accuracy_benchmark.py

Description:
    Entry point for the atperf-evaluation suite.
    Run this script on a chosen target machine.
    Ensure all requirements documented in benchmarks listed above are met.
    Additionally, we assume Git is installed.
    This script does the following:
    1. Parses command-line arguments to select and configure benchmarks
    2. Collects metadata about the target system and workloads requested
    3. Invokes the selected benchmark
===============================================================================
"""
# Standard library imports
import argparse
import logging
import shutil
import subprocess
import sys
import traceback

# Benchmark imports
from benchmarks.overhead.collector_overhead import CollectorOverheadBenchmark
from benchmarks.overhead.engine_overhead import AtperfOverheadBenchmark
from benchmarks.accuracy.code_hotspots_accuracy_benchmark import \
    CodeHotspotsAccuracyBenchmark
from benchmarks.accuracy.cpu_microarchitecture_accuracy_benchmark import CPUMicroarchitectureAccuracyBenchmark
from workload_compatibility.remote_workload_compatibility import RemoteWorkloadCompatibilityBenchmark
from workload_compatibility.localhost_workload_compatibility import LocalhostWorkloadCompatibilityBenchmark

# Local framework imports
from framework.constants import *
from framework.quality_level import indeterminable_distribution
from framework.runner import clean_environment, run_sysreport

BENCHMARK_REGISTRY = {
    CPUMicroarchitectureAccuracyBenchmark.name: CPUMicroarchitectureAccuracyBenchmark,
    AtperfOverheadBenchmark.name: AtperfOverheadBenchmark,
    CollectorOverheadBenchmark.name: CollectorOverheadBenchmark,
    CodeHotspotsAccuracyBenchmark.name: CodeHotspotsAccuracyBenchmark,
    RemoteWorkloadCompatibilityBenchmark.name: RemoteWorkloadCompatibilityBenchmark,
    LocalhostWorkloadCompatibilityBenchmark.name: LocalhostWorkloadCompatibilityBenchmark,
}

def main():
    global_parser = argparse.ArgumentParser(add_help=False)
    global_parser.add_argument('-v', '--verbose', action='store_true')

    parser = argparse.ArgumentParser(description="atperf-eval: Benchmark suite", parents=[global_parser])
    subparsers = parser.add_subparsers(title="Benchmarks", metavar=None, dest="benchmark")

    # Add subparsers for each benchmark
    for bench_cls in BENCHMARK_REGISTRY.values():
        sub = subparsers.add_parser(name=bench_cls.name,
                                    description=bench_cls.description,
                                    parents=[global_parser],
                                    add_help=True, help=bench_cls.description)
        bench_cls.add_arguments(sub)

    args = parser.parse_args()
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="[%(levelname)s] %(message)s"
    )

    try:
        if args.list_benchmarks:
            print("Available benchmarks:")
            for name, cls in BENCHMARK_REGISTRY.items():
                print(f" - {name}: {cls.description}")
            sys.exit(0)
    except AttributeError:
        pass

    clean_environment()

    if args.benchmark in BENCHMARK_REGISTRY:
        benchmark_cls = BENCHMARK_REGISTRY[args.benchmark]

        # Create instance of the benchmark
        benchmark = benchmark_cls()

        exit_code = 0

        # Collect system and workload metadata
        try:
            benchmark.metadata.benchmark_name = getattr(benchmark, "name", benchmark.metadata.benchmark_name)
            benchmark.metadata.n_runs = int(getattr(args, "n_runs", 0) or 0)
            benchmark.metadata.benchmark_args = vars(args)

            benchmark.metadata.collect_system_metadata()
            benchmark.metadata.add_workloads_metadata(args.workloads or [])
            benchmark.metadata.set_atperf_version(
                getattr(args, "atperf_path", "atperf"),
                getattr(args, "atperf_git_sha", ""),
                getattr(args, "atperf_git_ref", ""),
            )
            run_sysreport()

            logging.info("System and workload metadata collected")
        except Exception:
            # CI must show a traceback and ultimately fail.
            exit_code = 1
            logging.exception("Failed to collect system/workload metadata")
            try:
                benchmark.metadata.add_error(f"Failed to collect system/workload metadata: {traceback.format_exc()}")
            except Exception:
                # Don't mask the original error; traceback already printed above.
                pass

        # Run Benchmark
        try:
            success = benchmark.run(args)
            if success:
                logging.info("Benchmark completed successfully.")
            else:
                exit_code = 1
                logging.error("Benchmark completed with failures.")
        except subprocess.CalledProcessError as e:
            exit_code = 1
            # Include traceback + stdout/stderr context for CI.
            logging.exception(
                "%s failed with return code %s.\nstdout:\n%s\nstderr:\n%s",
                getattr(e, "cmd", "<unknown-cmd>"),
                getattr(e, "returncode", "<unknown-rc>"),
                getattr(e, "output", None),
                getattr(e, "stderr", None),
            )
            try:
                benchmark.metadata.add_error(f"Subprocess failed: {e.cmd}")
            except Exception:
                pass
        except KeyboardInterrupt:
            logging.error("\nBenchmark interrupted by user.")
            try:
                benchmark.metadata.add_error("Benchmark interrupted by user")
            except Exception:
                pass
            sys.exit(130)
        except Exception as e:
            exit_code = 1
            logging.error("Benchmark failed with unexpected error:")
            logging.exception(e)
            try:
                benchmark.metadata.add_error(f"Benchmark failed with unexpected error: {traceback.format_exc()}")
                benchmark.metadata.set_quality_distribution(indeterminable_distribution())
            except Exception:
                pass

        # Generate Report
        try:
            logging.info("Generating benchmark report...")
            metadata = benchmark.report(args)

            if metadata is not None:
                metadata_file = OUTPUT_DIR / "metadata.json"
                metadata.save_to_file(str(metadata_file))
            else:
                logging.warning("No metadata generated for the benchmark.")

        except Exception:
            # Must fail CI and must show traceback.
            exit_code = 1
            logging.exception("Failed to generate report")
            sys.exit(exit_code)

        # Cleanup log/data files if not verbose (leave only report & metadata)
        if not args.verbose:
            logging.info("Cleaning up reports and logs...")
            try:
                shutil.rmtree(Path(RESULTS))
                shutil.rmtree(Path(LOGS))
            except Exception:
                pass

        logging.info("Benchmark complete. Results written to output/reports")

        sys.exit(exit_code)
    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
