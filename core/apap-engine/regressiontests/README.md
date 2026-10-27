<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Performix Engine Renderer Regression (Integration) Tests

This is a suite of regression-based integration tests for the Performix Engine.

## Coverage Documentation

The coverage of most existing tests is not documented - this is an outstanding TODO. See [apap-engine/regressiontests/test-data/runs/README.md](apap-engine/regressiontests/test-data/runs/README.md). **When adding a new test, ensure you document its purpose & coverage**.

An audit was previously completed, the outcome of which is documented on the following Confluence page: https://confluence.arm.com/x/7JHSo

## CPU counter selection in Code Hotspots

Recipe-level render coverage lives in [`../recipejstest/code_hotspots_cpu_counters_test.go`](../recipejstest/code_hotspots_cpu_counters_test.go). A counter series is a sequence of counter measurements identified by `key_type` and `series_id`. These tests verify that collected and incomplete CPU-cycle outcomes render only their bound counter series. Missing or unavailable outcomes do not fall back to counter files. Windows sampling Runs also verify that no timeline is rendered.

A CPU group is a set of CPUs grouped by the collector. Generated Parquet fixtures represent one group on which the profiled process never ran and another with recorded measurements. The tests verify that the inactive group's counter series remains queryable and returns no observations, while the active group's measurements remain available. These tests run as part of the engine Go unit tests.

## NeoProf System Utilization idle counters

[`system_utilization_neoprof_test.go`](../recipejstest/system_utilization_neoprof_test.go) checks that missing samples in collected delta counters become zero, including idle disk and IRQ counters. It also checks that recorded values are preserved and missing or unsupported metrics, such as NUMA, stay NULL. These tests run with the engine Go unit tests.

The same tests verify that normalization produces a table, so chart queries reuse the results. The SQL renderer's optional `materialize: true` setting evaluates the query once per render session; other SQL renderers keep using views by default.

## Adding New Tests

The renderer regression tests are driven by JSON configuration files under `test-data/tests`.
Each configuration file is paired with expected output under `test-data/truth/<test-name>`, where `<test-name>` is the configuration filename without the `.json` extension.
The input run data used by those configurations lives under `test-data/runs`.

When adding a new regression test:

1. Add or reuse a fixture run under `test-data/runs`.
2. Add a JSON test configuration under `test-data/tests`.
   The configuration can specify:
   - `importedRuns`: optional logical run aliases that are resolved from a pre-recorded Artifactory run before the test session starts. `prerecordedRun.artifactDir` should point at the stable case directory that contains the exported `latest.zip`. The harness first uses `PERFORMIX_REGRESSIONTEST_PRERECORDED_RUN_CACHE_DIR` when it already contains that artifact, otherwise it downloads from `prerecordedRun.artifactoryRunBase` or `PERFORMIX_REGRESSIONTEST_ARTIFACTORY_RUN_BASE` with `ARTIFACTORY_API_TOKEN`.
   - `renderers`: renderer names, optional renderer IDs, renderer configuration, and the run IDs to render.
   - `visualizations`: optional visualization IDs and configuration.
   - `queries`: optional SQL overrides for generated output tables. If no override is provided for a visible manifest entry, the harness queries all columns from that table in a deterministic order.
   - `toolIntegrations`: optional tool integration versions and migrations needed by the test fixture.
3. Regenerate the expected output by running the engine tests with `REGEN=1`.
4. Review the generated `test-data/truth/<test-name>` files carefully before committing them.
   These files are the contract for the regression test, so only commit changes that are expected from the feature or bug fix being tested.
5. Run the normal engine test task without `REGEN=1` to make sure the checked-in truth data matches the renderer output.

Tests that declare `importedRuns` are skipped when neither a local prerecorded-run cache nor `ARTIFACTORY_API_TOKEN` is available. That keeps ordinary local unit-test runs usable while still letting CI exercise the imported-run coverage.

## Refreshing Instruction Mix fidelity fixtures

The Instruction Mix engine-fidelity regressions use two named, Artifactory-hosted
captures. Run the **Instruction Mix Fidelity Pre-record Run** workflow manually
to refresh one; it supplies the workload, recipe parameters, AWS instance type,
and Artifactory destination so that those inputs are not re-entered by hand.

| Workflow fixture | Recipe parameters | Artifactory archive | Regression configuration | Generated truth |
| --- | --- | --- | --- | --- |
| `pr-smoke` | `mode=dynamic` | `its.apx-prerecorded-runs/manual/instruction-mix-openssl-speed-pr-smoke/latest.zip` | `test-data/tests/instruction_mix_dynamic_pr_smoke.json` | `test-data/truth/instruction_mix_dynamic_pr_smoke/` |
| `nightly-comparison` | `mode=dynamic`, `sampling_freq=low` | `its.apx-prerecorded-runs/manual/instruction-mix-openssl-speed-comparison/latest.zip` | `test-data/tests/instruction_mix_dynamic_comparison.json` | `test-data/truth/instruction_mix_dynamic_comparison/` |

Both captures run the `openssl-speed` workload on an Ubuntu 24.04 Arm
`c7g.metal` target. Supply the ID of a successful **APX Extended Validation**
run that produced the CLI artifact to use for the capture. Use `dry_run=true`
first when checking an updated capture without replacing the shared fixture.

After intentionally refreshing a fixture:

1. Download the workflow artifact or let the regression harness download the
   Artifactory archive with `ARTIFACTORY_API_TOKEN`.
2. Regenerate and verify the required golden contracts with `REGEN=1`:
   - Refreshing `pr-smoke` requires both the PR-smoke and nightly-comparison
     contracts, because the comparison contract uses PR-smoke as its baseline.
   - Refreshing only `nightly-comparison` requires only the comparison
     contract.
3. Review every generated truth-file change; the archive input is external and
   the truth files are the engine contract.
4. Run the same focused test again without `REGEN=1`, then commit the test
   configuration and reviewed truth together.

From the repository root, use:

```shell
export ARTIFACTORY_API_TOKEN=…
export PERFORMIX_REGRESSIONTEST_PRERECORDED_RUN_CACHE_DIR="${TMPDIR:-/tmp}/performix-prerecorded-run-cache"
export PERFORMIX_REGRESSIONTEST_REQUIRE_IMPORTED_RUNS=1

# Refreshing pr-smoke: also refresh its dependent comparison contract.
test_pattern='TestRendererRegression/Regression_test_.instruction_mix_dynamic_(pr_smoke|comparison).json._should_pass'

# Refreshing only the comparison archive:
# test_pattern='TestRendererRegression/Regression_test_.instruction_mix_dynamic_comparison.json._should_pass'

REGEN=1 go test -C core/apap-engine -tags=duckdb_arrow ./regressiontests -count=1 -run "$test_pattern"
go test -C core/apap-engine -tags=duckdb_arrow ./regressiontests -count=1 -run "$test_pattern"
```

`ARTIFACTORY_API_TOKEN` is required only when the selected capture is not
already available in the cache directory.
The `nightly-comparison` fixture requires both the `pr-smoke` baseline archive
and the comparison archive; the harness downloads each missing archive when a
token is available.

The test harness also checks that every registered renderer is covered by at least one regression test configuration.
If you add a new renderer, add a matching configuration in `test-data/tests` and regenerate the corresponding truth data.

### C1 engine-to-GUI measurement contract

`renderimpls/TestC1FlatFunctionMeasurementsGUIContract` uses
`renderimpls/testdata/lumex-measurements.json` to verify C1-Pro collector prefixes,
stable telemetry aliases, descriptions, persisted group links, and values through
the function measurement registry and call-path renderer. The GUI's
`build-measurements-lumex.test.ts` consumes the same expected output names and
values to check table groups and topdown graph binding. Run the Go test with
`duckdb_arrow,confidential_telemetry` build tags. Other C1 tests cover all four
specifications, collector aliases, and rejection of foreign-core prefixes.
