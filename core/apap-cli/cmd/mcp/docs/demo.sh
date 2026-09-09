#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script drives the narrated terminal session recorded for the pull
# request. record.sh supplies the isolated client fixture and daemon ports.
set -u

pause() {
  sleep "${1:-1.5}"
}

comment() {
  printf '\033[2m# %s\033[0m\n' "$1"
  pause 1
}

run_apx() {
  label=$1
  shift
  printf '\033[1;32m❯\033[0m %s\n' "$label"
  pause 0.5
  "$APX_DEMO_BIN" \
    --server-port "$APX_DEMO_SERVER_PORT" \
    --auth-port "$APX_DEMO_AUTH_PORT" \
    "$@"
  pause 2
}

clear
comment "Start with detected clients but no MCP registrations."
run_apx "apx mcp status" mcp status

comment "Add Arm Performix MCP to one client."
run_apx "apx mcp install claude-code" mcp install claude-code
run_apx "apx mcp status claude-code" mcp status claude-code
pause 4

clear
comment "Configure every other available MCP client."
run_apx "apx mcp install --all" mcp install --all
run_apx "apx mcp status" mcp status
pause 4

clear
comment "Doctor explains why an unavailable client cannot be configured."
run_apx "apx mcp doctor codex" mcp doctor codex

comment "Remove Arm Performix MCP from every configured client."
run_apx "apx mcp uninstall --all" mcp uninstall --all
pause 6
