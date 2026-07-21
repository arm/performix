# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from benchmarks.benchmark import Benchmark
from framework.args import add_common_benchmark_args
from framework.code_hotspots.comparison import *
from framework.code_hotspots.metrics import profile_variability
from framework.metadata import BenchmarkMetadata
from framework.runner import *
from framework.code_hotspots.io import *

def _clean_symbol(symbol):
    """
    Clean a symbol string by removing ANSI color codes and perf report formatting.

    Args:
        symbol: Raw symbol string from perf report output

    Returns:
        Cleaned symbol string with ANSI codes and gutter formatting removed
    """
    _GUTTER_RE = re.compile(r"\s+-\s+-\s*$") # Removes gutters
    _ANSI_RE   = re.compile(r"\x1b\[[0-9;]*m") # Removes ANSI color codes
    s = _ANSI_RE.sub("", symbol)         # strip ANSI (usually none with --stdio)
    s = s.rstrip()                    # drop trailing spaces
    s = _GUTTER_RE.sub("", s)         # drop " -      -" gutter
    return s



def sanitize_atperf_data(json_response, csv_out_path):
    """
    Convert atperf JSON response data to a sanitized CSV format.

    Args:
        json_response: JSON response from atperf render query
        csv_out_path: Path where the sanitized CSV file will be written
    """
    rows = json_response["data"]["rows"]
    cols = [col["name"] for col in json_response["data"]["cols"]]

    with open(csv_out_path, "w", newline="", encoding="utf-8") as csvfile:
        writer = csv.writer(csvfile)
        writer.writerow(cols)
        for row in rows:
            if not row.get("function_name"):
                row["function_name"] = "ROOT"
            writer.writerow([row.get(col) for col in cols])

def sanitize_perf_data(perf_report_stdout, csv_out_path):
    """
    Parse perf report stdout and write to a sanitized CSV file.

    Args:
        perf_report_stdout: Raw stdout from perf report command
        csv_out_path: Path where the sanitized CSV file will be written

    CSV columns: percent, count, context, symbol
    """
    PERF_ROW_REGEX = re.compile(
    r""" ^
         \s*(?P<pct>\d+(?:\.\d+)?)%     # percentage
         \s+(?P<count>\d+)              # sample count
         \s+\[(?P<context>[^\]]+)\]     # context marker
         \s+(?P<symbol>.+?)             # symbol name
         \s*(?:>|$)                     # perf draws a '>' gutter sometimes
       $ """,
    re.VERBOSE
    )

    rows = []
    for line in perf_report_stdout.splitlines():
        m = PERF_ROW_REGEX.match(line.rstrip())
        if not m:
            continue
        symbol = _clean_symbol(m.group("symbol"))
        rows.append({
            "percent": float(m.group("pct")),
            "count": int(m.group("count")),
            "context": m.group("context"),
            "symbol": symbol,
        })

    with open(csv_out_path, "w", newline="", encoding="utf-8") as csvfile:
        writer = csv.DictWriter(csvfile, fieldnames=["percent", "count", "context", "symbol"])
        writer.writeheader()
        for row in rows:
            writer.writerow(row)
    return

def benchmark_atperf_code_hotspots(workload, atperf_path, log_dir, data_dir):
    """
    Run the Code Hotspots benchmark using atperf and save sanitized results.

    Args:
        workload: Command to profile with atperf
        atperf_path: Path to the atperf binary
        log_dir: Directory to store raw log files
        data_dir: Directory to store sanitized data files

    Returns:
        True if benchmark completed successfully, False otherwise
    """
    kill_atperf_engine(atperf_path)
    logging.info("-=== Benchmarking atperf ===-")

    # Run recipe
    logging.info(f"Running recipe code_hotspots for workload: {workload}")
    args = f"recipe run code_hotspots {ATPERF_WORKLOAD_FLAG} \"{workload}\" {ATPERF_TARGET_FLAG} localhost {ATPERF_DEPLOY_TOOLS_FLAG}"
    result = invoke_atperf(args, path=atperf_path)
    run_id = extract_run_id(result)
    logging.info(f"Run ID: {run_id}")

    # Render Run
    logging.info("Creating render session...")
    args = f"run render {run_id} {ATPERF_JSON_FLAG}"
    result = invoke_atperf(args, path=atperf_path)
    render_session_id = extract_session_id(result)
    if render_session_id == "":
        logging.error(f"Failed to extract render session ID from atperf output.")
        return False
    logging.info(f"Render Session ID: {render_session_id}")

    # Query render
    logging.info("Querying render session...")
    args = f"render query {render_session_id} \"{ATPERF_CODE_HOTSPOTS_QUERY}\" {ATPERF_JSON_FLAG}"
    atperf_benchmark_results = invoke_atperf(args, path=atperf_path)
    if atperf_benchmark_results is None:
        logging.error("Failed to get benchmark results from atperf render query.")
        return False

    # Store results
    logging.info("Storing results...")
    outfile = log_dir / f"{ATPERF_NAME}_code_hotspots_results{JSON_EXTENSION}"
    outfile.parent.mkdir(parents=True, exist_ok=True)
    outfile.write_text(atperf_benchmark_results)

    # Sanitize results
    sanitized_outfile = data_dir / ATPERF_FILENAME
    sanitized_outfile.parent.mkdir(parents=True, exist_ok=True)
    sanitize_atperf_data(json.loads(atperf_benchmark_results), sanitized_outfile)

    # Teardown
    args = f"render close {render_session_id}"
    result = invoke_atperf(args, path=atperf_path)
    logging.info(f"atperf results written to '{sanitized_outfile}'")
    return True

def benchmark_perf_code_hotspots(workload, log_dir, data_dir):
    """
    Run the Code Hotspots benchmark using perf and save sanitized results.

    Args:
        workload: Command to profile with perf
        log_dir: Directory to store raw log files (perf.data)
        data_dir: Directory to store sanitized data files
    """
    logging.info("-=== Benchmarking perf ===-")

    # Ensure log_dir exists
    log_dir.mkdir(parents=True, exist_ok=True)
    perf_data_path = log_dir / "perf.data"

    # Run perf record
    args = f"record -F 1000 -a -o {perf_data_path} -- {workload}"
    time_perf(args)
    logging.info(f"perf.data written to '{perf_data_path}'")

    # Run perf report using the perf.data in log_dir
    args = f"report -i {perf_data_path} --percent-limit 0 --stdio -g none -s symbol -n"
    perf_results, _, _ = time_perf(args)

    # Sanitize perf results
    sanitized_outfile = data_dir / PERF_FILENAME
    sanitize_perf_data(perf_results, sanitized_outfile)
    logging.info(f"perf results written to '{sanitized_outfile}'")
    return

CODE_HOTSPOTS_ACCURACY_BENCHMARK_NAME = "code_hotspots_accuracy"
class CodeHotspotsAccuracyBenchmark(Benchmark):
    name = CODE_HOTSPOTS_ACCURACY_BENCHMARK_NAME
    description = "Compares Code Hotspots recipe metrics from atperf vs perf"

    def __init__(self):
        super().__init__()

    @staticmethod
    def add_arguments(parser):
        """
        Add command-line arguments for the Code Hotspots accuracy benchmark.

        Args:
            parser: ArgumentParser instance to add arguments to
        """
        add_common_benchmark_args(parser)

    def run(self, args):
        """
        Execute the Code Hotspots accuracy benchmark.

        Runs atperf and perf on specified workloads, compares results,
        and generates comparison reports.

        Args:
            args: Parsed command-line arguments containing workloads, n_runs, and atperf_path

        Returns:
            True if benchmark completed successfully, False otherwise
        """
        # Setup
        atperf_path = args.atperf_path
        n_runs = args.n_runs
        workloads = args.workloads or []
        if not workloads:
            logging.error("No workloads specified. Provide at least one -w WORKLOAD.")
            return False
        atperf_prepare_target("localhost", atperf_path=atperf_path)

        # Create output directory
        logging.info(f"Running Code Hotspots Accuracy Benchmark with {n_runs} runs on workloads: {workloads}")
        for workload in workloads:
            workload_slug = slugify_workload(workload)
            workload_results_dir = RESULTS / f"{workload_slug}"
            logging.info(f"=== Processing workload: {workload} ===")
            for run_idx in range(1, n_runs + 1):
                logging.info(f"--- Run {run_idx}/{n_runs} ---")
                # Create directory for this run
                run_log_dir = LOGS / f"{workload_slug}" / f"run_{run_idx}"
                run_data_dir = SANITIZED_TOOL_DATA / f"{workload_slug}" / f"run_{run_idx}"
                for d in (run_log_dir, run_data_dir, workload_results_dir):
                    d.mkdir(parents=True, exist_ok=True)

                # Run atperf Code Hotspots
                benchmark_atperf_code_hotspots(workload, atperf_path, run_log_dir, run_data_dir)

                # Run perf
                benchmark_perf_code_hotspots(workload, run_log_dir, run_data_dir)

            if n_runs > 1:
                average_runs(workload_slug, n_runs, SANITIZED_TOOL_DATA / workload_slug, workload_results_dir)
        return True

    def report(self, args) -> BenchmarkMetadata:
        workloads = args.workloads or []
        reports_dir = pathlib.Path(REPORTS)
        reports_dir.mkdir(parents=True, exist_ok=True)
        results_root = pathlib.Path(RESULTS)
        results_root.mkdir(parents=True, exist_ok=True)

        summary: dict[str, dict[str, float]] = {}
        thresholds = [1.0, 3.0, 5.0]

        def pick_quality(coverage_pct: float) -> QualityLevel:
            """RAG on the 3%% coverage band."""
            if coverage_pct >= 90.0:
                return QualityLevel.GOOD
            if coverage_pct >= 85.0:
                return QualityLevel.MODERATE
            return QualityLevel.POOR

        for workload in workloads:
            workload_slug = slugify_workload(workload)
            workload_results_dir = results_root / workload_slug
            workload_results_dir.mkdir(parents=True, exist_ok=True)

            if args.n_runs and int(args.n_runs) > 1:
                perf_path = workload_results_dir / f"{workload_slug}_perf_avg.csv"
                atperf_path = workload_results_dir / f"{workload_slug}_atperf_avg.csv"
                # Profile variability (perf/atperf): stddev of per-run L1 distance to mean profile
                perf_mean_d, _ = profile_variability(
                    int(args.n_runs),
                    SANITIZED_TOOL_DATA / workload_slug,
                    PERF_FILENAME,
                    key_field="symbol",
                    pct_field="percent",
                )
                atperf_mean_d, _ = profile_variability(
                    int(args.n_runs),
                    SANITIZED_TOOL_DATA / workload_slug,
                    ATPERF_FILENAME,
                    key_field="function_name",
                    pct_field="periodic_samples_self_percent",
                )
            else:
                run_data_dir = SANITIZED_TOOL_DATA / workload_slug / "run_1"
                perf_path = run_data_dir / PERF_FILENAME
                atperf_path = run_data_dir / ATPERF_FILENAME
                perf_mean_d = atperf_mean_d = 0.0

            if not perf_path.exists() or not atperf_path.exists():
                self.metadata.add_warning(
                    f"Missing data for workload '{workload_slug}' (expected {perf_path} and {atperf_path})."
                )
                continue

            # Load the data and compute error rates
            perf_pct = load_perf_self_percent(perf_path)
            atperf_pct = load_atperf_self_percent(atperf_path)
            errors = compute_sample_error_rates(perf_pct, atperf_pct)

            # Calculate summary coverage within various thresholds. For any matching symbols within the threshold
            # error rate, sum their perf percentages to get coverage. We use perf percentages as weights since they
            # represent the "ground truth" distribution of samples.
            within_raw = {t: sum_perf_percent_within_threshold(errors, t) for t in thresholds}

            coverage = {
                1.0: round_percentage(within_raw.get(1.0, 0.0)),
                3.0: round_percentage(within_raw.get(3.0, 0.0)),
                5.0: round_percentage(within_raw.get(5.0, 0.0)),
            }
            quality_by_threshold = {t: pick_quality(within_raw.get(t, 0.0)) for t in thresholds}

            summary[workload_slug] = {
                "coverage": {
                    "within_1_pct": coverage[1.0],
                    "within_3_pct": coverage[3.0],
                    "within_5_pct": coverage[5.0],
                },
                "quality": {
                    "within_1_pct": quality_by_threshold[1.0].value,
                    "within_3_pct": quality_by_threshold[3.0].value,
                    "within_5_pct": quality_by_threshold[5.0].value,
                },
                "perf_variability_l1_mean": round_percentage(perf_mean_d),
                "atperf_variability_l1_mean": round_percentage(atperf_mean_d),
            }

        # Final JSON summary report
        summary_path = reports_dir / "code_hotspots_summary.json"
        summary_payload = {
            "workloads": summary,
        }
        summary_path.write_text(json.dumps(summary_payload, indent=2))
        logging.info(f"Code Hotspots summary report saved to: {summary_path}")

        self.metadata.add_data("code_hotspots_error_summary", summary_payload)
        return self.metadata
