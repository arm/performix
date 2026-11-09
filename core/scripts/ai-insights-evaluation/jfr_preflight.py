# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Check that saved JFR runs expose the tables required by AI evaluation.

Run this check after an attempt imports its run and before it invokes AI Insights,
so missing JFR tables are reported as input failures.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path
from typing import Any

import pytest

sys.path.append(str(Path(__file__).resolve().parents[1]))
from run_export_helper import run_cli


REQUIRED_JFR_RENDERERS = (
    "jfr_recordings",
    "jvm_garbage_collections",
    "jvm_heap_summary",
    "jvm_info_raw",
    "jvm_system_properties",
    "jvm_flags",
)


def _jfr_render_tables(
    invocation: dict[str, Any], test_id: str,
) -> dict[str, str]:
    """Resolve required JFR renderer IDs to their rendered table names."""
    entries = invocation.get("manifest", {}).get("entry") or []
    tables = {
        entry.get("renderer_id", {}).get("value"): entry.get("table_name")
        for entry in entries
        if isinstance(entry, dict) and isinstance(entry.get("renderer_id"), dict)
    }
    missing = [renderer for renderer in REQUIRED_JFR_RENDERERS if not tables.get(renderer)]
    if missing:
        raise pytest.UsageError(
            f"JFR preflight failed for {test_id}: missing rendered tables: "
            f"{', '.join(missing)}"
        )
    return tables


def validate_jfr_run(
    test_id: str, run_id: str, cli_bin: Path, env: dict[str, str],
) -> None:
    """Ensure the imported run exposes the JFR tables needed by AI evaluation.

    Each required table must appear in the rendered manifest and accept a query.
    JVM identity, effective flags and initial properties belong to the same JFR
    dataset as heap/GC data. Require a parsed recording containing that evidence.
    """
    try:
        def cli_call(*args: str) -> dict[str, Any]:
            output = run_cli([str(cli_bin), *args, "--json"], env=env, timeout=120).stdout
            return json.loads(output)["data"]

        invocation = cli_call("run", "render", run_id)["invocation"]
        session_id = invocation["session_id"]
        try:
            tables = _jfr_render_tables(invocation, test_id)
            quoted = {key: '"' + name.replace('"', '""') + '"' for key, name in tables.items() if name}
            for renderer in REQUIRED_JFR_RENDERERS:
                cli_call(
                    "render", "query", session_id,
                    f"SELECT * FROM {quoted[renderer]} LIMIT 0",
                )
            result = cli_call("render", "query", session_id, f'''
                SELECT r."Recording"
                FROM {quoted["jfr_recordings"]} r
                WHERE r."Parse Complete" = 'true'
                  AND EXISTS (
                    SELECT 1 FROM {quoted["jvm_info_raw"]} i
                    WHERE i."Recording" = r."Recording" AND i."PID" = r."PID"
                      AND i."JVM Version" IS NOT NULL AND i."JVM Version" <> '')
                  AND EXISTS (
                    SELECT 1 FROM {quoted["jvm_flags"]} f
                    WHERE f."Recording" = r."Recording" AND f."Flag" = 'MaxHeapSize'
                      AND TRY_CAST(f."Value" AS HUGEINT) > 0
                      AND f."Origin" IS NOT NULL AND f."Origin" <> ''
                      AND f."Type" IS NOT NULL AND f."Type" <> '')
                  AND EXISTS (
                    SELECT 1 FROM {quoted["jvm_system_properties"]} p
                    WHERE p."Recording" = r."Recording" AND p."Value" IS NOT NULL)
                LIMIT 1
            ''')
            if not result["rows"]:
                raise pytest.UsageError(
                    f"JFR preflight failed for {test_id}: no parsed recording "
                    "has matching JVM identity, effective heap configuration and initial properties. "
                    "Refresh the pre-recorded run with current JFR capture."
                )
        finally:
            run_cli([str(cli_bin), "render", "close", session_id], env=env, timeout=120)
    except pytest.UsageError:
        raise
    except (
        OSError, RuntimeError, ValueError, KeyError, TypeError, IndexError,
        subprocess.TimeoutExpired,
    ) as exc:
        raise pytest.UsageError(
            f"JFR preflight failed for {test_id}: {exc}"
        ) from exc
