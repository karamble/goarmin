#!/usr/bin/env bash
# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
#
# node.sh runs this computer's sync node in the background: goarmin sync -server, which syncs
# the watches paired here and hands their files to the server. The Makefile's node-* targets
# call it. It is no service: after a reboot, make node-start again.
#
#   node.sh start BIN SERVER [sync flags…]   start BIN sync -server SERVER unless it runs
#   node.sh stop [force]                     SIGTERM, then wait for a running sync to end
#   node.sh status                           whether it runs and the last log lines
#   node.sh logs                             follow the log
#   node.sh pid                              print the running node's PID (nothing if none)
#
# Environment: STATE_DIR (pid file and log, default ~/.local/state/goarmin).

set -uo pipefail

STATE_DIR=${STATE_DIR:-$HOME/.local/state/goarmin}
PIDFILE=$STATE_DIR/node.pid
LOG=$STATE_DIR/node.log
STOP_WAIT=60 # seconds; a sync ends cleanly within that

# running: the node's PID from the pid file, if that process is still a goarmin.
running() {
  local pid
  pid=$(cat "$PIDFILE" 2>/dev/null)
  if [[ -n $pid ]] && kill -0 "$pid" 2>/dev/null && [[ $(ps -o comm= -p "$pid" 2>/dev/null) == goarmin ]]; then
    echo "$pid"
  fi
}

start() {
  local bin=${1:?usage: node.sh start BIN SERVER [flags]} server=${2:-}
  shift 2 || true
  [[ -x $bin ]] || { echo "no binary at $bin: run make build"; exit 1; }
  [[ -n $server ]] || { echo "no server: make node-start SERVER=http://host:8642 (or NODE_SERVER in .env)"; exit 1; }
  local pid
  pid=$(running)
  if [[ -n $pid ]]; then
    echo "the sync node already runs (pid $pid). make node-restart replaces it."
    exit 0
  fi
  # Only one goarmin may sync the watch: not a local server with the sync role beside the node.
  local other
  other=$(pgrep -u "$(id -u)" -a goarmin 2>/dev/null | grep -- ' serve ' | grep -- '-with-sync' | head -1)
  if [[ -n $other ]]; then
    echo "a local goarmin server syncs the watches itself (${other%% *}: serve -with-sync)."
    echo "Stop it (make stop), or keep its dashboard without the sync role: make restart SERVE_ARGS="
    exit 1
  fi
  mkdir -p "$STATE_DIR" && chmod 700 "$STATE_DIR"
  if [[ -f $LOG && $(stat -c %s "$LOG") -gt 10485760 ]]; then mv -f "$LOG" "$LOG.1"; fi
  local from=1
  [[ -f $LOG ]] && from=$(($(wc -l <"$LOG") + 1))
  echo "=== $(date '+%F %T') start: $bin sync -server $server $*" >>"$LOG"
  # setsid: the node keeps running after this terminal and make are gone.
  setsid nohup "$bin" sync -server "$server" "$@" >>"$LOG" 2>&1 </dev/null &
  pid=$!
  echo "$pid" >"$PIDFILE"
  sleep 2
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "the sync node did not start. The end of $LOG:"
    tail -n +"$from" "$LOG" | tail -n 20
    rm -f "$PIDFILE"
    exit 1
  fi
  echo "sync node runs for $server (pid $pid)"
  echo "log: $LOG   stop: make node-stop"
}

stop() {
  local force=${1:-} pid
  pid=$(running)
  if [[ -z $pid ]]; then
    rm -f "$PIDFILE"
    echo "the sync node is not running"
    return 0
  fi
  echo "stopping the sync node (pid $pid)…"
  kill -TERM "$pid"
  local i
  for ((i = 0; i < STOP_WAIT * 4; i++)); do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PIDFILE"
      echo "the sync node stopped"
      return 0
    fi
    ((i == 12)) && echo "waiting for a running sync or live link to end (up to ${STOP_WAIT}s)…"
    sleep 0.25
  done
  if [[ $force == force ]]; then
    kill -KILL "$pid" 2>/dev/null
    rm -f "$PIDFILE"
    echo "the sync node was killed after ${STOP_WAIT}s (files a sync pulled stay in the spool)"
    return 0
  fi
  echo "the sync node (pid $pid) still runs after ${STOP_WAIT}s; make node-stop FORCE=1 kills it"
  return 1
}

status() {
  local pid
  pid=$(running)
  if [[ -z $pid ]]; then
    echo "the sync node is not running"
    return 3
  fi
  echo "the sync node runs (pid $pid, up $(ps -o etime= -p "$pid" | tr -d ' '))"
  echo "log: $LOG"
  tail -n 5 "$LOG"
}

logs() {
  [[ -f $LOG ]] || { echo "no log yet at $LOG"; exit 1; }
  tail -n 50 -F "$LOG"
}

cmd=${1:-}
shift || true
case $cmd in
  start) start "$@" ;;
  stop) stop "$@" ;;
  status) status ;;
  logs) logs ;;
  pid) running ;;
  *) echo "usage: node.sh start BIN SERVER [flags] | stop [force] | status | logs | pid"; exit 2 ;;
esac
