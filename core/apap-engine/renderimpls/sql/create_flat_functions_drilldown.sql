-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE __DRILLDOWN_TABLE__ (
    call_tree_id INTEGER,
    call_tree_parent_id INTEGER,
    node_type VARCHAR,
    measurement_value DOUBLE,
    measurement_name VARCHAR, -- col will be dropped later
    symbol_id INTEGER,
    measurement_id INTEGER
);
