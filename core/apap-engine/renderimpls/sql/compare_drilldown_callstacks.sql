-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE __DELTA_TABLE__ AS
WITH RECURSIVE
nodes1 AS (
    SELECT DISTINCT
        call_tree_id,
        call_tree_parent_id,
        CASE
            WHEN (s1.name IS NULL OR s1.name = '') THEN '__empty_' || CAST(call_tree_id AS VARCHAR)
            ELSE CAST(hash(s1.name) AS VARCHAR)
        END AS name,
        t.symbol_id,
        CASE
            WHEN i1.image_name IS NULL OR i1.image_name = '' THEN '__noimg_' || CAST(call_tree_id AS VARCHAR)
            ELSE i1.image_name
        END AS image_name,
        node_type
    FROM __DRILLDOWN_TABLE_1__ AS t
    LEFT JOIN __SYMBOLS_TABLE_1__ AS s1 ON t.symbol_id = s1.symbol_id
    LEFT JOIN __IMAGES_TABLE_1__ AS i1 ON s1.image_id = i1.image_id
),
nodes2 AS (
    SELECT DISTINCT
        call_tree_id,
        call_tree_parent_id,
        CASE
            WHEN (s2.name IS NULL OR s2.name = '') THEN '__empty_' || CAST(call_tree_id AS VARCHAR)
            ELSE CAST(hash(s2.name) AS VARCHAR)
        END AS name,
        t.symbol_id,
        CASE
            WHEN i2.image_name IS NULL OR i2.image_name = '' THEN '__noimg_' || CAST(call_tree_id AS VARCHAR)
            ELSE i2.image_name
        END AS image_name,
        node_type
    FROM __DRILLDOWN_TABLE_2__ AS t
    LEFT JOIN __SYMBOLS_TABLE_2__ AS s2 ON t.symbol_id = s2.symbol_id
    LEFT JOIN __IMAGES_TABLE_2__ AS i2 ON s2.image_id = i2.image_id
),
tree1 AS (
    SELECT
        call_tree_id,
        call_tree_parent_id,
        name,
        symbol_id,
        image_name,
        node_type,
        COALESCE(name, 'empty') AS path
    FROM nodes1
    WHERE call_tree_parent_id = -1
    UNION ALL
    SELECT
        n.call_tree_id,
        n.call_tree_parent_id,
        n.name,
        n.symbol_id,
        n.image_name,
        n.node_type,
        tree1.path || '>' || n.name
    FROM nodes1 AS n
    JOIN tree1 ON n.call_tree_parent_id = tree1.call_tree_id
),
tree2 AS (
    SELECT
        call_tree_id,
        call_tree_parent_id,
        name,
        symbol_id,
        image_name,
        node_type,
        COALESCE(name, 'empty') AS path
    FROM nodes2
    WHERE call_tree_parent_id = -1
    UNION ALL
    SELECT
        n.call_tree_id,
        n.call_tree_parent_id,
        n.name,
        n.symbol_id,
        n.image_name,
        n.node_type,
        tree2.path || '>' || n.name
    FROM nodes2 AS n
    JOIN tree2 ON n.call_tree_parent_id = tree2.call_tree_id
),
unified AS (
    SELECT
        t1.call_tree_id AS call_tree_id_1,
        t1.call_tree_parent_id AS call_tree_parent_id_1,
        t2.call_tree_id AS call_tree_id_2,
        t2.call_tree_parent_id AS call_tree_parent_id_2,
        t1.symbol_id AS symbol_id_1,
        t2.symbol_id AS symbol_id_2,
        COALESCE(t1.node_type, t2.node_type) AS node_type,
        COALESCE(t1.path, t2.path) AS path,
        CASE
            WHEN t1.call_tree_id IS NOT NULL AND t2.call_tree_id IS NOT NULL THEN 0
            WHEN t1.call_tree_id IS NOT NULL THEN 1
            ELSE 2
        END AS exclusivity
    FROM tree1 AS t1
    FULL OUTER JOIN tree2 AS t2 ON
        t1.path = t2.path AND
        t1.node_type = t2.node_type AND
        t1.image_name = t2.image_name
)
SELECT
    u.exclusivity,
    u.call_tree_id_1,
    u.call_tree_parent_id_1,
    u.call_tree_id_2,
    u.call_tree_parent_id_2,
    u.symbol_id_1,
    u.symbol_id_2,
    u.node_type,
    m1.measurement_value AS measurement_value_1,
    m2.measurement_value AS measurement_value_2,
    COALESCE(m1.measurement_id, m2.measurement_id) AS measurement_id,
    CASE
        WHEN u.exclusivity = 0 THEN (m2.measurement_value - m1.measurement_value)
        ELSE NULL
    END AS delta_value,
    CASE
        WHEN u.exclusivity = 0 AND m1.measurement_value <> 0 THEN (m2.measurement_value - m1.measurement_value) * 100.0 / m1.measurement_value
        ELSE NULL
    END AS delta_percentage
FROM unified AS u
-- First add entries (with all measurements) from table1 (where exclusivity = 0)
LEFT JOIN __DRILLDOWN_TABLE_1__ AS m1 ON m1.call_tree_id = u.call_tree_id_1
LEFT JOIN __DRILLDOWN_TABLE_2__ AS m2 ON m2.call_tree_id = u.call_tree_id_2 AND m2.measurement_id = m1.measurement_id
WHERE u.exclusivity = 0
UNION ALL
SELECT
    u.exclusivity,
    u.call_tree_id_1,
    u.call_tree_parent_id_1,
    u.call_tree_id_2,
    u.call_tree_parent_id_2,
    u.symbol_id_1,
    u.symbol_id_2,
    u.node_type,
    NULL AS measurement_value_1,
    m2.measurement_value AS measurement_value_2,
    m2.measurement_id AS measurement_id,
    NULL AS delta_value,
    NULL AS delta_percentage
FROM unified AS u
-- Then add the measurement entries that can only be found in table2 but not in table1 (where exclusivity = 0)
LEFT JOIN __DRILLDOWN_TABLE_2__ AS m2 ON m2.call_tree_id = u.call_tree_id_2
LEFT JOIN __DRILLDOWN_TABLE_1__ AS m1 ON m1.call_tree_id = u.call_tree_id_1 AND m1.measurement_id = m2.measurement_id
WHERE u.exclusivity = 0 AND m1.measurement_id IS NULL
UNION ALL
SELECT
    u.exclusivity,
    u.call_tree_id_1,
    u.call_tree_parent_id_1,
    u.call_tree_id_2,
    u.call_tree_parent_id_2,
    u.symbol_id_1,
    u.symbol_id_2,
    u.node_type,
    m1.measurement_value AS measurement_value_1,
    m2.measurement_value AS measurement_value_2,
    COALESCE(m1.measurement_id, m2.measurement_id) AS measurement_id,
    NULL AS delta_value,
    NULL AS delta_percentage
FROM unified AS u
LEFT JOIN __DRILLDOWN_TABLE_1__ AS m1 ON m1.call_tree_id = u.call_tree_id_1
LEFT JOIN __DRILLDOWN_TABLE_2__ AS m2 ON m2.call_tree_id = u.call_tree_id_2
WHERE u.exclusivity <> 0;
