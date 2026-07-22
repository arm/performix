-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

SELECT m.measurement_id
FROM stg_measurements s
         JOIN ref_measurements m ON m.identifier = s.identifier
ORDER BY s.ord;