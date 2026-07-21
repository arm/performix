-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

INSERT INTO __DRILLDOWN_TABLE__ (
    call_tree_id,
    call_tree_parent_id,
    node_type,
    measurement_value,
    measurement_name,
    symbol_id,
    measurement_id
)
SELECT
    DENSE_RANK() OVER (ORDER BY symbol, image, symbols.symbol_id) AS call_tree_id,
    NULL AS call_tree_parent_id,
    'function' AS node_type,
    measurement_value,
    measurement_name,
    symbols.symbol_id AS symbol_id,
    ROW_NUMBER() OVER (PARTITION BY measurement_name ORDER BY measurement_name, symbol, image, symbols.symbol_id) AS measurement_id
FROM (
    UNPIVOT __RAW_TABLE__
    ON COLUMNS (* EXCLUDE (uid, image, symbol, 'inlined from'))
    INTO
        NAME  measurement_name
        VALUE measurement_value
) AS u
LEFT JOIN __SYMBOLS_TABLE__ AS symbols
    ON CAST(u.uid AS BIGINT) = symbols.symbol_id
LEFT JOIN __IMAGES_TABLE__ AS images
    ON images.image_id = symbols.image_id
WHERE measurement_value IS NOT NULL
ORDER BY measurement_name ASC, symbol ASC, image ASC;
