-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE __TABLE_NAME__ (
    address BIGINT,
    symbol_id INTEGER,
    "offset" BIGINT,
    instruction VARCHAR,
    arguments VARCHAR,
    opcode BIGINT,
    periodic_samples INTEGER,
    source_file_id INTEGER,
    line_no INTEGER
);
