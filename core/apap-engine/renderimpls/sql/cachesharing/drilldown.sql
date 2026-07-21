-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE "%s" AS
WITH base AS (
  SELECT
    DENSE_RANK() OVER (ORDER BY CAST(cacheline_address AS UBIGINT), byte_offset, symbol, image, symbol_id) AS call_tree_id,
    symbol_id,
    sample_count AS "perf.c2c.samples",
    coherence_sample_count AS "perf.c2c.coherence_samples",
    store_sample_count AS "perf.c2c.store_samples",
    CAST(cacheline_address AS UBIGINT) AS "perf.c2c.cacheline_address",
    byte_offset AS "perf.c2c.byte_offset",
    thread_count AS "perf.c2c.thread_count",
    writer_thread_count AS "perf.c2c.writer_thread_count"
  FROM "%s"
),
unpivoted AS (
  UNPIVOT base
  ON COLUMNS (* EXCLUDE (call_tree_id, symbol_id))
  INTO
    NAME identifier
    VALUE measurement_value
)
SELECT
  u.call_tree_id,
  NULL AS call_tree_parent_id,
  'function' AS node_type,
  CAST(u.measurement_value AS DOUBLE) AS measurement_value,
  u.symbol_id,
  r.measurement_id
FROM unpivoted u
JOIN ref_measurements r
  ON r.identifier = u.identifier
