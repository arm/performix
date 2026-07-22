-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

WITH distinct_names AS (
    SELECT DISTINCT r.image
    FROM __RAW_TABLE__ r
    LEFT JOIN __IMAGES_TABLE__ i
        ON r.image = i.image_name
    WHERE i.image_name IS NULL
),
ordered AS (
    SELECT
        image,
        ROW_NUMBER() OVER (ORDER BY image) AS row_number
    FROM distinct_names
),
current_max AS (
    SELECT COALESCE(MAX(image_id), 0) as max_id FROM __IMAGES_TABLE__
)
INSERT INTO __IMAGES_TABLE__ (image_id, image_name)
SELECT
    c.max_id + o.row_number,
    o.image
FROM ordered as o, current_max as c;