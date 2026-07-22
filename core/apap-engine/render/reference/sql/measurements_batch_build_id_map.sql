-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

DROP TABLE IF EXISTS stg_map;

CREATE TEMP TABLE stg_map AS
SELECT s.ord, s.identifier, r.measurement_id
FROM stg_measurements s
         JOIN ref_measurements r USING (identifier);
