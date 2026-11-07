#!/usr/bin/env bash
# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
#
# server.sh runs the local goarmin server in the background: the Makefile's start, stop,
# status and logs targets call it.
#
#   server.sh start BIN [serve flags…]   start BIN serve -listen $LISTEN [flags] unless it runs
#   server.sh stop [force]               SIGTERM, then wait for a running sync to end cleanly
#   server.sh status                     whether it runs, where, and the last log lines
#   server.sh logs                       follow the log
#   server.sh pid                        print the running server's PID (nothing if none)
#
# Environment: LISTEN (host:port, default 127.0.0.1:8642), STATE_DIR (pid file and log,
# default ~/.local/state/goarmin). stop and status also find a goarmin of this user that was
# started another way (e.g. in a terminal) by the port it listens on.

set -uo pipefail

LISTEN=${LISTEN:-127.0.0.1:8642}
STATE_DIR=${STATE_DIR:-$HOME/.local/state/goarmin}
PIDFILE=$STATE_DIR/serve.pid
LOG=$STATE_DIR/serve.log
PORT=${LISTEN##*:}
URL=http://localhost:$PORT
STOP_WAIT=60 # seconds; the server itself gives a sync 45 s to end

# is_goarmin PID: a live process of this user whose program is goarmin.
is_goarmin() {
  [[ -n ${1:-} ]] && kill -0 "$1" 2>/dev/null && [[ $(ps -o comm= -p "$1" 2>/dev/null) == goarmin ]]
}

# listener: the PID listening on PORT, if this user may see it.
listener() {
  ss -ltnpH "sport = :$PORT" 2>/dev/null | grep -o 'pid=[0-9]*' | head -1 | cut -d= -f2
}

port_busy() {
  ss -ltnH "sport = :$PORT" 2>/dev/null | grep -q .
}

# running: the goarmin PID serving here, from the pid file or the port.
running() {
  local pid
  pid=$(cat "$PIDFILE" 2>/dev/null)
  if is_goarmin "$pid"; then echo "$pid"; return; fi
  pid=$(listener)
  if is_goarmin "$pid"; then echo "$pid"; fi
}

start() {
  local bin=${1:?usage: server.sh start BIN [flags]}
  shift
  [[ -x $bin ]] || { echo "no binary at $bin: run make build"; exit 1; }
  local pid
  pid=$(running)
  if [[ -n $pid ]]; then
    echo "goarmin already runs on $URL (pid $pid). make restart replaces it."
    exit 0
  fi
  if port_busy; then
    echo "something else listens on $LISTEN; stop it or use make start LISTEN=127.0.0.1:<port>"
    exit 1
  fi
  mkdir -p "$STATE_DIR" && chmod 700 "$STATE_DIR"
  # Keep one previous log.
  if [[ -f $LOG && $(stat -c %s "$LOG") -gt 10485760 ]]; then mv -f "$LOG" "$LOG.1"; fi
  local from
  from=1
  [[ -f $LOG ]] && from=$(($(wc -l <"$LOG") + 1))
  echo "=== $(date '+%F %T') start: $bin serve -listen $LISTEN $*" >>"$LOG"
  # setsid: the server keeps running after this terminal and make are gone.
  setsid nohup "$bin" serve -listen "$LISTEN" "$@" >>"$LOG" 2>&1 </dev/null &
  pid=$!
  echo "$pid" >"$PIDFILE"
  for _ in $(seq 1 60); do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "goarmin did not start. The end of $LOG:"
      tail -n +"$from" "$LOG" | tail -n 20
      rm -f "$PIDFILE"
      exit 1
    fi
    port_busy && break
    sleep 0.25
  done
  echo "goarmin runs on $URL (pid $pid)"
  echo "log: $LOG   stop: make stop"
  # First run: the one-time setup link.
  tail -n +"$from" "$LOG" | grep -A1 '^First run' || true
}

stop() {
  local force=${1:-} pid
  pid=$(running)
  if [[ -z $pid ]]; then
    rm -f "$PIDFILE"
    if port_busy; then echo "no goarmin of yours runs on $LISTEN (something else listens there)"; else echo "goarmin is not running"; fi
    return 0
  fi
  echo "stopping goarmin (pid $pid)…"
  kill -TERM "$pid"
  local i
  for ((i = 0; i < STOP_WAIT * 4; i++)); do
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$PIDFILE"
      echo "goarmin stopped"
      return 0
    fi
    ((i == 12)) && echo "waiting for a running sync or live link to end (up to ${STOP_WAIT}s)…"
    sleep 0.25
  done
  if [[ $force == force ]]; then
    kill -KILL "$pid" 2>/dev/null
    rm -f "$PIDFILE"
    echo "goarmin killed after ${STOP_WAIT}s (files a sync pulled stay in the spool)"
    return 0
  fi
  echo "goarmin (pid $pid) still runs after ${STOP_WAIT}s; make stop FORCE=1 kills it"
  return 1
}

status() {
  local pid
  pid=$(running)
  if [[ -z $pid ]]; then
    echo "goarmin is not running ($LISTEN)"
    return 3
  fi
  echo "goarmin runs on $URL (pid $pid, up $(ps -o etime= -p "$pid" | tr -d ' '))"
  if [[ $(cat "$PIDFILE" 2>/dev/null) == "$pid" ]]; then
    echo "log: $LOG"
    tail -n 5 "$LOG"
  else
    echo "started outside make (its log is in the terminal that runs it)"
  fi
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
  *) echo "usage: server.sh start BIN [flags] | stop [force] | status | logs | pid"; exit 2 ;;
esac
