-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

WITH to_insert AS (
    SELECT
       r.symbol,
       i.image_id
    FROM __RAW_TABLE__ r
       LEFT JOIN __IMAGES_TABLE__ i
           ON r.image = i.image_name
       LEFT JOIN __SYMBOLS_TABLE__ s
           ON r.symbol = s.name AND i.image_id = s.image_id
    WHERE s.name IS NULL
),
ordered AS (
    SELECT
        symbol,
        image_id,
        ROW_NUMBER() OVER (ORDER BY symbol, image_id) AS row_number
    FROM to_insert
),
current_max AS (
    SELECT COALESCE(MAX(symbol_id), 0) as max_id FROM __SYMBOLS_TABLE__
)
INSERT INTO __SYMBOLS_TABLE__ (symbol_id, name, image_id)
SELECT
    c.max_id + o.row_number,
    o.symbol,
    o.image_id
FROM ordered as o, current_max as c;