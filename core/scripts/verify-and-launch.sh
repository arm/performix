#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

set -u -o pipefail

readonly TIMEOUT_SECONDS=10              # wait time for the port file
readonly SLEEP_INTERVAL=0.02             # port file polling interval
readonly FRESHNESS_WINDOW=2              # allow port files written shortly before WAIT_START
readonly SIGTERM_GRACE_INTERVAL=0.1      # wait time after SIGTERM before SIGKILL
readonly LOG_TAIL_LINES=80
readonly TMP_ROOT="${TMPDIR:-/tmp}"

PID=""
PORTFILE=""
WAIT_START=0
LOGFILE=""
AGENT_EXIT_STATUS=""

now_ns() {
  date +%s%N
}

start_agent() {
  LOGFILE="$(mktemp "${TMPDIR:-/tmp}/verify-and-launch-apx-agent.log.XXXXXX")"
  nohup ./apx-agent start --console-log </dev/null >"$LOGFILE" 2>&1 &
  PID=$!
  PORTFILE="$TMP_ROOT/apx-agent_${PID}.port"
}

file_is_fresh() {
  [[ -s "$PORTFILE" ]] || return 1
  local mtime
  mtime=$(stat -c %Y "$PORTFILE" 2>/dev/null || stat -f %m "$PORTFILE" 2>/dev/null) || return 1
  (( mtime + FRESHNESS_WINDOW >= WAIT_START ))
}

wait_for_portfile() {
  WAIT_START="$(date +%s)"
  local deadline_ns=$(( $(now_ns) + TIMEOUT_SECONDS * 1000000000 ))
  for (( ; ; )); do
    file_is_fresh && return 0
    if ! kill -0 "$PID" 2>/dev/null; then
      wait "$PID" 2>/dev/null
      AGENT_EXIT_STATUS=$?
      return 2
    fi
    (( $(now_ns) >= deadline_ns )) && return 1
    sleep "$SLEEP_INTERVAL"
  done
}

kill_agent_if_running() {
  kill -0 "$PID" 2>/dev/null || return 0
  kill -TERM "$PID" 2>/dev/null || true
  sleep "$SIGTERM_GRACE_INTERVAL"
  if kill -0 "$PID" 2>/dev/null; then
    kill -KILL "$PID" 2>/dev/null || true
  fi
  wait "$PID" 2>/dev/null || true
}

error_and_exit() {
  local msg="$1"
  kill_agent_if_running
  printf '%s\n' "$msg" >&2
  if [[ -n "$LOGFILE" && -s "$LOGFILE" ]]; then
    printf 'agent launch log (%s, last %d lines):\n' "$LOGFILE" "$LOG_TAIL_LINES" >&2
    tail -n "$LOG_TAIL_LINES" "$LOGFILE" >&2 || true
  elif [[ -n "$LOGFILE" ]]; then
    printf 'agent launch log (%s) is empty\n' "$LOGFILE" >&2
  fi
  exit 1
}

start_agent
wait_for_portfile
wait_result=$?
if (( wait_result == 2 )); then
  error_and_exit "agent process exited before writing port file (exit code ${AGENT_EXIT_STATUS})"
elif (( wait_result != 0 )); then
  error_and_exit "port file not written within ${TIMEOUT_SECONDS}s"
fi
cat -- "$PORTFILE" || error_and_exit "unable to read port file"
rm -f -- "$LOGFILE"
