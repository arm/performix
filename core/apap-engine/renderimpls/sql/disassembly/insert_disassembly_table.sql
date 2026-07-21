-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

WITH symbols_dedup AS (
    SELECT
        MIN(symbol_id) AS symbol_id,
        name,
        image_id
    FROM __SYMBOLS_TABLE__
    GROUP BY
        name,
        image_id
),
raw_rows AS (
    SELECT
        filename,
        "Address",
        "Opcode",
        "Instruction",
        "Arguments",
        "Periodic Samples",
        "Source File",
        "Line No",
        "Address" IS NOT NULL
            AND "Opcode" IS NOT NULL
            AND "Instruction" IS NULL
            AND "Arguments" IS NULL
            AND "Periodic Samples" IS NULL
            AND "Source File" IS NULL
            AND "Line No" IS NULL AS is_symbol_marker
    FROM __RAW_ROWS_TABLE__
),
normalized_rows AS (
    SELECT
        *,
        CASE
            WHEN "Address" IS NULL THEN NULL
            ELSE CAST('0x' || "Address" AS BIGINT)
        END AS parsed_address,
        CASE
            WHEN is_symbol_marker OR "Opcode" IS NULL THEN NULL
            ELSE CAST('0x' || replace("Opcode", ' ', '') AS BIGINT)
        END AS parsed_opcode
    FROM raw_rows
),
file_images AS (
    SELECT
        f.filename,
        i.image_id
    FROM (
        SELECT
            filename,
            CASE image_name
                WHEN '_jitted-code_' THEN '<jitted-code>'
                ELSE image_name
            END AS image_name
        FROM (
            SELECT DISTINCT
                filename,
                substring(base_filename, length(__DISASSEMBLY_COMPONENT__) + 2, length(base_filename) - length(__DISASSEMBLY_COMPONENT__) - 5) AS image_name
            FROM (
                SELECT DISTINCT
                    filename,
                    regexp_extract(filename, '([^/\\]+)$', 1) AS base_filename
                FROM raw_rows
                WHERE is_symbol_marker
            )
        )
    ) f
    LEFT JOIN __IMAGES_TABLE__ i ON i.image_name = f.image_name
),
symbol_markers AS (
    SELECT
        r.filename,
        r.parsed_address AS symbol_address,
        s.symbol_id
    FROM normalized_rows r
    LEFT JOIN file_images f ON f.filename = r.filename
    LEFT JOIN symbols_dedup s
        ON s.name = r."Opcode"
        AND s.image_id = f.image_id
    WHERE r.is_symbol_marker
),
processed_raw_rows AS (
    SELECT
        s.symbol_id,
        r.parsed_address AS address,
        r.parsed_address - s.symbol_address AS "offset",
        r.parsed_opcode AS opcode,
        r."Instruction" AS instruction,
        r."Arguments" AS arguments,
        r."Periodic Samples" AS periodic_samples,
        r."Source File" AS source_file,
        r."Line No" AS line_no
    FROM normalized_rows r
    ASOF LEFT JOIN symbol_markers s
        ON r.filename = s.filename
        AND r.parsed_address >= s.symbol_address
    WHERE NOT r.is_symbol_marker
)
INSERT INTO __TABLE_NAME__ (address, symbol_id, "offset", instruction, arguments, opcode, periodic_samples, source_file_id, line_no)
SELECT
    r.address AS address,
    r.symbol_id AS symbol_id,
    r."offset" AS "offset",
    r.instruction AS instruction,
    r.arguments AS arguments,
    r.opcode AS opcode,
    r.periodic_samples AS periodic_samples,
    sf.source_file_id AS source_file_id,
    r.line_no AS line_no
FROM processed_raw_rows r
LEFT JOIN __SOURCE_FILES_TABLE__ sf ON r.source_file = sf.target_location
