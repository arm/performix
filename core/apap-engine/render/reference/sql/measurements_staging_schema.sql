-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TEMP TABLE stg_measurements
(
    ord         INTEGER, -- to return IDs in input order
    identifier  TEXT,
    name        TEXT,
    description TEXT,
    short_description TEXT,
    units       TEXT
);

CREATE TEMP TABLE stg_tags
(
    identifier TEXT,
    tag        TEXT
);

CREATE TEMP TABLE stg_aliases
(
    ord        INTEGER,
    identifier TEXT,
    source     TEXT,
    alias      TEXT
);

CREATE TEMP TABLE stg_colrefs
(
    identifier  TEXT,
    table_name  TEXT,
    column_name TEXT,
    renderer    TEXT NULL
);
