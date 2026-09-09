#!/bin/sh

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# This script records demo.sh with an isolated MCP client fixture and renders
# it with agg. It leaves the host MCP configuration unchanged.
set -eu

demo_directory=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH='' cd -- "$demo_directory/../../../../.." && pwd)
fixture=$(mktemp -d "/tmp/apx-mcp-demo.XXXXXX")
cast="$fixture/mcp-client-installation.cast"
output="$repository_root/docs/images/mcp-client-installation.gif"
apx="$repository_root/core/apap-cli/apx"
asciinema_bin=$(command -v asciinema)
agg_bin=$(command -v agg)

free_port() {
  python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

server_port=$(free_port)
auth_port=$(free_port)
while [ "$auth_port" = "$server_port" ]; do
  auth_port=$(free_port)
done

cleanup() {
  "$apx" \
    --server-port "$server_port" \
    --auth-port "$auth_port" \
    daemon stop --json >/dev/null 2>&1 || true
  rm -rf "$fixture"
}
trap cleanup EXIT INT TERM

mkdir -p \
  "$fixture/bin" \
  "$fixture/config" \
  "$fixture/data" \
  "$fixture/state"

go build \
  -o "$fixture/bin/mcp-client-stub" \
  "$repository_root/core/robot/resources/files/mcp-client-stub/main.go"
ln -s mcp-client-stub "$fixture/bin/claude"
ln -s mcp-client-stub "$fixture/bin/claude-desktop"
ln -s mcp-client-stub "$fixture/bin/code"
ln -s mcp-client-stub "$fixture/bin/cursor"

export HOME="$fixture/home"
export USERPROFILE="$HOME"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_STATE_HOME="$fixture/state"
export APXD_CONFIG_DIR="$fixture/config"
export APXD_DATA_DIR="$fixture/data"
export APXD_LOG_FILE="$fixture/state/apxd.log"
export APAP_MCP_STUB_STATE="$fixture/native-client-state"
export APAP_MCP_STUB_VSCODE_CONFIG="$HOME/Library/Application Support/Code/User/mcp.json"
export PATH="$fixture/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export APX_DEMO_BIN="$apx"
export APX_DEMO_SERVER_PORT="$server_port"
export APX_DEMO_AUTH_PORT="$auth_port"
export TERM=xterm-256color
unset NO_COLOR
export FORCE_COLOR=1

case $(uname -s) in
  Darwin)
    mkdir -p \
      "$HOME/Applications/Antigravity.app" \
      "$HOME/Applications/Claude.app" \
      "$HOME/Applications/Cursor.app"
    ;;
  Linux)
    mkdir -p \
      "$HOME/.gemini/antigravity" \
      "$HOME/.cursor" \
      "$XDG_CONFIG_HOME/Claude"
    export APAP_MCP_STUB_VSCODE_CONFIG="$XDG_CONFIG_HOME/Code/User/mcp.json"
    ;;
  *)
    printf 'Unsupported recording host: %s\n' "$(uname -s)" >&2
    exit 1
    ;;
esac

"$asciinema_bin" record \
  --quiet \
  --overwrite \
  --return \
  --idle-time-limit 4 \
  --window-size 80x31 \
  --command "$demo_directory/demo.sh" \
  "$cast"

"$agg_bin" \
  --theme github-dark \
  --font-size 14 \
  --fps-cap 30 \
  --idle-time-limit 4 \
  --last-frame-duration 6 \
  "$cast" \
  "$output"

printf 'Wrote %s\n' "$output"
