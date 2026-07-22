-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

SELECT
  cacheline_address,
  symbol,
  sample_count,
  coherence_sample_count,
  store_sample_count,
  thread_count,
  writer_thread_count,
  image,
  source_file,
  source_line,
  byte_offset
FROM read_csv_auto(?, header=true)
