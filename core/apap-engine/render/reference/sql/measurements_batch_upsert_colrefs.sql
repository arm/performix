-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

INSERT INTO ref_measurement_column_refs
    (measurement_id, table_name, column_name, renderer)
SELECT DISTINCT m.measurement_id,
                c.table_name,
                c.column_name,
                NULLIF(trim(c.renderer), '')
FROM stg_colrefs c
         JOIN stg_map m USING (identifier)
ON CONFLICT DO NOTHING;