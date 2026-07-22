-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE OR REPLACE VIEW __VIEW_NAME__ AS
SELECT DISTINCT ON (m.measurement_id) m.*
FROM ref_measurements m
         JOIN ref_measurement_column_refs c
              ON m.measurement_id = c.measurement_id
WHERE c.table_name IN (__TABLE_NAMES__);