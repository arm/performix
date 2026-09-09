<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# MCP client installation demo

These scripts generate the animated MCP client installation demo used in the
pull request description. The GIF lets reviewers see the `status`, `doctor`,
`install` and `uninstall` commands without building and running the CLI. It is
not used by the product or its documentation.

`demo.sh` defines the commands shown in the terminal. `record.sh` runs that
script with an isolated home directory, mock client commands and temporary
daemon ports, then renders the recording as a GIF. It does not read or update
the host's MCP client configuration.

Generate the recording from the repository root with the pinned releases of
[asciinema](https://github.com/asciinema/asciinema) and its official
[agg](https://github.com/asciinema/agg) GIF renderer:

```shell
mise x \
  github:asciinema/asciinema@3.2.1 \
  github:asciinema/agg@1.9.0 \
  -- core/apap-cli/cmd/mcp/docs/record.sh
```

The generated file is `docs/images/mcp-client-installation.gif`.
