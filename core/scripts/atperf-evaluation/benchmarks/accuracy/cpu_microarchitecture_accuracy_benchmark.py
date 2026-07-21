# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
===============================================================================
cpu_microarchitecture_accuracy_benchmark.py

Purpose:
    Collect Topdown metric groups via atperf and the Arm topdown_tool for one or
    more workloads and multiple repeated runs, normalize their outputs and
    produce per-run comparison CSVs (atperf vs topdown_tool).

Workflow:
    1. Discover metric groups: atperf recipe info cpu_microarchitecture
    2. For each workload and run:
         - Collect atperf JSON + sanitize -> metric,value,units
         - Collect topdown_tool CSV + sanitize -> metric,value,units
         - Compare and compute per-metric delta & percent delta
    3. Aggregate Across runs

Assumptions / Requirements:
    - Linux AArch64 target (or environment compatible with provided tools)
    - atperf, topdown_tool, perf installed and accessible
    - Workloads are safe/idempotent to run multiple times

Example Usage:
python3 atperf-eval.py cpu_microarchitecture_accuracy --atperf-path "/opt/deps/atperf" -w "/opt/deps/megabench 0 0" -w "/opt/deps/workload" --n-runs 10

===============================================================================
"""
import csv
import pathlib

from benchmarks.benchmark import Benchmark, BenchmarkResult
from framework.args import add_common_benchmark_args
from framework.metadata import BenchmarkMetadata
from framework.metric_mappings import get_atperf_metric_name
from framework.runner import *
from framework.rag_thresholds import *
from framework.utils import *

NONE_ENTRY = "none"
INDETERMINABLE_ENTRY = "indeterminable"

"""
Takes the full JSON response from the application,
writes a CSV with columns [metric, value, units],
skipping rows where metric name contains "(self)"
"""
def sanitize_atperf_data(response_json, csv_filename):
    rows = response_json["data"]["rows"]

    with open(csv_filename, "w", newline="") as csvfile:
        writer = csv.writer(csvfile)
        writer.writerow(["metric", "value", "units"])
        for row in rows:
            metric = row.get("metric").strip()
            value = row.get("value")
            if not metric.endswith("(self)"):
                metric = row.get("metric").removesuffix(" (total)")
                writer.writerow([metric, value, row.get("units")])

"""
Reads a Topdown tool CSV, writes a sanitized file in the atperf style.
Keeps only [metric, value, units], and standardizes units to 'percent' if needed.

Args:
    input_csv: Path to the topdown_tool output (can be a directory or CSV file)
    output_csv: Path where the sanitized CSV will be written
"""
def sanitize_topdown_tool_data(input_csv, output_csv):
    # Handle new directory structure: find the actual CSV file
    input_path = pathlib.Path(input_csv)
    if input_path.is_dir():
        # Navigate through the directory structure to find the CSV
        # Structure: <base_dir>/<timestamp>/cpu/<actual_csv>
        csv_files = list(input_path.glob("*/cpu/*.csv"))
        if not csv_files:
            logging.error(f"No CSV file found in topdown_tool output directory: {input_csv}")
            return
        if len(csv_files) > 1:
            logging.warning(f"Multiple CSV files found in topdown_tool output directory {input_csv}")
            raise ValueError("Multiple CSV files found")

        actual_csv = csv_files[0]
        logging.debug(f"Found topdown_tool CSV at: {actual_csv}")
    else:
        actual_csv = input_path

    with open(actual_csv, "r", newline='') as infile, open(output_csv, "w", newline='') as outfile:
        reader = csv.DictReader(infile)
        writer = csv.writer(outfile)
        writer.writerow(["metric", "value", "units"])
        for row in reader:
            metric = row.get("metric", "").strip()
            value = row.get("value", "").strip()
            units = row.get("units", "").strip()

            # Convert ratio metrics to percentages for comparison with atperf
            if "_ratio" in metric.lower():
                logging.debug(f"Converting {metric} value {value} to percentage")
                try:
                    value = str(100 * float(value))
                    units = "percent"
                    metric = metric.replace("_ratio", "_percentage")
                except (ValueError, TypeError):
                    pass

            # Map topdown_tool metric name to atperf metric name
            atperf_metric = get_atperf_metric_name(metric)
            if atperf_metric != metric:
                logging.debug(f"Mapped topdown_tool metric '{metric}' to atperf metric '{atperf_metric}'")

            writer.writerow([atperf_metric, value, units.lower()])


"""Run atperf cpu_microarchitecture recipe for a single metric group.

Args:
    workload: The workload command string to execute.
    metric_group: The atperf metric group name.
    atperf_path: Path to the atperf binary.
Returns:
    True on success, False otherwise.
"""
def benchmark_atperf_cpu_microarchitecture(workload, metric_group, atperf_path, logs_dir: pathlib.Path, sanitized_dir: pathlib.Path):
    kill_atperf_engine(atperf_path)
    logging.info("-=== Benchmarking atperf ===-")

    # Run recipe
    logging.info("Running recipe...")
    args = f"recipe run cpu_microarchitecture {ATPERF_WORKLOAD_FLAG} \"{workload}\" {ATPERF_METRIC_GROUPS_FLAG}{metric_group} {ATPERF_TARGET_FLAG} localhost {ATPERF_DEPLOY_TOOLS_FLAG}"
    result = invoke_atperf(args, path=atperf_path)
    run_id = extract_run_id(result)
    logging.info(f"Run ID: {run_id}")

    # Render run
    logging.info("Creating render session...")
    args = f"run render {run_id} {ATPERF_JSON_FLAG}"
    result = invoke_atperf(args, path=atperf_path)
    render_session_id = extract_session_id(result)
    if render_session_id == "":
        logging.error(f"[{metric_group}] Failed to extract render session ID from atperf output.")
        return False
    logging.info(f"Render Session ID: {render_session_id}")

    # Query render
    logging.info("Running render query...")
    args = f"render query {render_session_id} \"{ATPERF_TOPDOWN_QUERY}\" {ATPERF_JSON_FLAG}"
    atperf_benchmark_results = invoke_atperf(args, path=atperf_path)
    if atperf_benchmark_results is None:
        logging.error(f"[{metric_group}] Failed to query render session")
        return False

    # Store results
    logging.info("Storing results...")
    outfile = logs_dir / f"{metric_group}_{ATPERF_NAME}{JSON_EXTENSION}"
    outfile.parent.mkdir(parents=True, exist_ok=True)
    outfile.write_text(atperf_benchmark_results)

    # Sanitize results
    sanitized_outfile = sanitized_dir / f"{metric_group}_{ATPERF_NAME}{CSV_EXTENSION}"
    sanitized_outfile.parent.mkdir(parents=True, exist_ok=True)
    sanitize_atperf_data(json.loads(atperf_benchmark_results), sanitized_outfile)

    # Teardown
    args = f"render close {render_session_id}"
    result = invoke_atperf(args, path=atperf_path)

    logging.info(f"atperf results written to '{sanitized_outfile}'")
    logging.info("--- Benchmark complete ---")
    return True

def benchmark_topdown_tool(workload, metric_group, arm_topdown_tool_path, logs_dir: pathlib.Path, sanitized_dir: pathlib.Path):
    logging.info("-=== Benchmarking topdown_tool ===-")
    logging.info("Running topdown_tool...")
    # Create a directory for the output (topdown_tool now creates subdirectories)
    output_dir = logs_dir / f"{metric_group.lower()}_{TOPDOWN_TOOL_NAME}"
    output_dir.parent.mkdir(parents=True, exist_ok=True)
    args = f"{TOPDOWN_TOOL_PROBE_FLAG} CPU {TOPDOWN_TOOL_METRIC_GROUP_FLAG} {metric_group} {TOPDOWN_TOOL_GENERATE_CSV_FLAG} {TOPDOWN_TOOL_CSV_FLAG} {output_dir} {workload}"
    invoke_topdown_tool(args, path=arm_topdown_tool_path)

    # Sanitize results (sanitize_topdown_tool_data now handles directory structure)
    sanitized_outfile = sanitized_dir / f"{metric_group.lower()}_{TOPDOWN_TOOL_NAME}{CSV_EXTENSION}"
    sanitized_outfile.parent.mkdir(parents=True, exist_ok=True)
    sanitize_topdown_tool_data(output_dir, sanitized_outfile)

    logging.info(f"topdown_tool results written to '{sanitized_outfile}'")
    logging.info("--- Benchmark complete ---")
    return True


CPU_MICROARCHITECTURE_ACCURACY_BENCHMARK_NAME = "cpu_microarchitecture_accuracy"
class CPUMicroarchitectureAccuracyBenchmark(Benchmark):
    name = CPU_MICROARCHITECTURE_ACCURACY_BENCHMARK_NAME
    description = "Compares CPU Microarchitecture recipe metrics from atperf vs topdown_tool"

    def __init__(self):
        super().__init__()

    def generate_workload_report(self, workload_slug: str, workload_index: int, data_source_dir: pathlib.Path, n_runs: int) -> BenchmarkResult:
        """
        Generate a tidy workload report as a BenchmarkResult data structure.
        Only includes metrics that have both atperf and topdown_tool values.
        Reads per-run comparison files and creates aggregated report with quality levels.
        Metrics with very low MPKI (< MPKI_IGNORE_THRESHOLD) are marked as ignored / filtered out.

        Args:
            workload_slug: Identifier for the workload
            workload_index: Index of the workload for organizing metadata
            data_source_dir: Directory containing run1_comparison.csv, run2_comparison.csv, etc.
            n_runs: Number of runs to aggregate
        """
        # Define headers for the clean report
        headers = [
            'metric_group', 'metric_name', 'unit',
            'atperf_average', 'arm_topdown_tool_average',
            'atperf_stddev', 'arm_topdown_tool_stddev',
            'delta', 'percent_delta', 'quality_level', 'ignored'
        ]

        # Collect per-run files from data source directory
        per_run_files = []
        for i in range(1, n_runs + 1):
            f = data_source_dir / f"run{i}_comparison.csv"
            if f.exists():
                per_run_files.append((i, f))

        if not per_run_files:
            warning_msg = f"[workload={workload_slug}] No per-run comparison files found in {data_source_dir}"
            logging.warning(warning_msg)
            self.metadata.add_warning(warning_msg)
            return BenchmarkResult(headers=headers, rows=[])

        # Data collection (same as original compute_workload_summary)
        aggregated = {}
        for run_idx, filepath in per_run_files:
            with filepath.open(newline='') as f:
                reader = csv.DictReader(f)
                for row in reader:
                    group = row.get('group', NONE_ENTRY).strip()
                    metric = row.get('metric', NONE_ENTRY).strip()
                    units = row.get('units', NONE_ENTRY).strip()
                    key = (group, metric, units)

                    entry = aggregated.setdefault(key, {})
                    entry[run_idx] = {}

                    atperf_val = row.get('atperf value', NONE_ENTRY)
                    topdown_tool_val = row.get('topdown_tool value', NONE_ENTRY)
                    delta = row.get('delta', NONE_ENTRY)
                    percent_delta = row.get('percent delta', NONE_ENTRY)

                    if atperf_val != NONE_ENTRY: entry[run_idx]['atperf'] = atperf_val
                    if topdown_tool_val != NONE_ENTRY: entry[run_idx]['topdown_tool'] = topdown_tool_val
                    if delta != NONE_ENTRY: entry[run_idx]['delta'] = delta
                    if percent_delta != NONE_ENTRY: entry[run_idx]['percent_delta'] = percent_delta

        sorted_keys = sorted(aggregated.keys(), key=lambda k: (k[0], k[1]))

        # Collect all rows first and track unmatched metrics by tool
        all_rows = []
        atperf_only_metrics = []
        topdown_tool_only_metrics = []

        for (group, metric, units) in sorted_keys:
            per_metric = aggregated[(group, metric, units)]

            # Collect values across runs
            atperf_raw_vals = []
            topdown_raw_vals = []

            for i in range(1, n_runs + 1):
                cell = per_metric.get(i, {})
                if 'atperf' in cell: atperf_raw_vals.append(cell.get('atperf'))
                if 'topdown_tool' in cell: topdown_raw_vals.append(cell.get('topdown_tool'))

            atperf_nums = [float(v) for v in atperf_raw_vals if safe_float(v) is not None]
            topdown_nums = [float(v) for v in topdown_raw_vals if safe_float(v) is not None]

            # Compute averages
            atperf_mean = float(mean(atperf_nums)) if atperf_nums else ''
            atperf_mean_str = round_to_3sf(atperf_mean) if atperf_mean != '' else None

            topdown_mean = float(mean(topdown_nums)) if topdown_nums else ''
            topdown_mean_str = round_to_3sf(topdown_mean) if topdown_mean != '' else None

            avg_percent_delta = ''
            if atperf_mean != '' and topdown_mean != '':
                if topdown_mean == 0:
                    avg_percent_delta = float('inf')
                else:
                    avg_percent_delta = round_to_3sf(100 * (atperf_mean - topdown_mean) / topdown_mean)

            # CHECK FOR UNMATCHED METRICS: Track which metrics don't have comparison data
            if not atperf_raw_vals or not topdown_raw_vals:
                # Determine which tool has the data and which is missing
                if atperf_raw_vals and not topdown_raw_vals:
                    atperf_only_metrics.append(metric)
                elif topdown_raw_vals and not atperf_raw_vals:
                    topdown_tool_only_metrics.append(metric)

                logging.debug(f"Filtering out metric {metric} from group {group} - missing comparison data")
                continue

            # Compute delta from averages
            avg_delta = ''
            if atperf_mean != '' and topdown_mean != '':
                try:
                    avg_delta = round_to_3sf(atperf_mean - topdown_mean)
                except (ValueError, TypeError):
                    pass

            # Handle n_runs == 1 case for standard deviation
            if n_runs == 1:
                atperf_std = 'N/A'
                topdown_std = 'N/A'
            else:
                atperf_std = round_to_3sf(stddev(atperf_nums)) if len(atperf_nums) > 1 else ''
                topdown_std = round_to_3sf(stddev(topdown_nums)) if len(topdown_nums) > 1 else ''

            # Compute quality level using framework enums
            quality_level = CPUMicroarchitectureAccuracyBenchmark.threshold(avg_percent_delta)

            ignored = False
            if atperf_nums and topdown_nums:
                ignored = CPUMicroarchitectureAccuracyBenchmark.is_ignored(atperf_mean, topdown_mean, units)

            # Build row
            row = [
                group,               # metric_group
                metric,              # metric_name
                units,               # unit
                atperf_mean_str,     # atperf_average
                topdown_mean_str,    # arm_topdown_tool_average
                atperf_std,          # atperf_stddev
                topdown_std,         # arm_topdown_tool_stddev
                avg_delta,           # delta
                avg_percent_delta,   # percent_delta
                quality_level.value, # quality_level
                ignored              # ignored
            ]
            all_rows.append(row)

        # Store unmatched metrics in metadata.data organized by workload then tool
        if atperf_only_metrics or topdown_tool_only_metrics:
            # Initialize unmatched_metrics structure if not exists
            if 'unmatched_metrics' not in self.metadata.data:
                self.metadata.data['unmatched_metrics'] = {}

            # Create the workload entry organized by tool
            workload_key = f'workload_{workload_index}'
            self.metadata.data['unmatched_metrics'][workload_key] = {}

            if atperf_only_metrics:
                self.metadata.data['unmatched_metrics'][workload_key]['atperf'] = atperf_only_metrics
                debug_msg = f"Workload {workload_index}: {len(atperf_only_metrics)} atperf-only metrics"
                logging.debug(debug_msg)

            if topdown_tool_only_metrics:
                self.metadata.data['unmatched_metrics'][workload_key]['topdown_tool'] = topdown_tool_only_metrics
                warning_msg = f"Workload {workload_index}: {len(topdown_tool_only_metrics)} topdown-tool-only metrics"
                logging.warning(warning_msg)
                self.metadata.add_warning(warning_msg)

            total_unmatched = len(atperf_only_metrics) + len(topdown_tool_only_metrics)
            logging.info(f"[workload={workload_slug}] Filtered out {total_unmatched} unmatched metrics from final report")

        # Sort all rows by metric group alphabetically (index 0), then metric name (index 1)
        all_rows.sort(key=lambda r: (r[0], r[1]))

        # Create result and add sorted rows
        result = BenchmarkResult(headers=headers, rows=all_rows)
        logging.info(f"[workload={workload_slug}] Generated {len(result)} metrics in report data structure (sorted by quality)")
        return result

    @staticmethod
    def threshold(value: str) -> QualityLevel:
        """Determine quality level for cpu_microarchitecture accuracy benchmark based on percent delta."""
        if value == '' or value is None:
            return QualityLevel.INDETERMINABLE

        try:
            abs_delta = abs(float(value))
            if abs_delta <= TOPDOWN_ACCURACY_GOOD_QUALITY_THRESHOLD:
                return QualityLevel.GOOD
            elif abs_delta <= TOPDOWN_ACCURACY_MODERATE_QUALITY_THRESHOLD:
                return QualityLevel.MODERATE
            else:
                return QualityLevel.POOR
        except (ValueError, TypeError):
            return QualityLevel.INDETERMINABLE

    @staticmethod
    def is_ignored(atperf_mean: float, topdown_mean: float, units: str) -> bool:
        """Determines whether a metric should be filtered out based on the values collected."""
        match units:
            case "mpki":
                if atperf_mean < TOPDOWN_ACCURACY_MPKI_IGNORE_THRESHOLD and topdown_mean < TOPDOWN_ACCURACY_MPKI_IGNORE_THRESHOLD:
                    return True
            case "percent":
                if atperf_mean < TOPDOWN_ACCURACY_PERCENT_IGNORE_THRESHOLD and topdown_mean < TOPDOWN_ACCURACY_PERCENT_IGNORE_THRESHOLD:
                    return True
        return False


    @staticmethod
    def add_arguments(parser):
        add_common_benchmark_args(parser)
        parser.add_argument('--arm-topdown-tool-path', type=str, default=DEFAULT_TOPDOWN_TOOL_PATH, help=f'Path to the Arm topdown_tool binary (default: {DEFAULT_TOPDOWN_TOOL_PATH})')
        parser.add_argument('-t', '--test', action='store_true', help='Run a quick test instead of full benchmark')

    def run(self, args) -> bool:
        # Initial setup
        atperf_path = args.atperf_path
        arm_topdown_tool_path = args.arm_topdown_tool_path
        test_mode = bool(args.test)
        n_runs = int(args.n_runs)
        workloads = args.workloads or []
        if not workloads:
            logging.error("No workloads specified. Provide at least one -w WORKLOAD.")
            return False

        # Set perf paranoid
        run_cmd("sudo sysctl -w kernel.perf_event_paranoid=-1")
        atperf_prepare_target("localhost", atperf_path=atperf_path)

        # Discover metric groups once
        metric_groups = get_metric_groups(atperf_path)

        if test_mode:
            logging.warning("Test mode enabled, limiting to first 5 metric groups.")
            metric_groups = metric_groups[0:5]

        for workload in workloads:
            workload_slug = slugify_workload(workload)
            logging.info(f"=== Processing workload: {workload} ===")

            for run_idx in range(1, n_runs + 1):
                logging.info(f"-- Run {run_idx}/{n_runs} --")
                # Per-run directories
                run_log_dir = LOGS / f"{workload_slug}" / f"run_{run_idx}"
                run_data_dir = SANITIZED_TOOL_DATA / f"{workload_slug}" / f"run_{run_idx}"
                workload_results_dir = RESULTS / f"{workload_slug}"
                for d in (run_log_dir, run_data_dir, workload_results_dir):
                    d.mkdir(parents=True, exist_ok=True)

                for group in metric_groups:
                    logging.info(f"Collecting metrics group: {group}")
                    if benchmark_atperf_cpu_microarchitecture(workload, group, atperf_path, run_log_dir, run_data_dir) is False:
                        logging.error(f"{group} atperf collection failed.")
                    if benchmark_topdown_tool(workload, group, arm_topdown_tool_path, run_log_dir, run_data_dir) is False:
                        logging.error(f"{group} topdown_tool collection failed.")

                # Per-run comparison
                self.compute_run_summary(workload_slug, run_idx, run_data_dir, workload_results_dir)

        return True

    def report(self, args) -> BenchmarkMetadata:
        # Prepare vars
        workloads = args.workloads or []
        n_runs = int(args.n_runs)
        results_dir = pathlib.Path(RESULTS)
        reports_dir = pathlib.Path(REPORTS)
        reports_dir.mkdir(parents=True, exist_ok=True)

        # Generate reports for each workload
        for workload_index, workload in enumerate(workloads):
            workload_slug = slugify_workload(workload)
            data_source_dir = results_dir / workload_slug

            # Generate the report data structure from source data
            report_result = self.generate_workload_report(workload_slug, workload_index, data_source_dir, n_runs)
            logging.debug(f"Generated report result: {report_result}")
            if len(report_result.rows) == 0:
                self.metadata.add_warning(f"No metrics found for workload {workload_index}")
                continue

            # Compute and record full distribution only
            overall_quality = consolidate_column_quality(report_result, quality_column_name="quality_level", ignored_column_name="ignored")
            self.metadata.set_quality_distribution(overall_quality)

            # Save JSON report
            json_file = reports_dir / f"cpu_microarchitecture_accuracy{workload_index}{JSON_EXTENSION}"
            report_result.to_json(json_file)
            logging.info(f"Report saved to: {json_file}")

        return self.metadata

    def compute_run_summary(self, workload_slug: str, run_idx: int, sanitized_dir: pathlib.Path, results_dir: pathlib.Path):
        """Produce a per-run comparison CSV for a specific workload & run.

        Reads sanitized atperf & topdown_tool CSVs in sanitized_dir and writes
        run-scoped comparison to results_dir.
        """
        group_set = set()
        for f in sanitized_dir.glob(f"*_{ATPERF_NAME}{CSV_EXTENSION}"):
            match = re.match(rf"(.+?)_{ATPERF_NAME}\{CSV_EXTENSION}$", f.name)
            if match:
                group_set.add(match.group(1))
        for f in sanitized_dir.glob(f"*_{TOPDOWN_TOOL_NAME}{CSV_EXTENSION}"):
            match = re.match(rf"(.+?)_{TOPDOWN_TOOL_NAME}\{CSV_EXTENSION}$", f.name)
            if match:
                group_set.add(match.group(1))
        groups = sorted(group_set)

        summary_rows = []
        for group in groups:
            atperf_file = sanitized_dir / f"{group}_{ATPERF_NAME}{CSV_EXTENSION}"
            topdown_file = sanitized_dir / f"{group}_{TOPDOWN_TOOL_NAME}{CSV_EXTENSION}"
            if not (atperf_file.exists() and topdown_file.exists()):
                logging.debug(f"[workload={workload_slug} run={run_idx}] Skipping group '{group}' (missing file)")
                continue

            atperf = {}
            with atperf_file.open(newline='') as f:
                reader = csv.DictReader(f)
                for row in reader:
                    atperf[row["metric"]] = (row["value"], row.get("units", ""))

            topdown = {}
            with topdown_file.open(newline='') as f:
                reader = csv.DictReader(f)
                for row in reader:
                    topdown[row["metric"]] = row["value"], row.get("units", "")

            all_metrics = sorted(set(atperf.keys()) | set(topdown.keys()))
            for metric in all_metrics:
                atperf_val, units = atperf.get(metric, (None, None))
                if atperf_val is None:
                    topdown_val, units = topdown.get(metric, (None, None))
                else:
                    topdown_val, _ = topdown.get(metric, (None, None))

                if "percent" in units.lower():
                    units = "percent"
                    atperf_val = round_to_3sf(atperf_val) if atperf_val is not None else None
                    topdown_val = round_to_3sf(topdown_val) if topdown_val is not None else None
                else:
                    atperf_val = round_to_3sf(atperf_val) if atperf_val is not None else None
                    topdown_val = round_to_3sf(topdown_val) if topdown_val is not None else None

                values_present = atperf_val is not None and topdown_val is not None
                values_calculable = safe_float(atperf_val) is not None and safe_float(topdown_val) is not None
                delta = INDETERMINABLE_ENTRY
                percentage_delta = INDETERMINABLE_ENTRY

                if values_calculable:
                    delta = float(atperf_val) - float(topdown_val)
                    try:
                        percentage_delta = 100 * (float(atperf_val) - float(topdown_val)) / float(topdown_val)
                    except ZeroDivisionError:
                        percentage_delta = float('inf')

                atperf_val_display = NONE_ENTRY if atperf_val is None else atperf_val
                topdown_val_display = NONE_ENTRY if topdown_val is None else topdown_val
                row = [group, metric, units, atperf_val_display, topdown_val_display, delta, percentage_delta]

                summary_rows.append(row)

        results_dir.mkdir(parents=True, exist_ok=True)
        summary_file = results_dir / f"run{run_idx}_comparison.csv"
        with summary_file.open("w", newline='') as f:
            writer = csv.writer(f)
            writer.writerow(["group", "metric", "units", "atperf value", "topdown_tool value", "delta", "percent delta"])
            writer.writerows(summary_rows)
