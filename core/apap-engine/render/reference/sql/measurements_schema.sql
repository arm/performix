-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE IF NOT EXISTS ref_measurements
(
    measurement_id INTEGER PRIMARY KEY,
    identifier     TEXT NOT NULL UNIQUE,
    name           TEXT NOT NULL,
    description    TEXT,
    short_description TEXT,
    units          TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ref_measurement_aliases
(
    measurement_id INTEGER NOT NULL REFERENCES ref_measurements (measurement_id),
    source         TEXT    NOT NULL, -- "telemetry", "legacy", "import:v1"
    alias          TEXT    NOT NULL,
    UNIQUE (measurement_id, source)
);

CREATE TABLE IF NOT EXISTS ref_measurement_tags
(
    measurement_id INTEGER NOT NULL REFERENCES ref_measurements (measurement_id),
    tag            TEXT    NOT NULL,
    PRIMARY KEY (measurement_id, tag)
);

CREATE TABLE IF NOT EXISTS ref_measurement_column_refs
(
    measurement_id INTEGER NOT NULL REFERENCES ref_measurements (measurement_id),
    table_name     TEXT    NOT NULL,
    column_name    TEXT    NOT NULL,
    renderer       TEXT,
    UNIQUE (measurement_id, table_name, column_name)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ref_meas_identifier ON ref_measurements (identifier);
CREATE UNIQUE INDEX IF NOT EXISTS uq_ref_colref_tbl_col ON ref_measurement_column_refs (measurement_id, table_name, column_name);
CREATE INDEX IF NOT EXISTS idx_ref_alias_mid_source ON ref_measurement_aliases (measurement_id, source);

CREATE SEQUENCE IF NOT EXISTS ref_measurement_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE;
