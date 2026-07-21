# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# binaries
from pathlib import Path
import os

# Mirror of `apap-engine/terminology/terminology.json`
PRODUCT_FULL_NAME = "Arm Performix"
PRODUCT_BINARY_NAME = "apx"
AGENT_BINARY_NAME = "apx-agent"
DAEMON_DIR_NAME = "apxd"
ENV_VAR_PREFIX = "APXD"

DEFAULT_WORKLOAD = "/opt/deps/megabench 0"
DEFAULT_TOPDOWN_TOOL_PATH = "/opt/deps/telemetry-solution/tools/topdown_tool/topdown-tool"
DEFAULT_ATPERF_PATH = "/opt/deps/atperf/atperf"
PERF_TOOL_BINARY = "perf"

# environment
JSON_EXTENSION = ".json"
CSV_EXTENSION = ".csv"
ATPERF_NAME = "atperf"
TOPDOWN_TOOL_NAME = "topdown_tool"
OUTPUT_DIR = Path("./output")
RESULTS = OUTPUT_DIR / "results"
REPORTS = OUTPUT_DIR / "reports"
SANITIZED_TOOL_DATA = RESULTS / "sanitized_tool_data"
LOGS = OUTPUT_DIR / "logs"


_data_dir = os.environ.get(f"{ENV_VAR_PREFIX}_DATA_DIR")
RUN_DIR_WINDOWS = Path(_data_dir) / "runs" if _data_dir is not None else str(Path.home() / "AppData" / "Local" / DAEMON_DIR_NAME / "runs")
RUN_DIR_LINUX = Path(_data_dir) / "runs"  if _data_dir is not None else str(Path.home() / ".local" / "share" / DAEMON_DIR_NAME / "runs")
RUN_DIR_MACOS = Path(_data_dir) / "runs" if _data_dir is not None else str(Path.home() / ".local" / "share" / DAEMON_DIR_NAME / "runs")

# atperf flags/args
ATPERF_METRIC_MODE_FLAG = "--param=metric_mode="
ATPERF_METRIC_GROUPS_FLAG = "--param=metrics_group="
ATPERF_TARGET_FLAG = "--target"
ATPERF_JSON_FLAG = "--json"
ATPERF_WORKLOAD_FLAG = "--workload"
ATPERF_DEPLOY_TOOLS_FLAG = "--deploy-tools"
LOCALHOST_TARGET = "localhost"
ATPERF_TOPDOWN_QUERY = """SELECT
    dm1.NAME AS metric,
    dm1.UNITS AS units,
    d1.MEASUREMENT_VALUE AS value
FROM
    drilldown_1 d1
JOIN
    drilldown_measurements_1 dm1
    ON d1.MEASUREMENT_ID = dm1.MEASUREMENT_ID
WHERE
    d1.CALL_TREE_ID = 0
"""

# measurement_id = 1: periodic_samples_total
# measurement_id = 2: periodic_samples_self
# measurement_id = 3: periodic_samples_self_percent
# measurement_id = 4: periodic_samples_total_percent
ATPERF_CODE_HOTSPOTS_QUERY = """
WITH per_node AS (
  SELECT
    COALESCE(NULLIF(s.name, ''), 'EMPTY_SYMBOLS') AS function_name,
    d.node_type,
    d.call_tree_id,
    MAX(CASE WHEN m.name = 'Periodic Samples (self)' THEN d.measurement_value ELSE 0 END) AS periodic_samples_self,
    MAX(CASE WHEN m.name = 'Periodic Samples (self) - percentage' THEN d.measurement_value ELSE 0 END) AS periodic_samples_self_percent
  FROM drilldown_1 d
  LEFT JOIN symbols s
    ON d.symbol_id = s.symbol_id
  LEFT JOIN drilldown_measurements_1 m
    ON d.measurement_id = m.measurement_id
  GROUP BY d.call_tree_id, s.name, d.node_type
),
agg AS (
  SELECT
    function_name,
    node_type,
    SUM(periodic_samples_self)         AS periodic_samples_self,
    SUM(periodic_samples_self_percent) AS periodic_samples_self_percent
  FROM per_node
  GROUP BY function_name, node_type
)
SELECT
  a.function_name,
  a.node_type,
  a.periodic_samples_self,
  a.periodic_samples_self_percent
FROM agg a
ORDER BY a.periodic_samples_self DESC;
"""

# topdown_tool flags/args
TOPDOWN_TOOL_METRIC_GROUP_FLAG = "--cpu-metric-group"
TOPDOWN_TOOL_CSV_FLAG = "--csv-output-path"
TOPDOWN_TOOL_PROBE_FLAG = "--probe"
TOPDOWN_TOOL_PROBE_LIST_FLAG = "--probe-list"
TOPDOWN_TOOL_GENERATE_CSV_FLAG = "--cpu-generate-csv metrics"

# sl-record flags
SL_RECORD_TIMEOUT_FLAG = "-t"
SL_RECORD_SAMPLE_RATE_FLAG = "-r"
SL_RECORD_METRICS_GROUPS_FLAG = "-M"
SL_RECORD_OUTPUT_FLAG = "-o"
SL_RECORD_WORKLOAD_FLAG = "-A"
SL_RECORD_DEBUG_LOGGING_FLAG = "-d"

# perf flags
PERF_OUTPUT_FLAG = "-o"

# perf default args for collector_overhead benchmark (Neoverse-V2 only)
PERF_DEFAULT_ARGS_NEOVERSE_V2 = "-B -P -T --sample-cpu --call-graph fp --user-callchains -c 991088 -e '{r11/period=991088/u,r3a/period=495544/u,r3b/period=495544/u,r3e/period=495544/u,r10/period=61943/u,r8/period=495544/u,r3d/period=495544/u},{r11/period=991088/u,r1b/period=495544/u,r8/period=495544/u,r3b/period=495544/u,r3f/period=495544/u,r3a/period=495544/u,r10/period=61943/u},{r11/period=991088/u,r7d/period=495544/u,r7c/period=495544/u,r24/period=495544/u,r7e/period=495544/u,r23/period=495544/u,r1b/period=495544/u},{r11/period=991088/u,r21/period=247772/u,r1b/period=495544/u,r7a/period=123886/u,r78/period=123886/u,r8/period=495544/u,r22/period=61943/u},{r11/period=991088/u,r26/period=247772/u,r14/period=247772/u,r1/period=61943/u,r35/period=61943/u,r8/period=495544/u,r2/period=61943/u},{r11/period=991088/u,r34/period=61943/u,r42/period=61943/u,r43/period=61943/u,r8/period=495544/u,r3/period=61943/u,r4/period=247772/u},{r11/period=991088/u,r52/period=61943/u,r53/period=61943/u,r5/period=61943/u,r8/period=495544/u,r25/period=247772/u,r34/period=61943/u},{r11/period=991088/u,r37/period=61943/u,r2a/period=61943/u,r8/period=495544/u,r17/period=61943/u,r16/period=247772/u,r2b/period=247772/u},{r11/period=991088/u,r36/period=247772/u,r37/period=61943/u,r2f/period=247772/u,r8/period=495544/u,r2d/period=61943/u},{r11/period=991088/u,r73/period=495544/u,r8018/period=495544/u,r77/period=495544/u,r8014/period=495544/u,r1b/period=495544/u,r801c/period=495544/u},{r11/period=991088/u,r75/period=495544/u,r90/period=495544/u,r1b/period=495544/u,r6c/period=495544/u,r70/period=495544/u,r91/period=495544/u},{r11/period=991088/u,r6e/period=495544/u,r6f/period=495544/u,r1b/period=495544/u,r8006/period=495544/u,r74/period=495544/u,r71/period=495544/u}' --intr-regs=x0,x1,x2,x3,x4,x5,x6,x7,x8,x9,x10,x11,x12,x13,x14,x15,x16,x17,x18,x19,x20,x21,x22,x23,x24,x25,x26,x27,x28,x29,lr,sp,pc"