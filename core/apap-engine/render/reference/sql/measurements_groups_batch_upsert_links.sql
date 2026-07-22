-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

INSERT INTO ref_measurement_group_links (measurement_id, group_id)
SELECT DISTINCT m.measurement_id, gl.group_id
FROM stg_group_links gl
JOIN ref_measurements m ON m.identifier = gl.measurement_identifier
WHERE gl.group_id IS NOT NULL
ON CONFLICT DO NOTHING;