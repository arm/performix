<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Performix test suites

This is a complete list of Performix test suites. It should be kept up to date with hyperlinks to the relevant part of the code base where needed. Avoid bloating this file with additional information, such as what is currently covered by these test suites - information like this should go in a README file or similar alongside that test suite.

1. [Collector accuracy tests](https://github.com/Arm-Debug/performix-collectors/tree/main/.github/scripts/live-tests/README.md) (collector-level; Arm-internal access required)
2. [Recipe accuracy benchmarks](core/scripts/atperf-evaluation/benchmarks/accuracy) (core)
3. [Core unit and integration tests](core/README.md#testing-strategy) (Go, JavaScript recipes/tool integrations, Python)
4. [Engine renderer regression tests](core/apap-engine/regressiontests/README.md)
5. [Workload compatibility tests](core/scripts/atperf-evaluation/workload_compatibility) (localhost and remote)
6. [Engine and collector overhead benchmarks](core/scripts/atperf-evaluation/benchmarks/overhead)
7. [Version and historical-run compatibility tests](core/scripts/atperf-compat/compatibility_verification.py)
8. [CLI/engine functional end-to-end tests](core/robot/README.md) (Robot)
9. MCP integration and client tests (Robot): [integration tests](core/robot/tests/mcp), [client testing guide](core/robot/README.md#testing-mcp-clients)
10. [AI Insights and Pathways evaluation suites](core/scripts/ai-insights-evaluation/README.md)
11. [GUI unit and component tests](https://github.com/Arm-Debug/performix/blob/main/gui/src/test/README.md) (Vitest; Arm-internal access required)
12. [GUI functional end-to-end tests](https://github.com/Arm-Debug/performix/blob/main/gui/docs/end-to-end-tests.md) (Playwright; includes data fidelity; Arm-internal access required)
13. [GUI Android end-to-end tests](https://github.com/Arm-Debug/performix/blob/main/gui/docs/android-e2e-tests.md) (Playwright; Arm-internal access required)
14. [GUI performance tests](https://github.com/Arm-Debug/performix/tree/main/gui/e2e-tests/performance-tests) (Playwright; Arm-internal access required)
15. [GUI soak tests](https://github.com/Arm-Debug/performix/blob/main/gui/e2e-tests/soak-tests/soak.test.ts) (Playwright; Arm-internal access required)
16. Packaging and installation smoke tests: [GUI Snap](https://github.com/Arm-Debug/performix/blob/main/.github/workflows/snap-smoke-test.yaml) (Arm-internal access required), [APX smoke](https://github.com/Arm-Debug/performix/blob/main/.github/workflows/apx-extended-validation.yml) (Arm-internal access required), [CLI installation](core/robot/tests/install)
17. [CI script unit tests](https://github.com/Arm-Debug/performix/blob/main/.github/scripts/run_ci_script_tests.py) (Arm-internal access required)
