-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE "%s" AS
SELECT a.*,
       s.symbol_id,
       s.image_id
FROM "%s" AS a
LEFT JOIN "%s" AS i
  ON a.image = i.image_name
LEFT JOIN "%s" AS s
  ON a.symbol = s.name AND s.image_id = i.image_id
