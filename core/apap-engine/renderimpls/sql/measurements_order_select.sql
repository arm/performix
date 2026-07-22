-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

SELECT
    measurement_id,
    order_index
FROM __TABLE_NAME__
ORDER BY order_index ASC;
