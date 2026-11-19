-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

-- Synthetic test data. These samples were not recorded by JFR.
-- GenerateRun replaces the output paths before executing this script.
CREATE TEMP TABLE fixture_symbols (
    symbol_id UBIGINT NOT NULL,
    symbol_name VARCHAR,
    inline_parent_symbol_id UBIGINT,
    class_name VARCHAR,
    descriptor VARCHAR
);
INSERT INTO fixture_symbols VALUES
    (1, 'main', NULL, 'fixture/Main', '([Ljava/lang/String;)V'),
    (2, 'run', NULL, 'fixture/Runner', '()V'),
    (3, 'work', NULL, 'fixture/Worker', '()V'),
    (4, 'helper', NULL, 'fixture/Worker', '()V'),
    (5, 'run', NULL, 'fixture/OtherRunner', '()V'),
    (6, 'work', NULL, 'fixture/Worker', '(I)V');

CREATE TEMP TABLE fixture_paths (
    path_id UBIGINT NOT NULL,
    parent_path_id UBIGINT,
    symbol_id UBIGINT NOT NULL,
    is_inline BOOLEAN NOT NULL,
    bytecode_index BIGINT NOT NULL,
    line_number BIGINT NOT NULL,
    frame_type VARCHAR
);
INSERT INTO fixture_paths VALUES
    (1, NULL, 1, false, 0, 10, 'JIT compiled'),
    (2, 1, 2, false, 5, 20, 'JIT compiled'),
    (3, 2, 3, false, 10, 30, 'JIT compiled'),
    (4, 2, 3, false, 20, 31, 'JIT compiled'),
    (5, 3, 4, true, 2, 40, 'Inlined'),
    (6, 1, 5, false, 0, 50, 'JIT compiled'),
    (7, 6, 3, false, 10, 30, 'JIT compiled'),
    (8, 3, 3, false, 10, 30, 'JIT compiled'),
    (9, 2, 6, false, 0, 60, 'JIT compiled'),
    (10, 4, 4, true, 6, 41, 'Inlined');

CREATE TEMP TABLE fixture_observations (
    observation_id UBIGINT NOT NULL,
    path_id UBIGINT,
    timestamp_ns BIGINT,
    duration_ns UBIGINT,
    upid UBIGINT,
    utid UBIGINT,
    device_id UINTEGER,
    execution_samples UBIGINT,
    state VARCHAR,
    sampled_thread STRUCT(
        os_name VARCHAR,
        os_thread_id BIGINT,
        java_name VARCHAR,
        java_thread_id BIGINT,
        group_name VARCHAR,
        group_parent_name VARCHAR,
        virtual BOOLEAN
    )
);
INSERT INTO fixture_observations
SELECT observation_id::UBIGINT, path_id::UBIGINT,
       (1000000000 + (observation_id - 1) * 10000000)::BIGINT,
       NULL::UBIGINT, 1::UBIGINT, 1::UBIGINT, NULL::UINTEGER,
       CASE observation_id WHEN 20 THEN 0 WHEN 21 THEN NULL ELSE 1 END::UBIGINT,
       'STATE_RUNNABLE',
       struct_pack(os_name := 'fixture-worker', os_thread_id := 424242::BIGINT,
                   java_name := 'fixture-worker', java_thread_id := 1::BIGINT,
                   group_name := 'main', group_parent_name := 'system', virtual := false)
FROM (VALUES
    (1, 3), (2, 3), (3, 3),
    (4, 4), (5, 4),
    (6, 5), (7, 5),
    (8, 10),
    (9, 7), (10, 7), (11, 7), (12, 7),
    (13, 8),
    (14, 9), (15, 9),
    (16, 2), (17, 1),
    (18, NULL), (19, NULL),
    (20, 5), (21, 3) -- Measured zero and absent measurement; neither adds samples.
) AS input(observation_id, path_id)
ORDER BY observation_id;

COPY fixture_symbols TO 'symbols.parquet' (FORMAT PARQUET, COMPRESSION ZSTD);
-- Dense IDs need not be stored in ID or caller order.
COPY (SELECT * FROM fixture_paths ORDER BY path_id % 3, path_id) TO 'paths.parquet' (
    FORMAT PARQUET, COMPRESSION ZSTD,
    KV_METADATA {'performix.paths.projections': '{"Bytecode":["symbol_id","bytecode_index"],"Function":["symbol_id"],"Source":["symbol_id","line_number"]}'}
);
COPY (
    SELECT *, 'jdk.ExecutionSample' AS event,
           CASE WHEN observation_id <= 10 THEN 1 ELSE 2 END AS recording
    FROM fixture_observations
    ORDER BY timestamp_ns
) TO 'observations' (
    FORMAT PARQUET, COMPRESSION ZSTD, PARTITION_BY (event, recording),
    FILENAME_PATTERN 'part-{i}',
    KV_METADATA {
        'performix.observation.measurements': '{"execution_samples":"samples"}',
        'performix.observation.timestamp_ascending': 'true'
    }
);
