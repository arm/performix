# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import json
import os
import re
import statistics

from framework.quality_level import QualityLevel


def _cell_truthy(cell) -> bool:
    """Interpret common spreadsheet-like boolean values."""
    if cell is None:
        return False
    if isinstance(cell, bool):
        return cell
    if isinstance(cell, (int, float)):
        return cell != 0
    if isinstance(cell, str):
        return cell.strip().lower() in {"true", "1", "yes", "y"}
    return False


def round_to_3sf(value):
    """
    Round a number to 3 significant figures.
    Always returns a string.
    """
    try:
        num = float(value)
        if num == 0:
            return "0"
        return "{:.3g}".format(num)
    except (ValueError, TypeError):
        return str(value)

def round_percentage(value):
    """
    Round a number to 1 decimal place and return as float.
    """
    try:
        return round(float(value), 1)
    except (ValueError, TypeError):
        return value

def safe_float(val):
    if val is None or val == "":
        return None
    try:
        return float(val)
    except (ValueError, TypeError):
        return None

"""Parse a time string and return the value in milliseconds."""
def parse_time(timestr: str) -> int:
    timestr = timestr.strip()
    total_ms = 0
    pattern = r'(?:(\d+)\s*h)?\s*(?:(\d+(?:\.\d+)?)\s*ms)?\s*(?:(\d+)\s*m)?\s*(?:(\d+(?:\.\d+)?)\s*s)?'
    match = re.match(pattern, timestr)
    if not match:
        raise ValueError(f"Can't parse time: {timestr}")
    hours, ms, minutes, seconds = match.groups(default='0')
    total_ms += int(hours) * 3600 * 1000
    total_ms += float(minutes) * 60 * 1000
    total_ms += float(seconds) * 1000
    total_ms += float(ms)
    return int(total_ms)


def get_dir_size_mb(path):
    total = 0
    for dirpath, dirnames, filenames in os.walk(path):
        for f in filenames:
            fp = os.path.join(dirpath, f)
            if os.path.isfile(fp):
                total += os.path.getsize(fp)
    return total / (1024 * 1024)


"""
Extracts the run_id from plaintext output.
Looks for a line like: 'Run ID: <run_id>'
"""
def extract_run_id(plain_text: str) -> str:
    for line in plain_text.strip().splitlines():
        match = re.match(r"Run ID:\s*([a-fA-F0-9]+)", line.strip())
        if match:
            return match.group(1)
    return ""

"""
Extracts the session_id from the JSON output of 'run render'.
Returns the session_id string if found, else returns an empty string.
"""
def extract_session_id(json_text: str) -> str:
    try:
        data = json.loads(json_text)
        return data.get("data", {}).get("invocation", {}).get("session_id", "")
    except Exception:
        return ""

"""
Extracts a list of CPU Microarchitecture metric groups available on the target using atperf recipe info
"""
def extract_metric_groups(json_str) -> list:
    data = json.loads(json_str)
    params = data["data"]["parameters"]
    for param in params:
        if param["id"] == "metrics_group":
            return param["config"]["options"]
    return []


def mean(lst):
    """Return the mean of lst, or an empty string if not computable."""
    try:
        return statistics.mean(lst)
    except statistics.StatisticsError:
        return ''


def variance(lst):
    """Return population variance (divides by N) or '' when not computable."""
    try:
        return statistics.pvariance(lst)
    except statistics.StatisticsError:
        return ''


def stddev(lst):
    """Return population standard deviation or '' when not computable."""
    try:
        return statistics.pstdev(lst)
    except statistics.StatisticsError:
        return ''


def slugify_workload(workload: str) -> str:
    """Create a filesystem-friendly slug from the workload string."""
    slug = workload.lower().strip()
    slug = re.sub(r'[^a-z0-9]+', '-', slug)
    slug = re.sub(r'-{2,}', '-', slug).strip('-')
    return slug[:80]  # truncate for safety


def consolidate_column_quality(report_result, quality_column_name, ignored_column_name=None):
    """
    Determine overall quality distribution from BenchmarkResult.
    
    Args:
        report_result: BenchmarkResult object with headers and rows
        quality_column_name: Name of the column containing quality values
        ignored_column_name: (optional) Name of the column indicating whether a row is ignored

    Returns:
        A quality distribution, which is a dictionary of QualityLevel keys and float percentages of results with this QualityLevel as values.
    """
    from collections import Counter
    from framework.quality_level import QualityLevel, indeterminable_distribution

    # Find the quality column index
    try:
        quality_col_index = report_result.headers.index(quality_column_name)
        if ignored_column_name is not None:
            ignored_col_index = report_result.headers.index(ignored_column_name)
    except ValueError:
        return indeterminable_distribution()

    # Exit early if no results were collected
    total_rows = len(report_result.rows)
    if total_rows == 0:
        return indeterminable_distribution()

    normalize = {
        QualityLevel.GOOD.value: QualityLevel.GOOD,
        QualityLevel.MODERATE.value: QualityLevel.MODERATE,
        QualityLevel.POOR.value: QualityLevel.POOR,
        QualityLevel.INDETERMINABLE.value: QualityLevel.INDETERMINABLE,
    }
    normalized_values = []

    included_rows = 0

    for row in report_result.rows:
        if quality_col_index >= len(row):
            continue

        if ignored_column_name is not None:
            # If the ignored column exists in headers but the row doesn't have that cell, treat it as ignored.
            if ignored_col_index >= len(row) or _cell_truthy(row[ignored_col_index]):
                continue  # Skip ignored rows
        
        val = row[quality_col_index]
        try:
            s = str(val).strip().upper()
        except (AttributeError, ValueError, TypeError):
            s = ""
        normalized_values.append(normalize.get(s, QualityLevel.INDETERMINABLE))
        included_rows += 1

    counts = Counter(normalized_values)

    if included_rows == 0:
        return indeterminable_distribution()

    return {
        QualityLevel.GOOD: (counts.get(QualityLevel.GOOD, 0) / included_rows) * 100,
        QualityLevel.MODERATE: (counts.get(QualityLevel.MODERATE, 0) / included_rows) * 100,
        QualityLevel.POOR: (counts.get(QualityLevel.POOR, 0) / included_rows) * 100,
        QualityLevel.INDETERMINABLE: (counts.get(QualityLevel.INDETERMINABLE, 0) / included_rows) * 100,
    }
