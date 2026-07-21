-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

SELECT
  cacheline_address,
  sample_count,
  sample_pct,
  sharing_classification,
  distinct_sharing_offsets,
  sharing_thread_count,
  writer_thread_count
FROM read_csv_auto(
  ?,
  header=true,
  columns={
    'cacheline_address': 'VARCHAR',
    'sample_count': 'BIGINT',
    'sample_pct': 'DOUBLE',
    'sharing_classification': 'VARCHAR',
    'distinct_sharing_offsets': 'BIGINT',
    'sharing_thread_count': 'BIGINT',
    'writer_thread_count': 'BIGINT'
  }
)
