<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>

SPDX-License-Identifier: Apache-2.0
-->

# CPU telemetry specifications

This package is the single source of truth for the CPU telemetry specifications used by the engine, recipes, and API clients.

The JSON files in `data/` are obtained from the [Arm Telemetry Solution](https://gitlab.arm.com/telemetry-solution/telemetry-solution/) project. Add or update specifications here rather than packaging copies with individual clients.

- `data/public/` contains specifications approved for inclusion in the public repository.
- `data/private/` contains specifications not approved for inclusion in the public repository.

Specifications in `data/private/` must set `document.confidential` to `true`. The
OSSmosis built-in `json-confidential` keyword treats that metadata as a
confidential-content marker, so a private specification moved outside the
excluded directory fails the public-content scan.

Neoverse V3AE currently uses the Neoverse V3 telemetry specification.
