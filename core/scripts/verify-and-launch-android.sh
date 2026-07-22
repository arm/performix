#!/system/bin/sh

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Launches the target agent, waits for its port file, and prints the port

set -u

readonly TIMEOUT_SECONDS=10
readonly SLEEP_FALLBACK_INTERVAL=1
readonly PORTFILE_CHECK_INTERVAL=0.02
readonly FRESHNESS_WINDOW=2
readonly SIGTERM_GRACE_INTERVAL=0.1
readonly LOG_TAIL_LINES=80
readonly AGENT_NAME=apx-agent
readonly AGENT_BIN=./apx-agent
readonly TMP_ROOT="${TMPDIR:-/data/local/tmp}"

PID=""
PORTFILE=""
WAIT_START=0
LOGFILE=""
AGENT_EXIT_STATUS=""

now_seconds() {
  date +%s
}

# POSIX sleep may not support fractional seconds, so fallback to an integer interval if the fractional sleep fails
portable_sleep() {
  sleep "$1" 2>/dev/null && return 0
  sleep "$SLEEP_FALLBACK_INTERVAL"
}

make_temp_log() {
  if command -v mktemp >/dev/null 2>&1; then
    mktemp "$TMP_ROOT/verify-and-launch-${AGENT_NAME}.log.XXXXXX" 2>/dev/null && return 0
  fi

  # if mktemp is not available, generate a unique log filepath to pass to the target agent
  printf '%s\n' "$TMP_ROOT/verify-and-launch-${AGENT_NAME}.$$.$(now_seconds).log"
}

start_agent() {
  [ -x "$AGENT_BIN" ] || return 1

  command -v nohup >/dev/null 2>&1 || return 2

  LOGFILE=$(make_temp_log) || return 3

  nohup "$AGENT_BIN" start --console-log </dev/null >"$LOGFILE" 2>&1 &
  PID=$!
  PORTFILE="$TMP_ROOT/${AGENT_NAME}_${PID}.port"
}

file_is_fresh() {
  [ -s "$PORTFILE" ] || return 1
  mtime=$(stat -c %Y "$PORTFILE" 2>/dev/null || stat -f %m "$PORTFILE" 2>/dev/null) || return 1
  [ $((mtime + FRESHNESS_WINDOW)) -ge "$WAIT_START" ]
}

wait_for_portfile() {
  WAIT_START=$(now_seconds)

  deadline=$(($(now_seconds) + TIMEOUT_SECONDS))
  while :; do
    file_is_fresh && return 0

    if [ -n "$PID" ] && ! kill -0 "$PID" 2>/dev/null; then
      wait "$PID" 2>/dev/null
      AGENT_EXIT_STATUS=$?
      return 2
    fi

    [ "$(now_seconds)" -ge "$deadline" ] && return 1
    portable_sleep "$PORTFILE_CHECK_INTERVAL"
  done
}

kill_agent_if_running() {
  [ -n "$PID" ] || return 0
  kill -0 "$PID" 2>/dev/null || return 0
  kill -TERM "$PID" 2>/dev/null || true
  portable_sleep "$SIGTERM_GRACE_INTERVAL"
  if kill -0 "$PID" 2>/dev/null; then
    kill -KILL "$PID" 2>/dev/null || true
  fi
  wait "$PID" 2>/dev/null || true
}

cleanup() {
  [ -n "$LOGFILE" ] && rm -f "$LOGFILE" 2>/dev/null || true
}

error_and_exit() {
  msg=$1
  kill_agent_if_running
  printf '%s\n' "$msg" >&2
  if [ -n "$LOGFILE" ] && [ -s "$LOGFILE" ]; then
    printf 'agent launch log (%s, last %d lines):\n' "$LOGFILE" "$LOG_TAIL_LINES" >&2
    tail -n "$LOG_TAIL_LINES" "$LOGFILE" >&2 || true
  elif [ -n "$LOGFILE" ]; then
    printf 'agent launch log (%s) is empty\n' "$LOGFILE" >&2
  fi
  exit 1
}

start_agent
start_result=$?
case "$start_result" in
0) ;;
1) error_and_exit "agent executable not found or not executable: $AGENT_BIN" ;;
2) error_and_exit "nohup is required to launch the agent detached from adb shell" ;;
3) error_and_exit "failed to create unique log file location" ;;
*) error_and_exit "unable to start agent process" ;;
esac

wait_for_portfile
wait_result=$?
case "$wait_result" in
0) ;;
1) error_and_exit "port file not written within ${TIMEOUT_SECONDS}s" ;;
2) error_and_exit "agent process exited before writing port file (exit code ${AGENT_EXIT_STATUS})" ;;
*) error_and_exit "unable to find port file" ;;
esac

IFS= read -r port <"$PORTFILE" || error_and_exit "unable to read port file"
case "$port" in
"" | *[!0-9]*) error_and_exit "port file contains non-numeric port: $port" ;;
esac

cleanup
printf '%s\n' "$port"
