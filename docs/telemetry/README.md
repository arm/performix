<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Product telemetry preference

`telemetry-enabled` is an APX configuration setting, enabled by default. The existing CLI configuration loader reads it from `apx.yml` (or the file selected by `--config`) and passes it to the engine alongside the other startup settings.

Clients read and update it using `GetProductTelemetryPreference` and `SetProductTelemetryPreference`. Updates save the setting in the selected configuration file and immediately update the running engine's value. `configured=false` means the setting has not been explicitly saved in the file; this supports a later migration of existing GUI preferences.

The CLI uses the same engine API:

```shell
apx config get --telemetry        # Read the current engine value
apx config set --telemetry=false  # Disable and persist
apx config set --telemetry=true   # Enable and persist
```

Both commands require the `--telemetry` flag and print `true` or `false`; `--json` uses the standard CLI response with a Boolean `data` value. The normal daemon connection and autostart behavior applies. Changes are saved in the connected engine's configuration file.

The save helper uses a separate Viper instance containing only file values, so environment overrides and defaults are not written into the file. Other setting values are retained; formatting and comments may be rewritten.

This PR supplies the preference API. Connecting GUI telemetry and future engine events to the preference is subsequent work. The GUI's existing `--disable-telemetry` override is unchanged.
