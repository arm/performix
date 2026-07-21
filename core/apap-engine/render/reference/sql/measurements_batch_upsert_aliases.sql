-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

INSERT INTO ref_measurement_aliases (measurement_id, source, alias)
SELECT DISTINCT ON (m.measurement_id, a.source)
    m.measurement_id,
    a.source,
    a.alias
FROM stg_aliases a
         JOIN stg_map m USING (identifier)
WHERE length(trim(a.source)) > 0
  AND length(trim(a.alias)) > 0
ORDER BY m.measurement_id, a.source, a.ord DESC
ON CONFLICT (measurement_id, source) DO UPDATE
SET alias = EXCLUDED.alias;
