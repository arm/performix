# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
===============================================================================
collector_overhead.py

Purpose:
    Measures the additional amount of time a workload takes to complete when running under sl-record
    as compared to running on its own. Overhead is split into startup, workload, and shutdown phases.
    Can be applied to one or more workloads, across repeated runs. Produces a JSON report detailing
    timing information, and aggregating results from multiple runs together. Runs with localhost.

Timing Categories:
    STARTUP, WORKLOAD, SHUTDOWN

Workflow:
    For each workload:
      1. Run workload on its own to establish baseline
      For n runs:
        2. Invoke sl-record on workload
        3. Parse timings from [FINE]-level sl-record logging
        4. Compute startup, workload and shutdown timings
    5. Reformat data and compute statistics
    6. Write JSON

Requirements:
    - Workloads, sl-record are installed on local machine. atperf is installed on local machine, unless providing custom
        sl-record args
    - Workloads are safe/idempotent to run multiple times

===============================================================================
"""
import json
import re
import statistics
from dataclasses import dataclass, fields
from typing import Any

from benchmarks.benchmark import Benchmark, BenchmarkResult
from framework.args import add_common_benchmark_args
from framework.metadata import BenchmarkMetadata
from framework.runner import *
from framework.quality_level import indeterminable_distribution

collector_overhead_BENCHMARK_NAME = "collector_overhead"
WORKLOAD_START_TAG = "WORKLOAD_START"
WORKLOAD_END_TAG = "WORKLOAD_END"
OUTPUT_DIR = Path("/tmp/collector_overhead_benchmark")

PERF_TOTAL_MS = "perf_total_ms_mean"
SL_RECORD_TOTAL_MS = "sl-record_total_ms_mean"
SL_RECORD_MS_OVER_PERF_KEY = "sl-record_ms_over_perf"
SL_RECORD_PERCENT_OVER_PERF_KEY = "sl-record_percent_over_perf"

@dataclass
class CollectorTimings:
    """
    Represents timing measurements for a single invocation of a collector (sl-record or perf).

    Attributes
    ----------
    startup_ms : float
        Duration from (starting collector command) -> (collector starting workload), in milliseconds.
    workload_ms : float
        Duration from (collector starting workload) -> (workload exiting), in milliseconds.
    shutdown_ms : float
        Duration from (workload exiting) -> (collector command exiting), in milliseconds.
    total_ms : float
        Duration from (starting collector command) -> (collector command exiting), in milliseconds.
    """
    startup_ms: float
    workload_ms: float
    shutdown_ms: float
    total_ms: float


@dataclass
class CollectorOverhead:
    """
    Represents computed overhead measurements for a single invocation of a collector (sl-record or perf).

    Attributes
    ----------
    workload_overhead_ms : float
        Difference between (workload duration under collector) and (workload duration on its own), in milliseconds.
    workload_overhead_percent : float
        (workload_overhead_ms) / (workload duration on its own)
    net_overhead_ms : float
        Difference between (collector total duration) and (workload duration on its own), in milliseconds.
    net_overhead_percent : float
        (net_overhead_ms) / (workload duration on its own)
    """
    workload_overhead_ms: float
    workload_overhead_percent: float
    net_overhead_ms: float
    net_overhead_percent: float


@dataclass
class WorkloadMeasurements:
    """
    Represents the complete set of timing measurements for a workload.

    Attributes
    ----------
    raw_workload_time_ms : float
        Time taken for the workload to run on its own, in milliseconds.
    sl_record_measurements : list[tuple[CollectorTimings, CollectorOverhead]]
        Timing measurements for each run of sl-record.
    perf_measurements : list[tuple[CollectorTimings, CollectorOverhead]]
        Timing measurements for each run of perf.
    """
    raw_workload_time_ms: float
    sl_record_measurements: list[tuple[CollectorTimings, CollectorOverhead]]
    perf_measurements: list[tuple[CollectorTimings, CollectorOverhead]]


class CollectorOverheadBenchmark(Benchmark):
    """
    A collector overhead benchmark instance.

    Attributes
    ----------
    n_runs : int
        The number of times to measure sl-record timings for each workload.
    workloads : list[str]
        A list of workloads to run.
    atperf_path : str
        The path to the atperf binary to use (needed for determining metrics groups to collect).
    benchmark_sl_record : bool
        Whether to benchmark sl-record overhead.
    benchmark_perf : bool
        Whether to benchmark perf overhead.
    sl_record_path : str
        The path to the sl-record binary to use.
    sl_record_args : str
        The args to provide to sl-record. Mandatory args (-o, -d, -A) are applied separately.
    perf_args : str
        The args to provide to perf.
    results : dict[WorkloadMeasurements]
        A dictionary of WorkloadTimings, one for each workload.
    """
    name = collector_overhead_BENCHMARK_NAME
    description = "Benchmark to measure collector overhead and timing breakdown"
    long_description = (
        "Measures the additional amount of time a workload takes to complete when running under "
        "sl-record, as compared to running on its own. Overhead is split into startup, workload, "
        "and shutdown phases. Can be applied to one or more workloads, across repeated runs. "
        "Produces a JSON report detailing timing information, and aggregating results from "
        "multiple runs together. Runs with localhost. "
        "Prereqs: sl-record, perf and workload installed on local machine. atperf installed on "
        "local machine unless custom sl-record args are provided. "
        "***** Any running sl-record processes will be killed *****"
    )

    def __init__(self):
        super().__init__()
        self.n_runs = None
        self.workloads = None
        self.benchmark_sl_record = None
        self.benchmark_perf = None
        self.atperf_path = None
        self.sl_record_path = None
        self.sl_record_args = None
        self.perf_args = None
        self.results = None

    @staticmethod
    def add_arguments(parser):
        add_common_benchmark_args(parser)
        parser.add_argument("--collector", type=str, choices=['sl-record','perf','both'],
                            help="Specify which collectors to benchmark (default='both').", default='both')
        parser.add_argument("--sl-record-path", type=str,
                            help="Specify the path to the sl-record binary")
        parser.add_argument("--sl-record-args", type=str,
                            help="Specify custom args to provide to sl-record. By default, the benchmark will use "
                                 "the same args as those used by the ATP cpu_microarchitecture recipe.")
        parser.add_argument("--perf-args", type=str,
                                help="Specify custom args to provide to perf. By default, the benchmark will use "
                                 "the same args as those used by the ATP cpu_microarchitecture recipe.")

    def consolidate_args(self, args):
        self.n_runs = int(args.n_runs)
        self.workloads = args.workloads
        self.atperf_path = args.atperf_path

        self.benchmark_sl_record = args.collector == "sl-record" or args.collector == "both"
        self.benchmark_perf = args.collector == "perf" or args.collector == "both"
        self.sl_record_path = args.sl_record_path
        self.sl_record_args = args.sl_record_args
        self.perf_args = args.perf_args

        if not self.workloads:
            raise Exception("No workloads specified. Provide at least one -w WORKLOAD.")

        if self.benchmark_sl_record:
            if not self.sl_record_path:
                raise Exception("Cannot benchmark sl-record as the path to the sl-record binary was not provided. Specify this path using the '--sl-record-path' flag.")

            if not self.sl_record_args:
                logging.debug("No sl-record args provided, defaulting to atperf cpu_microarchitecture sl-record args.")

                # Prepare target
                atperf_prepare_target("localhost", self.atperf_path)
                # Discover metric groups once
                metric_groups = get_metric_groups(self.atperf_path)

                args = f"{SL_RECORD_TIMEOUT_FLAG} 0 {SL_RECORD_METRICS_GROUPS_FLAG} {','.join(metric_groups)} {SL_RECORD_SAMPLE_RATE_FLAG} normal"
                logging.debug(f"Default sl-record args: {args}")
                self.sl_record_args = args
            else:
                logging.debug(f"Custom sl-record args provided ({self.sl_record_args}), using these.")

        if self.benchmark_perf:
            if not self.perf_args:
                logging.debug("No perf args provided, using default args.")
                try:
                    run_cmd(f"lscpu | grep Neoverse-V2")
                    self.perf_args = PERF_DEFAULT_ARGS_NEOVERSE_V2
                    logging.debug(f"Default perf args: {self.perf_args}")
                except subprocess.CalledProcessError:
                    logging.warning("Cannot use default perf args as system is not Neoverse-V2. Perf overhead will not be measured. Specify perf args manually using the '--perf-args' flag.")
                    self.benchmark_perf = False
                    if not self.benchmark_sl_record:
                        raise Exception("No collector to benchmark")
            else:
                logging.debug(f"Custom perf args provided ({self.perf_args}), using these.")

    @staticmethod
    def _time_raw_workload(workload: str) -> float:
        """Times how long it takes for the provided workload to execute, in milliseconds. This is determined by
        checking time.time_ns() immediately before and after running the workload, and calculating the difference."""
        _, workload_start_ms, workload_end_ms = time_cmd(workload)

        duration = workload_end_ms - workload_start_ms
        logging.info(f"Raw workload time: {duration}ms")

        return duration

    @staticmethod
    def _clean_up_capture(capture_file_path: Path):
        if not capture_file_path.exists():
            logging.warning(f"No capture output found at {capture_file_path}")
            return

        resolved_capture = capture_file_path.resolve()
        if not resolved_capture.is_relative_to(OUTPUT_DIR):
            raise Exception(f"Capture file path {capture_file_path} is not within benchmark output dir {OUTPUT_DIR}")

        if resolved_capture.is_dir():
            shutil.rmtree(capture_file_path)
        else:
            resolved_capture.unlink()

    @staticmethod
    def _wrap_workload(workload: str) -> str:
        """Wraps the specified workload in a bash script which outputs the workload start and end times (in nanoseconds
        since the epoch) to stdout."""
        wrapped_workload = (f"bash -c '"
                            f"echo \"{WORKLOAD_START_TAG}=$(date +%s%N)\";"
                            f"{workload};"
                            f"echo \"{WORKLOAD_END_TAG}=$(date +%s%N)\";"
                            f"'")
        return wrapped_workload

    @staticmethod
    def _collect_sl_record_overhead(workload: str, sl_record_path: str, sl_record_args: str, n_runs: int,
                                    raw_workload_time_ms: float) -> list[
        tuple[CollectorTimings, CollectorOverhead]]:
        """
        Collects timing measurements for sl-record profiling the specified workload.

        Returns
        -------
        list[tuple[CollectorTimings, CollectorOverhead]]
            An array of measurements for each invocation of sl-record.
        """
        apc_dir = OUTPUT_DIR / Path("sl-record")
        apc_file_name = Path("capture.apc")
        apc_file_path = apc_dir / apc_file_name

        wrapped_workload = CollectorOverheadBenchmark._wrap_workload(workload)

        args = f"{SL_RECORD_OUTPUT_FLAG} {apc_file_path} {sl_record_args} {SL_RECORD_WORKLOAD_FLAG} {wrapped_workload}"
        results = []

        # Ensure apc dir exists
        apc_dir.mkdir(parents=True, exist_ok=True)

        for i in range(n_runs):
            # Make sure capture apc doesn't exist
            CollectorOverheadBenchmark._clean_up_capture(apc_file_path)

            # Ensure there are no existing sl-record processes hanging around
            kill_sl_record()

            logging.info(f"Timing sl-record invocation #{i + 1}...")

            stdout, sl_record_start_ms, sl_record_end_ms = time_sl_record(args, sl_record_path)

            logging.info(f"Sl-record invocation #{i + 1} complete. Duration: {sl_record_end_ms - sl_record_start_ms}ms")

            sl_record_timings = CollectorOverheadBenchmark._compute_collector_timings(sl_record_start_ms,
                                                                                      sl_record_end_ms,
                                                                                      stdout)
            sl_record_overhead = CollectorOverheadBenchmark._compute_collector_overhead(raw_workload_time_ms,
                                                                                        sl_record_timings)

            results.append((sl_record_timings, sl_record_overhead))
        # Don't leave capture apc around
        CollectorOverheadBenchmark._clean_up_capture(apc_file_path)

        return results

    @staticmethod
    def _collect_perf_overhead(workload: str, perf_args: str, n_runs: int, raw_workload_time_ms: float) -> list[
        tuple[CollectorTimings, CollectorOverhead]]:
        """
        Collects timing measurements for perf profiling the specified workload.

        Returns
        -------
        list[tuple[CollectorTimings, CollectorOverhead]]
            An array of measurements for each invocation of perf.
        """
        capture_dir = OUTPUT_DIR / Path("perf")
        capture_file_name = Path("perf.data")
        capture_file_path = capture_dir / capture_file_name

        wrapped_workload = CollectorOverheadBenchmark._wrap_workload(workload)

        args = f"record {PERF_OUTPUT_FLAG} {capture_file_path} {perf_args} -- {wrapped_workload}"
        results = []

        # Ensure capture dir exists
        capture_dir.mkdir(parents=True, exist_ok=True)

        for i in range(n_runs):
            # Make sure capture file doesn't exist
            CollectorOverheadBenchmark._clean_up_capture(capture_file_path)

            logging.info(f"Timing perf invocation #{i + 1}...")

            stdout, perf_start_ms, perf_end_ms = time_perf(args)

            logging.info(f"Perf invocation #{i + 1} complete. Duration: {perf_end_ms - perf_start_ms}ms")

            perf_timings = CollectorOverheadBenchmark._compute_collector_timings(perf_start_ms,
                                                                                      perf_end_ms,
                                                                                      stdout)
            perf_overhead = CollectorOverheadBenchmark._compute_collector_overhead(raw_workload_time_ms,
                                                                                        perf_timings)

            results.append((perf_timings, perf_overhead))
        # Don't leave capture file around
        CollectorOverheadBenchmark._clean_up_capture(capture_file_path)

        return results

    @staticmethod
    def _compute_collector_timings(start_ms: float, end_ms: float,
                                   stdout: str) -> CollectorTimings:
        """
        Takes the start and end time of an invocation of sl-record or perf, along with its stdout, and
        computes the startup, workload and shutdown timings.

        Parameters
        ----------
        start_ms : float
            The time at which the collector command was started, in milliseconds since the epoch.
        end_ms : float
            The time at which the collector command exited, in milliseconds since the epoch.
        stdout : str
            The stdout output of the collector.
        Returns
        -------
        CollectorTimings
            The computed timing measurements for this invocation of sl-record or perf.
        """
        WORKLOAD_START_REGEX = re.compile(rf'^{WORKLOAD_START_TAG}=(\d+)$', re.MULTILINE)
        WORKLOAD_END_REGEX = re.compile(rf'^{WORKLOAD_END_TAG}=(\d+)$', re.MULTILINE)

        start_match = WORKLOAD_START_REGEX.search(stdout)
        if start_match is None:
            raise Exception(f"couldn't identify workload start from stdout ({stdout})")

        logging.debug(f"Found match for workload start regex: \"{start_match.group(0)}\"")
        workload_start_ms = float(start_match.group(1)) / 1e6

        end_match = WORKLOAD_END_REGEX.search(stdout)
        if end_match is None:
            raise Exception(f"couldn't identify workload end from stdout ({stdout})")

        logging.debug(f"Found match for workload end regex: \"{end_match.group(0)}\"")
        workload_end_ms = float(end_match.group(1)) / 1e6

        logging.debug(
            f"collector_start = 0ms; workload_start = {workload_start_ms-start_ms}ms; workload_end = {workload_end_ms-start_ms}ms; collector_end = {end_ms-start_ms}ms")

        startup_ms = workload_start_ms - start_ms
        workload_ms = workload_end_ms - workload_start_ms
        shutdown_ms = end_ms - workload_end_ms
        total_ms = end_ms - start_ms

        return CollectorTimings(startup_ms, workload_ms, shutdown_ms, total_ms)

    @staticmethod
    def _compute_collector_overhead(raw_workload_time_ms: float,
                                    collector_timings: CollectorTimings) -> CollectorOverhead:
        """
        Takes the timings for a particular invocation of sl-record or perf, as well as the raw workload time, and
        computes the overhead.

        Parameters
        ----------
        raw_workload_time_ms : float
            Time taken for the workload to run on its own, in milliseconds.
        collector_timings : CollectorTimings
            Timing measurements for a single invocation of sl-record or perf.

        Returns
        -------
        CollectorOverhead
            Computed overhead timings for the collector invocation.
        """
        workload_overhead_ms = collector_timings.workload_ms - raw_workload_time_ms
        workload_overhead_percent = (workload_overhead_ms / raw_workload_time_ms) * 100

        net_overhead_ms = collector_timings.total_ms - raw_workload_time_ms
        net_overhead_percent = (net_overhead_ms / raw_workload_time_ms) * 100
        return CollectorOverhead(workload_overhead_ms, workload_overhead_percent, net_overhead_ms,
                                 net_overhead_percent)

    def run(self, args):
        """Run benchmark to collect collector overhead metrics across workloads."""
        logging.debug(f"CollectorOverheadBenchmark.run called with args {args}")
        self.consolidate_args(args)

        # Data structures for tracking results
        results = {}

        # Process each workload
        for workload in self.workloads:
            # Time raw workload
            logging.info("Timing raw workload...")
            raw_workload_time_ms = CollectorOverheadBenchmark._time_raw_workload(workload)
            logging.info("Raw workload measured.")

            if self.benchmark_sl_record:
                # Time sl-record
                sl_record_measurements = CollectorOverheadBenchmark._collect_sl_record_overhead(workload, self.sl_record_path,
                                                                                 self.sl_record_args, self.n_runs,
                                                                                 raw_workload_time_ms)
            else:
                sl_record_measurements = []

            if self.benchmark_perf:
                # Time perf
                perf_measurements = CollectorOverheadBenchmark._collect_perf_overhead(workload, self.perf_args, self.n_runs,
                                                                                      raw_workload_time_ms)
            else:
                perf_measurements = []

            results[workload] = WorkloadMeasurements(raw_workload_time_ms, sl_record_measurements, perf_measurements)
        self.results = results
        return True

    def generate_summary_report(self) -> BenchmarkResult:
        """Summarises the data collected into a BenchmarkResult."""
        headers = [
            "workload",
            "raw_workload_time_ms",
        ]

        for m in fields(CollectorTimings) + fields(CollectorOverhead):
            if self.benchmark_perf:
                headers.append("perf_" + m.name + "_mean")
                headers.append("perf_" + m.name + "_stdev")
            if self.benchmark_sl_record:
                headers.append("sl-record_" + m.name + "_mean")
                headers.append("sl-record_" + m.name + "_stdev")
        if self.benchmark_perf and self.benchmark_sl_record:
            headers.append(SL_RECORD_MS_OVER_PERF_KEY)
            headers.append(SL_RECORD_PERCENT_OVER_PERF_KEY)

        # Note that data is deliberately structured as a list of dictionaries to be easily portable
        # to the format required by HyperGen for future migration.
        data = []
        for workload in self.workloads:
            # result is a WorkloadMeasurements object
            result = self.results[workload]
            entry = {
                "workload": workload,
                "raw_workload_time_ms": round(result.raw_workload_time_ms, 1)
            }

            if self.benchmark_perf:
                CollectorOverheadBenchmark.add_measurements(result.perf_measurements, "perf", entry)
            if self.benchmark_sl_record:
                CollectorOverheadBenchmark.add_measurements(result.sl_record_measurements, "sl-record", entry)
            if self.benchmark_perf and self.benchmark_sl_record:
                perf_total = entry[PERF_TOTAL_MS]
                sl_record_total = entry[SL_RECORD_TOTAL_MS]
                entry[SL_RECORD_MS_OVER_PERF_KEY] = sl_record_total - perf_total
                entry[SL_RECORD_PERCENT_OVER_PERF_KEY] = ((sl_record_total - perf_total) / perf_total) * 100

            data.append(entry)

        return BenchmarkResult(headers, [[e[h] for h in headers] for e in data])

    @staticmethod
    def add_measurements(measurements: list[tuple[CollectorTimings, CollectorOverhead]], collector_name: str, entry: dict[str, float]):
        """Modifies the provided 'entry' dict in-place, adding the mean and stddev of the values in 'measurements'
        for each field in CollectorTimings / CollectorOverhead."""
        # list of dicts from measurement name to value, one per run
        serialized = CollectorOverheadBenchmark.serialize_measurements(measurements)
        for measurement in serialized[0].keys():
            # list of value for measurement in each dict
            vals = [e[measurement] for e in serialized]
            entry[collector_name + "_" + measurement + "_mean"] = round(statistics.mean(vals), 1)
            entry[collector_name + "_" + measurement + "_stdev"] = round(statistics.stdev(vals), 1) if len(vals) > 1 else 0

    @staticmethod
    def serialize_measurements(measurements: list[tuple[CollectorTimings, CollectorOverhead]]) -> list[dict[str, float]]:
        """Takes the provided list of measurements, and serialises the CollectorTimings and CollectorOverhead structs
        into dicts"""
        result = []
        for run_measurements in measurements:
            measurements_entry = {}

            for m in fields(CollectorTimings):
                field_name = m.name
                val = getattr(run_measurements[0], field_name)
                measurements_entry[field_name] = round(val, 1)
            for m in fields(CollectorOverhead):
                field_name = m.name
                val = getattr(run_measurements[1], field_name)
                measurements_entry[field_name] = round(val, 1)

            result.append(measurements_entry)

        return result

    def generate_raw_data_report(self) -> list[dict[str, Any]]:
        """Produces a JSON-serializable report of the raw data collected."""
        data = []
        for workload in self.workloads:
            # result is a WorkloadMeasurements object
            result = self.results[workload]
            entry = {
                "workload": workload,
                "raw_workload_time_ms": round(result.raw_workload_time_ms, 1)
            }
            if self.benchmark_perf:
                entry["perf_measurements"] = CollectorOverheadBenchmark.serialize_measurements(result.perf_measurements)

            if self.benchmark_sl_record:
                entry["sl-record_measurements"] = CollectorOverheadBenchmark.serialize_measurements(result.sl_record_measurements)
            data.append(entry)

        return data

    def report(self, args) -> BenchmarkMetadata:
        # Check if we have results from run phase
        if self.results is None:
            error_msg = "No benchmark results available. Run phase must complete successfully before generating report."
            logging.error(error_msg)
            self.metadata.add_error(error_msg)
            return self.metadata

        reports_dir = Path(REPORTS)
        reports_dir.mkdir(parents=True, exist_ok=True)

        if args.verbose:
            logging.info("Verbose flag is set, generating raw data report")
            raw_data_report = self.generate_raw_data_report()
            logging.info(f"Generated raw data report with {len(raw_data_report)} workload results")
            json_file = reports_dir / f"collector_overhead_raw_data{JSON_EXTENSION}"

            with open(json_file, "w") as f:
                json.dump(raw_data_report, f, indent=2)

            logging.info(f"Raw data report saved to: {json_file}")

        report_result = self.generate_summary_report()
        logging.info(f"Generated summary report with {len(report_result)} workload results")
        json_file = reports_dir / f"collector_overhead_report{JSON_EXTENSION}"
        report_result.to_json(json_file)

        logging.info(f"Summary report saved to: {json_file}")

        # TODO: Produce more detailed quality analysis?
        # For now: No per-metric quality for this benchmark, so publish the default indeterminable distribution
        self.metadata.set_quality_distribution(indeterminable_distribution())

        return self.metadata
