# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
Shared performance quality checks for AI Insights evaluation attempts.
Designed to align with core/scripts/atperf-benchmark-reporting.
"""

from __future__ import annotations

from dataclasses import dataclass
import json
import math
from pathlib import Path
from typing import Any


# Performance quality thresholds are defined for production Performix MCP only;
# REST and Hackathon MCP are comparison modes and we don't track their performance.
PERFORMIX_MCP_MODE = "performix_mcp"
PERFORMANCE_EVALUATED_MODES = frozenset({PERFORMIX_MCP_MODE})
QUALITY_GOOD = "GOOD"
QUALITY_POOR = "POOR"
QUALITY_INDETERMINABLE = "INDETERMINABLE"
THRESHOLD_FIELD = "performance_thresholds"
REQUIRED_THRESHOLD_KEYS = ("duration_seconds", "input_tokens", "output_tokens")
PERFORMANCE_EVALUATED_PROPERTY = "ai_performance_evaluated"


@dataclass(frozen=True)
class MetricProperties:
    key: str
    label: str
    value_property: str
    threshold_property: str
    quality_property: str


METRIC_PROPERTIES = (
    MetricProperties(
        key="duration_seconds",
        label="runtime",
        value_property="ai_agent_duration_seconds",
        threshold_property="ai_performance_duration_threshold_seconds",
        quality_property="ai_performance_duration_quality",
    ),
    MetricProperties(
        key="input_tokens",
        label="input tokens",
        value_property="ai_input_tokens",
        threshold_property="ai_performance_input_token_threshold",
        quality_property="ai_performance_input_tokens_quality",
    ),
    MetricProperties(
        key="output_tokens",
        label="output tokens",
        value_property="ai_output_tokens",
        threshold_property="ai_performance_output_token_threshold",
        quality_property="ai_performance_output_tokens_quality",
    ),
)


def is_performance_evaluated_mode(mode: str | None) -> bool:
    """Return whether AI Insights performance quality is evaluated for a mode."""
    return mode in PERFORMANCE_EVALUATED_MODES


@dataclass(frozen=True)
class MetricQuality:
    key: str
    label: str
    value: int | float | None
    threshold: int | float
    quality: str


def performance_metrics_for_attempt(
    attempt: dict[str, str],
    *,
    performance_thresholds: dict[str, dict[str, int | float]] | None = None,
) -> list[MetricQuality]:
    """Return threshold assessments for one performance-evaluated attempt.

    A testcase must provide all required performance thresholds in the manifest
    before any assessment is made. Missing or partial thresholds return an
    empty list so callers can omit quality output without treating the attempt
    as a failed or indeterminate performance check.
    """
    if not is_performance_evaluated_mode(attempt.get("ai_mode")):
        return []

    thresholds = thresholds_for_attempt(
        attempt,
        performance_thresholds=performance_thresholds,
    )
    if thresholds is None:
        return []
    return [
        MetricQuality(
            key=properties.key,
            label=properties.label,
            value=number(attempt.get(properties.value_property)),
            threshold=thresholds[properties.key],
            quality=threshold_quality(
                number(attempt.get(properties.value_property)),
                thresholds[properties.key],
            ),
        )
        for properties in METRIC_PROPERTIES
    ]


def performance_failures_for_attempt(
    attempt: dict[str, str],
    *,
    performance_thresholds: dict[str, dict[str, int | float]] | None = None,
) -> list[MetricQuality]:
    """Return configured performance metrics that are not GOOD."""
    if not is_performance_evaluated_mode(attempt.get("ai_mode")):
        return []

    return [
        metric
        for metric in performance_metrics_for_attempt(
            attempt,
            performance_thresholds=performance_thresholds,
        )
        if metric.quality != QUALITY_GOOD
    ]


def recorded_performance_properties(metrics: list[MetricQuality]) -> list[tuple[str, str]]:
    """Serialize one pytest performance assessment as JUnit properties."""
    properties = [(PERFORMANCE_EVALUATED_PROPERTY, str(bool(metrics)).lower())]
    metrics_by_key = {metric.key: metric for metric in metrics}
    for metric_properties in METRIC_PROPERTIES:
        metric = metrics_by_key.get(metric_properties.key)
        if metric is None:
            continue
        properties.extend(
            [
                (metric_properties.threshold_property, str(metric.threshold)),
                (metric_properties.quality_property, metric.quality),
            ]
        )
    return properties


def has_recorded_performance_evaluation(attempt: dict[str, str]) -> bool:
    """Return whether an attempt records a completed performance assessment."""
    return attempt.get(PERFORMANCE_EVALUATED_PROPERTY, "").lower() == "true"


def recorded_performance_metrics_for_attempt(attempt: dict[str, str]) -> list[MetricQuality]:
    """Read the performance assessment recorded by pytest for one attempt."""
    if not has_recorded_performance_evaluation(attempt):
        return []

    metrics = []
    for properties in METRIC_PROPERTIES:
        threshold = number(attempt.get(properties.threshold_property))
        quality = attempt.get(properties.quality_property, "")
        if threshold is None or quality not in {
            QUALITY_GOOD,
            QUALITY_POOR,
            QUALITY_INDETERMINABLE,
        }:
            raise ValueError(f"incomplete recorded performance assessment for {properties.key}")
        metrics.append(
            MetricQuality(
                key=properties.key,
                label=properties.label,
                value=number(attempt.get(properties.value_property)),
                threshold=threshold,
                quality=quality,
            )
        )
    return metrics


def recorded_performance_failures_for_attempt(attempt: dict[str, str]) -> list[MetricQuality]:
    """Return recorded performance metrics that pytest classified as not GOOD."""
    return [
        metric
        for metric in recorded_performance_metrics_for_attempt(attempt)
        if metric.quality != QUALITY_GOOD
    ]


def thresholds_for_attempt(
    attempt: dict[str, str],
    *,
    performance_thresholds: dict[str, dict[str, int | float]] | None = None,
) -> dict[str, int | float] | None:
    """Return a testcase's complete threshold set, if one is available."""
    thresholds_by_test_id = performance_thresholds or {}
    thresholds = thresholds_by_test_id.get(attempt.get("ai_test_id", ""), {})
    if all(key in thresholds for key in REQUIRED_THRESHOLD_KEYS):
        return dict(thresholds)
    return None


def threshold_quality(value: int | float | None, threshold: int | float) -> str:
    """Classify one observed metric value against its threshold."""
    if value is None:
        return QUALITY_INDETERMINABLE
    return QUALITY_GOOD if value <= threshold else QUALITY_POOR


def quality_distribution(values: list[str]) -> dict[str, int]:
    """Return integer percentage buckets for benchmark-reporting quality data."""
    counts = {
        QUALITY_GOOD: 0,
        QUALITY_POOR: 0,
        QUALITY_INDETERMINABLE: 0,
    }
    for value in values:
        counts[value if value in counts else QUALITY_INDETERMINABLE] += 1
    total = sum(counts.values())
    if total == 0:
        return {
            QUALITY_GOOD: 0,
            QUALITY_POOR: 0,
            QUALITY_INDETERMINABLE: 100,
        }
    return {key: int(round((count / total) * 100)) for key, count in counts.items()}


def performance_thresholds_from_manifest(manifest_path: Path) -> dict[str, dict[str, int | float]]:
    """Read per-test performance thresholds from ai_insights_evaluation.json.

    Threshold blocks are optional so new tests can be added before performance
    limits are agreed. When present, each field is validated, but completeness is
    enforced later by ``thresholds_for_attempt`` so partial blocks simply skip
    quality assessment instead of breaking the whole reporting step.
    """
    if not manifest_path.exists():
        return {}

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    tests = manifest.get("tests", [])
    if not isinstance(tests, list):
        raise ValueError(f"{manifest_path}: 'tests' must be a list")

    thresholds_by_test_id: dict[str, dict[str, int | float]] = {}
    for test in tests:
        if not isinstance(test, dict) or THRESHOLD_FIELD not in test:
            continue
        test_id = test.get("id")
        raw_thresholds = test[THRESHOLD_FIELD]
        if not isinstance(test_id, str) or not test_id:
            raise ValueError(f"{manifest_path}: threshold override is missing a test id")
        if not isinstance(raw_thresholds, dict):
            raise ValueError(f"{manifest_path}: {test_id}.{THRESHOLD_FIELD} must be an object")

        unknown_keys = sorted(set(raw_thresholds) - set(REQUIRED_THRESHOLD_KEYS))
        if unknown_keys:
            raise ValueError(
                f"{manifest_path}: {test_id}.{THRESHOLD_FIELD} has unknown keys: {', '.join(unknown_keys)}"
            )

        parsed = {}
        for key in REQUIRED_THRESHOLD_KEYS:
            if key in raw_thresholds:
                parsed[key] = _manifest_threshold(raw_thresholds[key], manifest_path, test_id, key)
        if parsed:
            thresholds_by_test_id[test_id] = parsed
    return thresholds_by_test_id


def number(value: str | None) -> int | float | None:
    """Parse a non-negative numeric property recorded by pytest."""
    if value is None or str(value).strip() == "":
        return None
    try:
        parsed = float(str(value))
    except ValueError:
        return None
    if not math.isfinite(parsed) or parsed < 0:
        return None
    return int(parsed) if parsed.is_integer() else parsed


def _manifest_threshold(value: Any, manifest_path: Path, test_id: str, key: str) -> int | float:
    """Validate and normalize one manifest threshold value."""
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError(f"{manifest_path}: {test_id}.{THRESHOLD_FIELD}.{key} must be a number")
    if not math.isfinite(float(value)) or value < 0:
        raise ValueError(f"{manifest_path}: {test_id}.{THRESHOLD_FIELD}.{key} must be non-negative")
    return int(value) if float(value).is_integer() else float(value)
