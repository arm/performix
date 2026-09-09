<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>

SPDX-License-Identifier: Apache-2.0
-->

# CPU telemetry specifications

This package is the single source of truth for the CPU telemetry specifications used by the engine, recipes, and API clients.

The JSON files in `data/` are obtained from the [Arm Telemetry Solution](https://gitlab.arm.com/telemetry-solution/telemetry-solution/) project. Add or update specifications here rather than packaging copies with individual clients.

- `data/public/` contains specifications approved for inclusion in the public repository.
- `data/private/` contains specifications not approved for inclusion in the public repository.
- `data/schemas/` contains the v1.0 and v1.2 JSON schemas vendored from Arm
  Telemetry Solution commit
  [`6036adbc6a0b46c7ba05363bd8d58d89ccdecba6`](https://gitlab.arm.com/telemetry-solution/telemetry-solution/-/commit/6036adbc6a0b46c7ba05363bd8d58d89ccdecba6).
  Their SHA-256 hashes are `adca64bafe17ab18e5f7b5c46c8dd583c711e6f795749294242edda2b57ec8f5`
  for v1.0 and `31f25e7f8c667941fd609f91e1973ba80d3a9cccde6f723448bfc8d4c95567c5`
  for v1.2.

Run `task core:telemetry:validate` after changing telemetry. The `$schema`
field must name one of the locally vendored schema files; validation never
downloads a schema. Public specification findings fail validation. Private
specification findings are warnings, allowing their known legacy format to
remain visible without blocking development or CI.

Specifications in `data/private/` must set `document.confidential` to `true`. The
OSSmosis built-in `json-confidential` keyword treats that metadata as a
confidential-content marker, so a private specification moved outside the
excluded directory fails the public-content scan.
