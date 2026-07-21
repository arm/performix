-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE OR REPLACE VIEW __VIEW_NAME__ AS (
    SELECT *
    FROM read_csv(
        '__DISASSEMBLY_PATH__-*.csv',
        auto_detect   = FALSE,
        filename      = TRUE,
        header        = TRUE,
        null_padding  = TRUE,
        union_by_name = FALSE,
        delim         = ',',
        quote         = '"',
        escape        = '"',
        columns = {
            'Address':'VARCHAR',
            'Opcode':'VARCHAR',
            'Instruction':'VARCHAR',
            'Arguments':'VARCHAR',
            'Target Symbol':'VARCHAR',
            'Periodic Samples':'INTEGER',
            'Symbol UID':'VARCHAR',
            'Source File':'VARCHAR',
            'Line No':'INTEGER',
            'Inlined From Function':'VARCHAR',
            'Inlined Function Source File':'VARCHAR',
            'Inlined Function Line No':'INTEGER'
        })
);
