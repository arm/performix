# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import argparse
import datetime
import json
import sys
from pathlib import Path
import os
from typing import Dict, Any, List, Optional

from parse_results import (
    eprintln,
    extract_and_parse_zip,
    ResultsSet,
)
from sinks.slack import send_blocks as slack_send_blocks


def utc_timestamp_for_artifactory() -> str:
    # Must match: date -u +'%Y-%m-%d_%H-%M-%SZ'
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d_%H-%M-%SZ")


def _join_url(base: str, path: str) -> str:
    base = (base or "").rstrip("/")
    path = (path or "").lstrip("/")
    return f"{base}/{path}" if base else path


def _resolve_artifactory_urls(summary_filename: str) -> tuple[Optional[str], Optional[str]]:
    """Return (folder_url, file_url) if UPLOAD_DESTINATION is configured."""
    base_url = os.environ.get("ARTIFACTORY_BASE_URL") or "https://artifactory.arm.com/ui/repos/tree/General/"
    dest = os.environ.get("UPLOAD_DESTINATION") or os.environ.get("ARTIFACTORY_UPLOAD_DESTINATION")
    if not dest:
        return None, None

    if not dest.endswith("/"):
        dest = dest + "/"

    # Match CI upload layout with use of --flat=false: UPLOAD_DESTINATION + 'benchmarking-results/'
    results_prefix = "benchmarking-results/"
    folder_url = _join_url(base_url, dest + results_prefix)
    file_url = _join_url(base_url, dest + results_prefix + summary_filename)
    return folder_url, file_url


def main(argv: List[str]) -> int:
    ap = argparse.ArgumentParser(description="Performix Core benchmark-reporting")
    ap.add_argument(
        "--results-dir",
        required=True,
        help="Directory containing downloaded result zip artifacts (top-level).")
    ap.add_argument(
        "--slack-webhook-url",
        required=False,
        help="Slack Incoming Webhook URL. If omitted, reads SLACK_WEBHOOK_URL env var.",
    )
    ap.add_argument(
        "--dry-run",
        choices=("true", "false"),
        required=True,
        help="Boolean string ('true' or 'false'). If false, send message to Slack. If true, print message to console.",
    )
    args = ap.parse_args(argv)

    # Explicitly convert dry-run string to bool
    dry_run: bool = args.dry_run == "true"

    results_dir = Path(args.results_dir).expanduser().resolve()
    if not results_dir.exists() or not results_dir.is_dir():
        eprintln(f"[ERROR] results dir not found or not a directory: {results_dir}")
        return 2

    work_dir = results_dir / "_extracted"
    work_dir.mkdir(parents=True, exist_ok=True)

    # Find downloaded test result zip artifacts in results_dir.
    outer_zips = sorted(results_dir.glob("results_*.zip"))
    if not outer_zips:
        eprintln(f"[ERROR] No zip files found under: {results_dir}")

    aggregated_results: List[Dict[str, Any]] = []

    for outer_zip in outer_zips:
        try:
            rs: ResultsSet = extract_and_parse_zip(outer_zip, work_dir)
            aggregated_results.append(
                {
                    "benchmarking_run_id": rs.benchmarking_run_id,
                    "results": [
                        {
                            "results_root": str(p.results_root),
                            "metadata": p.metadata,
                            "reports": p.reports,
                            "sysreport": getattr(p, "sysreport", {}),
                        }
                        for p in rs.results
                    ],
                    "errors": rs.errors,
                }
            )
        except Exception as ex:
            msg = f"Failed processing artifact zip {outer_zip}: {ex}"
            eprintln(f"[ERROR] {msg}")
            aggregated_results.append(
                {
                    "benchmarking_run_id": outer_zip.name,
                    "results": [],
                    "errors": [msg],
                }
            )

    # If we parsed zero payloads, fail fast so CI marks this step as failed
    total_payloads = sum(len(rs_dict.get("results", [])) for rs_dict in aggregated_results)
    if (not outer_zips) or (total_payloads == 0):
        if not outer_zips:
            eprintln("[ERROR] No results artifacts (results_*.zip) were found; failing CI")
        else:
            eprintln("[ERROR] Parsed 0 payloads from results artifacts; failing CI")
        print(json.dumps({"aggregated_results": aggregated_results}, indent=2))
        return 1

    # Construct CI run URL when running inside GitHub Actions
    server = os.environ.get("GITHUB_SERVER_URL")
    repo = os.environ.get("GITHUB_REPOSITORY")
    run_id = os.environ.get("GITHUB_RUN_ID")
    run_url = f"{server}/{repo}/actions/runs/{run_id}" if server and repo and run_id else None

    # Evaluate data quality across all parsed payloads and decide whether to alert
    def get_dqd(metadata: Dict[str, Any]) -> Dict[str, Any]:
        dqd = metadata.get("data_quality_distribution")
        if isinstance(dqd, dict):
            return dqd
        return {}

    def has_poor_or_indeterminable(dqd: Dict[str, Any]) -> bool:
        # Treat missing distribution as INDETERMINABLE present
        if not dqd:
            return True
        try:
            poor = float(dqd.get("POOR", 0.0))
        except Exception:
            poor = 0.0
        try:
            indeterminable = float(dqd.get("INDETERMINABLE", 0.0))
        except Exception:
            indeterminable = 0.0
        return (poor > 0.0) or (indeterminable > 0.0)

    # Build list of test payloads with poor/indeterminable data quality.
    test_alerts: List[Dict[str, Any]] = []
    for rs_dict in aggregated_results:
        test_run_id = rs_dict.get("benchmarking_run_id")
        for p in rs_dict.get("results", []):
            metadata: Dict[str, Any] = p.get("metadata", {})
            dqd = get_dqd(metadata)
            if has_poor_or_indeterminable(dqd):
                test_name = metadata.get("benchmark_name") or "unknown-test"
                workloads = metadata.get("workloads") or []
                if isinstance(workloads, list) and workloads and isinstance(workloads[0], dict):
                    workload_exec = workloads[0].get("executable") or "unknown-executable"
                else:
                    workload_exec = "unknown-executable"
                sysinfo: Dict[str, Any] = p.get("sysreport", {}) or {}
                cpu_types = sysinfo.get("cpu_types") or "unknown-cpu"
                distribution = sysinfo.get("distribution") or "unknown-os"
                test_alerts.append(
                    {
                        "run_id": test_run_id,
                        "test_name": test_name,
                        "workload_executable": workload_exec,
                        "data_quality_distribution": dqd,
                        "cpu_types": cpu_types,
                        "distribution": distribution,
                    }
                )

    # Always write the summary file (even if there are no alerts)
    ts = os.environ.get("ARTIFACTORY_UPLOAD_TS") or utc_timestamp_for_artifactory()
    summary_filename = f"test_results_summary_{ts}"
    summary_path = results_dir / summary_filename
    folder_url, file_url = _resolve_artifactory_urls(summary_filename)

    summary_lines: List[str] = []
    if test_alerts:
        header_text = "Performix Core test alert (benchmarking & MCP performance) - runs with POOR or INDETERMINABLE results detected."
        summary_lines.append(header_text)
        summary_lines.append("")
        summary_lines.append(f"Affected test runs: {len(test_alerts)}")
        summary_lines.append("")

        lines: List[str] = []
        lines.append(f"*Affected test runs:* {len(test_alerts)}")
        lines.append("")
        for idx, a in enumerate(test_alerts, start=1):
            dqd = a["data_quality_distribution"]
            lines.append(f"*Test run #{idx} with POOR or INDETERMINABLE results*")
            lines.append(f"• Test name: {a['test_name']}")
            lines.append(f"• Workload executable: `{a['workload_executable']}`")
            lines.append(f"• Target system hardware CPU types: {a['cpu_types']}")
            lines.append(f"• Target system OS distribution: {a['distribution']}")
            lines.append("• RAG status of metrics measured by the test:")
            lines.append(f"  - GOOD: {dqd.get('GOOD', 0)}%")
            lines.append(f"  - MODERATE: {dqd.get('MODERATE', 0)}%")
            lines.append(f"  - POOR: {dqd.get('POOR', 0)}%")
            lines.append(f"  - INDETERMINABLE: {dqd.get('INDETERMINABLE', 0)}%")
            lines.append(
                "MODERATE or GOOD RAG indicators pass configured thresholds, POOR does not. "
                "INDETERMINABLE indicates missing or invalid data."
            )
            lines.append("")
        detail_text = "\n".join(lines)
    else:
        header_text = "Performix Core test report (benchmarking & MCP performance) - no POOR or INDETERMINABLE results detected."
        summary_lines.append(header_text)
        summary_lines.append("")
        summary_lines.append("No alerts were detected.")
        summary_lines.append("There were no test runs with POOR or INDETERMINABLE data quality.")
        summary_lines.append("")
        detail_text = "No alerts."

    summary_lines.append(f"Timestamp (UTC): {ts}")
    if run_url:
        summary_lines.append(f"CI run: {run_url}")
    if folder_url:
        summary_lines.append(f"Artifactory folder: {folder_url}")
    summary_lines.append("")
    summary_lines.append(detail_text)
    summary_path.write_text("\n".join(summary_lines) + "\n", encoding="utf-8")
    eprintln(f"[INFO] Wrote Performix Core test summary: {summary_path}")

    # Slack is only used for alerting; keep it short and only post when alerts exist.
    slack_webhook = args.slack_webhook_url or os.environ.get("SLACK_WEBHOOK_URL")
    if not test_alerts:
        if dry_run:
            print("[DRY RUN] No alerts; no Slack message would be sent.")
            print("[DRY RUN] Summary written to: " + str(summary_path))
        else:
            eprintln("[INFO] No payloads have POOR or INDETERMINABLE data quality; skipping Slack alert")
    else:
        slack_lines: List[str] = []
        slack_lines.append(header_text)
        slack_lines.append(f"Affected test runs: {len(test_alerts)}")
        slack_lines.append(f"Summary file: `{summary_filename}`")
        if file_url:
            slack_lines.append(f"Summary of test results on Artifactory: <{file_url}|Open summary>")
        elif folder_url:
            slack_lines.append(f"Test results folder on Artifactory: <{folder_url}|Open results folder>")
        if run_url:
            slack_lines.append(f"GitHub CI artifacts: <{run_url}|View CI run artifacts>")

        blocks = [
            {"type": "section", "text": {"type": "mrkdwn", "text": "\n".join(slack_lines)}},
        ]

        if dry_run:
            print("[DRY RUN] Slack message:\n" + "\n".join(slack_lines))
            print("\n[DRY RUN] Summary written to:\n" + str(summary_path))
        else:
            if not slack_webhook:
                eprintln("[INFO] Slack webhook not configured; skipping Slack post")
            else:
                ok = slack_send_blocks(slack_webhook, blocks)
                if not ok:
                    eprintln("[WARN] Failed to send Slack alert via webhook")

    print(json.dumps({"aggregated_results": aggregated_results}, indent=2))

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
