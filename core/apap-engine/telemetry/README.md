<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>

SPDX-License-Identifier: Apache-2.0
-->

# CPU telemetry specifications

This package is the single source of truth for the CPU telemetry specifications used by the engine, recipes, and API clients.

Most JSON files in `data/` are obtained from the [Arm Telemetry Solution](https://gitlab.arm.com/telemetry-solution/telemetry-solution/) project. Add or update specifications here rather than packaging copies with individual clients.

- `data/public/` contains specifications used in public builds. The generic
  Cortex specification is excluded from the open-source mirror as described below.
- `data/private/` contains specifications not approved for inclusion in the public repository.
- `data/schemas/` contains the v1.0 and v1.2 JSON schemas vendored from Arm
  Telemetry Solution commit
  [`6036adbc6a0b46c7ba05363bd8d58d89ccdecba6`](https://gitlab.arm.com/telemetry-solution/telemetry-solution/-/commit/6036adbc6a0b46c7ba05363bd8d58d89ccdecba6).
  Their SHA-256 hashes are `adca64bafe17ab18e5f7b5c46c8dd583c711e6f795749294242edda2b57ec8f5`
  for v1.0 and `31f25e7f8c667941fd609f91e1973ba80d3a9cccde6f723448bfc8d4c95567c5`
  for v1.2.

Run `task core:telemetry:validate` after changing telemetry. The `$schema`
field must name one of the locally vendored schema files; validation never
downloads a schema. Specification findings in both `data/public/` and
`data/private/` are warnings, allowing known legacy formats to remain visible
without blocking development or CI.

Specifications in `data/private/` must set `document.confidential` to `true`. The
OSSmosis built-in `json-confidential` keyword treats that metadata as a
confidential-content marker, so a private specification moved outside the
excluded directory fails the public-content scan.

The generic Cortex specification, `data/public/generic-cortex.json`, is not an
official Arm Telemetry Solution specification. We have permission to bundle and
use it in the public application, but not to publish its source in the
open-source repository. It remains marked `document.confidential=true`, and
`.ossmosis.json` excludes the JSON file and its license sidecar from the public
mirror. The engine embeds the file when it is present; the mirrored source also
builds without it, but does not provide the generic Cortex fallback.

## Lumex C1 imports

The `c1-nano.json`, `c1-pro.json`, `c1-premium.json`, and `c1-ultra.json`
files come from the corresponding `arm_c1_<variant>_r0p0_pmu.json` files in
[Arm Telemetry Solution at commit df65af392a6fee2c39f174b1d1d467194bb4bd60](https://gitlab.arm.com/telemetry-solution/telemetry-solution/-/tree/df65af392a6fee2c39f174b1d1d467194bb4bd60/data/pmu/cpu/specifications/lumex).
The public copies set `document.confidential` to `false`. Copyright, Apache-2.0
licensing, quality, timestamp, and source SHA metadata are preserved, including
Premium's upstream `Development` quality designation.

Resolution uses the collector's exact names: `C1-Nano`, `C1-Pro`, `C1-Premium`,
and `C1-Ultra`. These specifications are available without the
`confidential_telemetry` build tag.
The public CPU detection IDs are supplemented in
`core/atperf-agent/systeminfo/armcpus/cpus_lumex.go` because the upstream CPU-name
catalog does not yet include them.

All ten upstream files are retained under `data/public/`. In addition to the
four r0p0 CPU files above, `c1-pro-r1p1.json`, `c1-pro-r1p2.json`, and
`c1-sme2-{r0p0,r1p0,r1p1,r1p2}.json` preserve the corresponding
`arm_c1_<variant>_<revision>_pmu.json` files from the same upstream commit.
Every file has a matching license sidecar and `document.confidential=false`.

Within each product, only `product_configuration.major_revision` and
`minor_revision` differ; events, metrics, groups, and methodology are identical.
The current API resolves by CPU name, so Pro uses the r0p0 representative without
adding revision-specific aliases.

C1-SME2 is a shared execution unit, not a separately detected CPU. Its standalone
specification is not registered as a CPU or merged into these imports. The CPU
specifications retain their own SME2 interaction metrics; exposing the separate
SME2 methodology requires an explicit selection/merging design. No hardware
accuracy or standalone SME2 profiling validation is implied by these imports.

C1 measurements use the existing renderer catalogue and fallback for unlisted
collector names. The GUI's `sl-collect-to-telemetry.ts` table maps C1 collector
names to specification metric IDs, including SME terminology and percentage
names for ratio metrics. The shared engine and GUI matching algorithms are
unchanged. Collected values and units remain supplied by the collector.

The collector source inspected during integration does not define
`sm_active_cycles_ratio` or Nano's `backend_stall_interlock_{from,to}_cme_bound`
and `backend_stall_interlock_{from,to}_cme_{flags,gpr,predicate}_bound` metrics.
Those upstream metrics and graph nodes are retained, but their values depend on
collector support. This integration does not modify collector definitions.

GUI regression tests cover collector-name binding and group membership for the
four registered C1 CPU specifications, including their SME interaction metrics.
