# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
===============================================================================
engine_overhead.py

Purpose:
    Collect various apx CLI time, memory and disk space overhead metrics.

Measuring time overhead for recipe run:
    CONNECTION, TARGET_PREPARATION, FILE_TRANSMISSION, and either:
      - COLLECTION_ANALYSIS combined (sl-collect / non-agent mode), or
      - COLLECTION + ANALYSIS separately (agent mode)
    All timing values are in milliseconds (ms).

Render Metrics:
    RENDER_TIME_MS        - wall-clock time to render a single run
    RENDER_MEMORY_MB      - render engine in-memory database usage after rendering
    COMPARISON_RENDER_TIME_MS    - wall-clock time to render a two-run comparison
    COMPARISON_MEMORY_USAGE_MB   - render engine in-memory database usage for comparison

Workflow:
    1. Invoke: recipe run <recipe> per workload x run
    2. Parse lines '<desc> - done! [<time>]'
    3. Map descriptions to high-level categories (sl-collect map if APXD_ENABLE_AGENT env var is false)
    4. Optionally measure run artifact directory size (if run ID found)
    5. Render each run and measure render time and memory usage
    6. Average timings over runs and write JSON report

Assumptions / Requirements:
    - Benchmark is run from a HOST machine with atperf installed
    - atperf, perf installed and accessible
    - Workloads are safe/idempotent to run multiple times

Environment:
    APXD_ENABLE_AGENT=false switches to sl-collect timing category map
===============================================================================
"""

import platform
from enum import Enum, auto
from typing import Optional

from benchmarks.benchmark import Benchmark, BenchmarkResult
from framework.args import add_common_benchmark_args
from framework.metadata import BenchmarkMetadata
from framework.rag_thresholds import *
from framework.runner import *
from framework.utils import *
from framework.utils import consolidate_column_quality
from framework.quality_level import QualityLevel

enable_agent_env_var = f"{ENV_VAR_PREFIX}_ENABLE_AGENT"
USE_SL_COLLECT = os.environ.get(enable_agent_env_var, "").lower() == "false"

class TimingCategory(Enum):
    CONNECTION = auto()
    COLLECTION = auto()
    ANALYSIS = auto()
    TARGET_PREPARATION = auto()
    FILE_TRANSMISSION = auto()
    COLLECTION_ANALYSIS = auto()

_category_map_default = {
    "Establishing SSH connection": TimingCategory.CONNECTION,
    "Connecting to sl-collect daemon": TimingCategory.CONNECTION,
    "Identifying target architecture": TimingCategory.TARGET_PREPARATION,
    "Collecting target information (running processes)": TimingCategory.TARGET_PREPARATION,
    "Collecting target information": TimingCategory.TARGET_PREPARATION,
    "Preparing output directories": TimingCategory.TARGET_PREPARATION,
    "Checking workload": TimingCategory.TARGET_PREPARATION,
    "Running neoprof with CPU Microarchitecture": TimingCategory.COLLECTION_ANALYSIS,
    "Retrieving output files": TimingCategory.FILE_TRANSMISSION,
}

# Placeholder for agent-enabled category map; fill in as needed
_category_map_agent = {
    "Establishing SSH connection to target": TimingCategory.CONNECTION,
    "Connecting to target agent": TimingCategory.CONNECTION,
    "Identifying target architecture": TimingCategory.TARGET_PREPARATION,
    "Collecting target information (running processes)": TimingCategory.TARGET_PREPARATION,
    "Collecting target information": TimingCategory.TARGET_PREPARATION,
    "Preparing output directories": TimingCategory.TARGET_PREPARATION,
    "Checking workload": TimingCategory.TARGET_PREPARATION,
    "Collecting data": TimingCategory.COLLECTION,
    "Analyzing collection": TimingCategory.ANALYSIS,
    "Retrieving recipe artifacts": TimingCategory.FILE_TRANSMISSION,
}

def get_category_map():
    return _category_map_default if USE_SL_COLLECT else _category_map_agent

category_map = get_category_map()


class TimingCollection:
    def __init__(self):
        self.totals_ms = {cat: 0.0 for cat in TimingCategory}
        self.recipe_run_total_ms = 0.0  # milliseconds
        self.run_size_mb = 0.0  # MB
        self.render_time_ms = 0.0  # milliseconds
        self.render_memory_mb = 0.0  # MB (render engine in-memory database usage)
        self.comparison_render_time_ms = 0.0  # milliseconds
        self.comparison_memory_usage_mb = 0.0  # MB (render engine in-memory database usage)

    def add_time(self, category: TimingCategory, ms: float) -> None:
        """Add timing measurement to a specific category."""
        self.totals_ms[category] += ms
        self.recipe_run_total_ms += ms
        # Note: run_size is set separately, not here

    def add_render_metrics(self, render_metrics: dict) -> None:
        """Add rendering metrics to this run."""
        if render_metrics:
            self.render_time_ms = render_metrics.get("render_time_ms", 0.0)
            self.render_memory_mb = render_metrics.get("memory_usage_mb", 0.0) or 0.0

    def get_metric_value(self, metric: MetricType) -> float:
        """Get the value for any metric directly without mapping."""
        if hasattr(self, metric.value):
            return getattr(self, metric.value)
        elif metric.value in ['connection_ms', 'collection_ms', 'analysis_ms', 'target_preparation_ms', 'file_transmission_ms', 'collection_analysis_ms']:
            # For timing category metrics, strip _ms suffix to match TimingCategory enum name
            cat_name = metric.value.removesuffix("_ms")
            timing_cat = next((cat for cat in TimingCategory if cat.name.lower() == cat_name), None)
            return self.totals_ms.get(timing_cat, 0.0) if timing_cat else 0.0
        else:
            return 0.0

    def __str__(self):
        parts = []
        for cat in TimingCategory:
            label = cat.name.replace("_", " ").title()
            parts.append(f"{label}: {self.totals_ms[cat]:.3f} ms")
        parts.append(f"Total: {self.recipe_run_total_ms:.3f} ms")
        parts.append(f"Run Data Size: {self.run_size_mb:.3f} MB")
        parts.append(f"Render Time: {self.render_time_ms:.3f} ms")
        parts.append(f"Render Memory Usage: {self.render_memory_mb:.3f} MB")
        return "\n".join(parts)


def extract_run_timings(atperf_output: str) -> TimingCollection:
    """Extract timing information from atperf command output."""
    timings = TimingCollection()
    # Example line: "Establishing SSH connection - done! [0.123s]"
    pattern = re.compile(r'^(.*?) - done!\s*\[([^\]]+)\]', re.MULTILINE)

    for match in pattern.finditer(atperf_output):
        desc, time_str = match.groups()
        # Find matching category based on description
        category = None
        for key, cat in category_map.items():
            if key in desc:
                category = cat
                break

        if not category:
            continue

        try:
            ms = parse_time(time_str)
            timings.add_time(category, ms)
        except Exception as e:
            logging.warning(f"Skipping line (can't parse time): {desc} [{time_str}] ({e})")

    return timings


def extract_render_session_memory_usage(session_id: str, json_str: str) -> Optional[float]:
    """Extract memory usage in GiB for a render session from the render list JSON response.

    The session's db_key is used to look up memory_usage_gib from the db_instances list,
    since that field is not present on the session object itself.
    """
    try:
        data = json.loads(json_str)
        data_block = data.get("data", {})
        sessions = data_block.get("sessions", [])
        db_instances = data_block.get("db_instances", [])

        db_key = next(
            (s.get("db_key") for s in sessions if s.get("session_id") == session_id),
            None
        )
        if db_key is None:
            return None

        return next(
            (db.get("memory_usage_gib") for db in db_instances if db.get("db_key") == db_key),
            None
        )
    except (json.JSONDecodeError, AttributeError, TypeError):
        return None


def collect_engine_overhead(workload: str, recipe: str, target: str, atperf_path: str) -> str:
    """Run atperf collection for a specific workload."""
    logging.info("Collecting atperf timings...")
    args = f"recipe run {recipe} {ATPERF_WORKLOAD_FLAG} \"{workload}\" {ATPERF_TARGET_FLAG} {target} {ATPERF_DEPLOY_TOOLS_FLAG}"
    result = invoke_atperf(args, path=atperf_path)
    return result



def gather_render_metrics(run_ids: list, atperf_path: str) -> dict:
    """Run render for one or more run IDs and return time/memory metrics."""
    import time

    # Ensure run_ids is a list
    if isinstance(run_ids, str):
        run_ids = [run_ids]

    # Skip if no valid run IDs
    if not run_ids or not all(run_ids):
        return {"render_time_ms": 0.0, "memory_usage_mb": 0.0}

    # Measure render time
    start_time = time.time()
    run_ids_str = " ".join(run_ids)
    result = invoke_atperf(f"run render {run_ids_str} {ATPERF_JSON_FLAG}", path=atperf_path)
    end_time = time.time()

    # Get session info and memory usage
    session_id = extract_session_id(result)
    render_list_output = invoke_atperf(f"render list {ATPERF_JSON_FLAG}", path=atperf_path)
    memory_usage_gib = extract_render_session_memory_usage(session_id, render_list_output)

    try:
        if not memory_usage_gib:
            logging.debug(f"render list output for session '{session_id}': {render_list_output}")
            raise ValueError(
                f"render list returned no memory_usage_gib for session '{session_id}'. "
                "The db_instances entry may be missing, zero, or the render session was not found."
            )

        return {
            "render_time_ms": (end_time - start_time) * 1000,
            "memory_usage_mb": memory_usage_gib * 1024
        }
    finally:
        # Best-effort cleanup: always close the session to avoid leaking render sessions
        try:
            invoke_atperf(f"render close {session_id}", path=atperf_path)
        except Exception as close_err:
            logging.warning(f"Failed to close render session '{session_id}': {close_err}")

engine_overhead_BENCHMARK_NAME = "engine_overhead"
class AtperfOverheadBenchmark(Benchmark):
    name = engine_overhead_BENCHMARK_NAME
    description = "Benchmark to measure atperf overhead and timing breakdown"
    long_description = (
        "Measures per-workload atperf overhead by running a recipe multiple times, parsing timing lines into "
        "connection, setup overhead, collection/analysis (or collection + analysis split in agent mode), file transmission, "
        "and estimating run artifact size. Produces JSON reports with individual metric quality assessments. "
        "Prereqs: configured target, atperf, perf. "
        "Agent mode controlled by APXD_ENABLE_AGENT."
    )

    def __init__(self):
        super().__init__()


    def generate_report(self) -> BenchmarkResult:
        """Generate benchmark report as a BenchmarkResult data structure."""

        # Define all metrics we want to report on
        all_metrics = [
            # Timing category metrics
            MetricType.CONNECTION_MS,
            MetricType.COLLECTION_MS,
            MetricType.ANALYSIS_MS,
            MetricType.TARGET_PREPARATION_MS,
            MetricType.FILE_TRANSMISSION_MS,
            MetricType.COLLECTION_ANALYSIS_MS,
            MetricType.RECIPE_RUN_TOTAL_MS,
            MetricType.RUN_SIZE_MB,
            MetricType.RENDER_TIME_MS,
            MetricType.RENDER_MEMORY_MB,
            MetricType.COMPARISON_RENDER_TIME_MS,
            MetricType.COMPARISON_MEMORY_USAGE_MB,
        ]

        # Generate headers: workload + each metric + each metric_quality
        headers = ["workload"]
        for metric in all_metrics:
            headers.append(metric.value)
            headers.append(f"{metric.value}_quality")

        # Generate rows
        rows = []
        for workload, timing in self._avg_runs.items():
            row = [workload]

            if not is_recognised_workload(workload):
                logging.warning(
                    f"Unrecognised workload '{workload}': no collection target is defined. "
                    "All quality assessments for this workload will be INDETERMINABLE."
                )
                for metric in all_metrics:
                    row.append(f"{timing.get_metric_value(metric):.3f}")
                    row.append(QualityLevel.INDETERMINABLE.value)
            else:
                # Add each metric value and its quality assessment
                for metric in all_metrics:
                    # Get the value for this metric from the timing object
                    value = timing.get_metric_value(metric)
                    # Add value and quality assessment
                    row.append(f"{value:.3f}")
                    quality = AtperfOverheadBenchmark.threshold(metric, value, workload)
                    row.append(quality.value)

            rows.append(row)

        return BenchmarkResult(headers=headers, rows=rows)


    @staticmethod
    def add_arguments(parser):
        add_common_benchmark_args(parser)
        parser.add_argument("--recipe", type=str, required=True, help="Specify the atperf recipe to use")
        parser.add_argument("-t", "--target", type=str, default="localhost", help="Specify the target to use (default: localhost)")
# Helper for comparison render metrics

    @staticmethod
    def _collect_workload_metrics(workload: str, recipe: str, target: str, n_runs: int, atperf_path: str):
        """Collect metrics for a single workload across multiple runs."""
        run_metadata_list = []
        run_ids_list = []
        comparison_metrics = []
        first_run_id = None

        logging.info(f"Collecting atperf overhead for workload: {workload}")

        # Get OS-specific run directory
        os_dir_dict = {"Windows": RUN_DIR_WINDOWS, "Linux": RUN_DIR_LINUX, "Darwin": RUN_DIR_MACOS}

        # Collect data for each run
        for i in range(n_runs):
            logging.info(f" Run {i+1}/{n_runs}")
            result = collect_engine_overhead(workload, recipe, target=target, atperf_path=atperf_path)
            run = extract_run_timings(result)
            run_id = extract_run_id(result)
            run_ids_list.append(run_id)

            if i == 0:
                first_run_id = run_id

            logging.debug(f"Extracted run ID: {run_id}")

            # Measure run artifact size
            if run_id:
                logging.debug(f"Detected OS: {platform.system()}")
                run_dir = os.path.join(os_dir_dict[platform.system()], run_id)
                logging.debug(f"Run directory: {run_dir}")
                if run_dir and os.path.exists(run_dir):
                    run.run_size_mb = get_dir_size_mb(run_dir)
                    logging.debug(f"Measured run size: {run.run_size_mb} MB")
                else:
                    logging.warning(f"Run directory not found: {run_dir}")
            else:
                logging.warning("No run_id found; cannot measure run size.")

            # Get render metrics for this run
            render_metrics = gather_render_metrics([run_id], atperf_path)
            run.add_render_metrics(render_metrics)
            run_metadata_list.append(run)

        # Per-run comparisons (if n_runs >= 2)
        if n_runs >= 2 and first_run_id:
            for i in range(1, n_runs):
                this_run_id = run_ids_list[i]
                if not this_run_id:
                    continue
                metrics = gather_render_metrics([first_run_id, this_run_id], atperf_path)
                comparison_metrics.append(metrics)

        return run_metadata_list, run_ids_list, first_run_id, comparison_metrics

    @staticmethod
    def _compute_average_metrics(runs: list, comparison_metrics: list) -> TimingCollection:
        """Compute average metrics across multiple runs."""
        if not runs:
            return TimingCollection()

        avg_timing = TimingCollection()
        num_runs = len(runs)

        # Sum all metrics
        for run in runs:
            for cat in TimingCategory:
                avg_timing.totals_ms[cat] += run.totals_ms[cat]
            avg_timing.recipe_run_total_ms += run.recipe_run_total_ms
            avg_timing.run_size_mb += run.run_size_mb
            avg_timing.render_time_ms += run.render_time_ms
            avg_timing.render_memory_mb += run.render_memory_mb

        # Calculate averages
        for cat in TimingCategory:
            avg_timing.totals_ms[cat] /= num_runs
        avg_timing.recipe_run_total_ms /= num_runs
        avg_timing.run_size_mb /= num_runs
        avg_timing.render_time_ms /= num_runs
        avg_timing.render_memory_mb /= num_runs

        # Average comparison metrics
        if comparison_metrics:
            avg_timing.comparison_render_time_ms = sum(m["render_time_ms"] for m in comparison_metrics) / len(comparison_metrics)
            avg_timing.comparison_memory_usage_mb = sum(m["memory_usage_mb"] for m in comparison_metrics) / len(comparison_metrics)
        else:
            avg_timing.comparison_render_time_ms = 0.0
            avg_timing.comparison_memory_usage_mb = 0.0

        return avg_timing

    @staticmethod
    def _log_average_metrics(workload: str, avg_timing: TimingCollection) -> None:
        """Log average metrics for a workload."""
        logging.info(f" Workload: {workload}")
        logging.info(f"  Connection: {avg_timing.totals_ms[TimingCategory.CONNECTION]:.3f} ms")
        logging.info(f"  Target Preparation: {avg_timing.totals_ms[TimingCategory.TARGET_PREPARATION]:.3f} ms")

        if USE_SL_COLLECT:
            logging.info(f"  Collection & Analysis: {avg_timing.totals_ms[TimingCategory.COLLECTION_ANALYSIS]:.3f} ms")
        else:
            logging.info(f"  Collection: {avg_timing.totals_ms[TimingCategory.COLLECTION]:.3f} ms")
            logging.info(f"  Analysis: {avg_timing.totals_ms[TimingCategory.ANALYSIS]:.3f} ms")

        logging.info(f"  File Transmission: {avg_timing.totals_ms[TimingCategory.FILE_TRANSMISSION]:.3f} ms")
        logging.info(f"  Run Size: {avg_timing.run_size_mb:.3f} MB")
        logging.info(f"  Render Time: {avg_timing.render_time_ms:.3f} ms")
        logging.info(f"  Render Memory Usage: {avg_timing.render_memory_mb:.3f} MB")
        logging.info(f"  Comparison Render Time: {avg_timing.comparison_render_time_ms:.3f} ms")
        logging.info(f"  Comparison Render Memory Usage: {avg_timing.comparison_memory_usage_mb:.3f} MB")
        logging.info(f"  Total: {avg_timing.recipe_run_total_ms:.3f} ms")

    @staticmethod
    def threshold(metric: MetricType, actual_value: float, workload: str = "") -> QualityLevel:
        """Assess quality level for a single metric."""
        # Find the MetricType that matches this string value
        metric_type = None
        for mt in MetricType:
            if mt.value == metric.value:
                metric_type = mt
                break

        quality_targets = get_workload_engine_overhead_targets(workload)

        # If no matching metric type or no quality target, return moderate
        if not metric_type or metric_type not in quality_targets:
            return QualityLevel.INDETERMINABLE

        target = quality_targets[metric_type]

        # Apply quality thresholds
        if actual_value > target:  # Over threshold = poor
            return QualityLevel.POOR
        elif actual_value > target * ENGINE_OVERHEAD_MODERATE_MULTIPLIER:  # Between multiplier and 1x threshold = moderate
            return QualityLevel.MODERATE
        else:  # Below multiplier threshold = good
            return QualityLevel.GOOD

    def run(self, args):
        """Run benchmark to collect atperf overhead metrics across workloads."""
        atperf_path = args.atperf_path
        n_runs = int(args.n_runs)
        recipe = args.recipe
        target = args.target
        workloads = args.workloads or []

        if not workloads:
            logging.error("No workloads specified. Provide at least one -w WORKLOAD.")
            return False

        kill_atperf_engine(atperf_path)
        atperf_prepare_target(target, atperf_path=atperf_path)
        # Connect to target agent in advance, so that all workload runs use cached connection for consistency
        create_agent_connection(target, atperf_path)

        # Data structures for tracking results
        results = {}
        comparison_metrics_by_workload = {}
        avg_runs = {}

        # Process each workload
        for workload in workloads:
            # Collect metrics for this workload
            runs, run_ids, _, comp_metrics = AtperfOverheadBenchmark._collect_workload_metrics(
                workload, recipe, target, n_runs, atperf_path
            )

            # Store results
            results[workload] = runs
            comparison_metrics_by_workload[workload] = comp_metrics

            # Compute average metrics
            avg_timing = AtperfOverheadBenchmark._compute_average_metrics(runs, comp_metrics)
            avg_runs[workload] = avg_timing

            # Log results for this workload
            AtperfOverheadBenchmark._log_average_metrics(workload, avg_timing)

        # Store averaged results for report generation
        self._avg_runs = avg_runs

        return True

    def report(self, args) -> BenchmarkMetadata:
        # Check if we have results from run phase
        if not hasattr(self, '_avg_runs'):
            error_msg = "No benchmark results available. Run phase must complete successfully before generating report."
            logging.error(error_msg)
            self.metadata.add_error(error_msg)
            return self.metadata

        report_result = self.generate_report()
        logging.info(f"Generated report with {len(report_result.rows)} workload results")

        reports_dir = Path(REPORTS)
        reports_dir.mkdir(parents=True, exist_ok=True)
        json_file = reports_dir / f"engine_overhead_report{JSON_EXTENSION}"
        report_result.to_json(json_file)
        logging.info(f"Report saved to: {json_file}")

        # Compute distribution across all *_quality columns
        quality_columns = [i for i, h in enumerate(report_result.headers) if h.endswith('_quality')]

        # Flatten all *_quality cells into a single-column BenchmarkResult
        flat_rows = []
        for row in report_result.rows:
            for ci in quality_columns:
                if ci < len(row):
                    flat_rows.append([row[ci]])

        quality_only = BenchmarkResult(headers=['quality_level'], rows=flat_rows)
        dist = consolidate_column_quality(quality_only, quality_column_name='quality_level')
        self.metadata.set_quality_distribution(dist)

        return self.metadata
