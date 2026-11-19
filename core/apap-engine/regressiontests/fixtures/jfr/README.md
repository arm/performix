<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Synthetic JFR execution samples

The `jfr_execution_samples` fixture uses `generate.sql` to create a synthetic Run
with `manifest.json`, `metadata.json` and these Parquet files:

```text
tool/neoprof/0/java/parquet/
├── symbols.parquet
├── paths.parquet
└── observations/event=jdk.ExecutionSample/
    ├── recording=1/part-0.parquet
    └── recording=2/part-0.parquet
```

The manifest has three `jfr-parquet` components: `symbols.parquet`,
`paths.parquet` and `observations/**/*` (all relative to the Parquet directory).
The `observations/**/*` glob includes nested event and recording folders.

There are 6 symbols, 10 paths and 21 observations containing 19 samples, including
2 samples without stacks. The two extra observations contain a measured zero
and an absent (null) measurement. Path IDs cover `1..10`, but rows are deliberately
shuffled so consumers cannot use row order as identity. Path and symbol IDs are
shared across recordings.

Every observation partition declares `performix.observation.measurements` as
`{"execution_samples":"samples"}`: column names map to units, not display names.
`TestGeneratedExecutionSampleRunShouldMatchTheParquetContract` checks this metadata in each partition, column types,
zero/null preservation, dense IDs, counts, path/symbol links and the declared
ascending timestamp order within each partition.

Set `APX_KEEP_GENERATED_TEST_DATA=1` to retain the generated Runs for inspection.
The JFR fixture is stored at
`regressiontests/test-data/runs/generated/jfr_execution_samples/`. Each fresh test
execution clears that directory and generates fresh data there for the tests to
share. Without retention, tests use a temporary directory and leave previous
retained data untouched. Avoid simultaneous executions with retention enabled.

To execute the fixture tests and refresh the retained data, bypass Go's test
cache with `-count=1`:

```shell
APX_KEEP_GENERATED_TEST_DATA=1 mise exec -- go test -C core/apap-engine -tags=duckdb_arrow \
  ./regressiontests -run 'TestGenerated|TestInvalidRunSources|TestLocalRuns' -count=1 -v
```
