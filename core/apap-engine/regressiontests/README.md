<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Performix Engine Renderer Regression (Integration) Tests

This is a suite of regression-based integration tests for the Performix Engine.

## Coverage Documentation

The coverage of most existing tests is not documented - this is an outstanding TODO. See [apap-engine/regressiontests/test-data/runs/README.md](apap-engine/regressiontests/test-data/runs/README.md). **When adding a new test, ensure you document its purpose & coverage**.

An audit was previously completed, the outcome of which is documented on the following Confluence page: https://confluence.arm.com/x/7JHSo

## Adding New Tests

The renderer regression tests are driven by JSON configuration files under `test-data/tests`.
Each configuration file is paired with expected output under `test-data/truth/<test-name>`, where `<test-name>` is the configuration filename without the `.json` extension.
The input run data used by those configurations lives under `test-data/runs`.

When adding a new regression test:

1. Add or reuse a fixture run under `test-data/runs`.
2. Add a JSON test configuration under `test-data/tests`.
   The configuration can specify:
   - `renderers`: renderer names, optional renderer IDs, renderer configuration, and the run IDs to render.
   - `visualizations`: optional visualization IDs and configuration.
   - `queries`: optional SQL overrides for generated output tables. If no override is provided for a visible manifest entry, the harness queries all columns from that table in a deterministic order.
   - `toolIntegrations`: optional tool integration versions and migrations needed by the test fixture.
3. Regenerate the expected output by running the engine tests with `REGEN=1`.
4. Review the generated `test-data/truth/<test-name>` files carefully before committing them.
   These files are the contract for the regression test, so only commit changes that are expected from the feature or bug fix being tested.
5. Run the normal engine test task without `REGEN=1` to make sure the checked-in truth data matches the renderer output.

From the repository root, use:

```shell
REGEN=1 task core:test:unit:engine
task core:test:unit:engine
```

The test harness also checks that every registered renderer is covered by at least one regression test configuration.
If you add a new renderer, add a matching configuration in `test-data/tests` and regenerate the corresponding truth data.
