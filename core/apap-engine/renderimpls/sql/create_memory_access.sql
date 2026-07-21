-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE __RESULT_TABLE_NAME__ AS
WITH unpivot_intermediate AS (
    UNPIVOT __FLAT_TABLE_NAME__
    ON COLUMNS (* EXCLUDE ("Function", "Image", "symbol_id"))
    INTO
        NAME measurement_name
        VALUE measurement_value
)
SELECT
    DENSE_RANK() OVER (ORDER BY ui."Function", ui."Image", s.symbol_id) AS call_tree_id,
    NULL AS call_tree_parent_id,
    'function' AS node_type,
    s.symbol_id,
    m.measurement_id,
    ui.measurement_value
FROM unpivot_intermediate as ui
LEFT JOIN __MEASUREMENTS_TABLE_NAME__ AS m
    ON m.name = ui.measurement_name
LEFT JOIN __SYMBOLS_TABLE_NAME__ AS s
    ON s.__SYMBOLS_TABLE_JOIN_COL__ = ui.__FLAT_TABLE_JOIN_COL__