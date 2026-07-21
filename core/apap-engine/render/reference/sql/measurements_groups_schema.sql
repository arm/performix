-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE SEQUENCE IF NOT EXISTS ref_measurement_groups_group_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE;

CREATE TABLE IF NOT EXISTS ref_measurement_groups (
    group_id   INTEGER PRIMARY KEY DEFAULT nextval('ref_measurement_groups_group_id_seq'),
    group_name TEXT    NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);


CREATE TABLE IF NOT EXISTS ref_measurement_group_links (
    measurement_id INTEGER NOT NULL,
    group_id      INTEGER NOT NULL,
    PRIMARY KEY (measurement_id, group_id),
    FOREIGN KEY (measurement_id) REFERENCES ref_measurements (measurement_id),
    FOREIGN KEY (group_id) REFERENCES ref_measurement_groups (group_id)
);