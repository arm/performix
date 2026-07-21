-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

INSERT INTO ref_measurement_tags (measurement_id, tag)
SELECT DISTINCT m.measurement_id, t.tag
FROM stg_tags t
         JOIN stg_map m USING (identifier)
WHERE t.tag IS NOT NULL
  AND length(trim(t.tag)) > 0
ON CONFLICT DO NOTHING;