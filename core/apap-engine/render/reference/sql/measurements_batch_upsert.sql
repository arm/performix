-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

INSERT INTO ref_measurements
(measurement_id, identifier, name, description, short_description, units)
SELECT nextval('ref_measurement_id_seq'),
       s.identifier,
       s.name,
       s.description,
       s.short_description,
       s.units
FROM (SELECT DISTINCT ON (identifier) identifier,
                                      name,
                                      description,
                                      short_description,
                                      units
      FROM stg_measurements
      ORDER BY ord DESC) s
ON CONFLICT (identifier) DO UPDATE
    SET name        = EXCLUDED.name,
        description = EXCLUDED.description,
        short_description = EXCLUDED.short_description,
        units       = EXCLUDED.units;
