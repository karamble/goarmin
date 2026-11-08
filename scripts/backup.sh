#!/usr/bin/env bash
# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
#
# backup.sh lists, cleans and restores goarmin's backups; the Makefile's backup targets call it.
# The archives themselves are written by `goarmin backup` (make backup): a consistent snapshot of
# the database with the raw files, the watch spool and node.json.
#
#   backup.sh list                        list the archives and data set aside by restores
#   backup.sh clean                       remove archives older than KEEP_DAYS, keeping KEEP_MIN
#   backup.sh restore ARCHIVE [START…]    stop the server, swap the data in, start it again with
#                                         START (a goarmin binary and its serve flags) if it ran
#
# Environment: DATA (data directory), BACKUP_DIR, SQLITE (sqlite3 binary), KEEP_DAYS (30),
# KEEP_MIN (3), YES=1 (restore without asking), LISTEN and STATE_DIR (passed to server.sh).
#
# The Bluetooth bond with the watch lives in BlueZ (/var/lib/bluetooth), not here.

set -uo pipefail
umask 077

DATA=${DATA:?DATA must name the data directory}
BACKUP_DIR=${BACKUP_DIR:-$HOME/goarmin-backups}
SQLITE=${SQLITE:-sqlite3}
KEEP_DAYS=${KEEP_DAYS:-30}
KEEP_MIN=${KEEP_MIN:-3}
DB=$DATA/goarmin.db
HERE=$(cd "$(dirname "$0")" && pwd)

RED=$(tput -Txterm setaf 1 2>/dev/null)
GREEN=$(tput -Txterm setaf 2 2>/dev/null)
YELLOW=$(tput -Txterm setaf 3 2>/dev/null)
BLUE=$(tput -Txterm setaf 4 2>/dev/null)
NC=$(tput -Txterm sgr0 2>/dev/null)

die() {
  echo "${RED}✗ $*${NC}" >&2
  exit 1
}

need_sqlite() {
  command -v "$SQLITE" >/dev/null || die "$SQLITE is not installed (the backup needs the sqlite3 command line tool)"
}

# check_db FILE: the file is a sound goarmin database.
check_db() {
  [[ -s $1 ]] || return 1
  [[ $("$SQLITE" "$1" 'PRAGMA integrity_check;' 2>&1) == ok ]] || return 1
  "$SQLITE" "$1" 'SELECT count(*) FROM schema_version;' >/dev/null 2>&1
}

list() {
  echo "${BLUE}Backups in $BACKUP_DIR:${NC}"
  local found=0 f
  while IFS= read -r f; do
    found=1
    printf '  %s  %6s  %s\n' "$(date -r "$f" '+%F %R')" "$(du -h "$f" | cut -f1)" "$(basename "$f")"
  done < <(ls -1t "$BACKUP_DIR"/goarmin_backup_*.tar.gz 2>/dev/null)
  ((found)) || echo "  none yet (make backup)"
  local aside
  aside=$(ls -1d "$DATA".before-restore-* 2>/dev/null)
  if [[ -n $aside ]]; then
    echo "${BLUE}Data set aside by restores (delete when no longer needed):${NC}"
    while IFS= read -r f; do printf '  %6s  %s\n' "$(du -sh "$f" | cut -f1)" "$f"; done <<<"$aside"
  fi
}

clean() {
  local all old=() i=0 f
  mapfile -t all < <(ls -1t "$BACKUP_DIR"/goarmin_backup_*.tar.gz 2>/dev/null)
  for f in "${all[@]}"; do
    i=$((i + 1))
    ((i <= KEEP_MIN)) && continue # the newest few stay, however old
    [[ -n $(find "$f" -mtime +"$KEEP_DAYS" 2>/dev/null) ]] && old+=("$f")
  done
  if ((${#old[@]} == 0)); then
    echo "${GREEN}Nothing to remove (keeping the newest $KEEP_MIN and anything from the last $KEEP_DAYS days)${NC}"
    return
  fi
  for f in "${old[@]}"; do
    rm -f "$f" && echo "removed $(basename "$f")"
  done
  echo "${GREEN}✓ Removed ${#old[@]} backup(s) older than $KEEP_DAYS days${NC}"
}

restore() {
  need_sqlite
  local arg=${1:-}
  shift || true
  [[ ${1:-} == -- ]] && shift
  if [[ -z $arg ]]; then
    echo "${RED}Name the backup: make restore BACKUP=goarmin_backup_YYYYMMDD_HHMMSS.tar.gz${NC}"
    list
    exit 1
  fi
  local f=$arg
  [[ -f $f ]] || f=$BACKUP_DIR/$arg
  [[ -f $f ]] || die "no backup $arg (make backup-list shows them)"
  # The whole archive must read back before anything is touched.
  local listing top
  listing=$(tar -tzf "$f" 2>/dev/null) || die "$f is not a complete archive; nothing was changed"
  top=${listing%%/*}
  grep -qx "$top/goarmin.db" <<<"$listing" || die "$f is not a goarmin backup; nothing was changed"

  echo "${YELLOW}=====================================${NC}"
  echo "${YELLOW}Restore $(basename "$f") into $DATA${NC}"
  echo "${YELLOW}=====================================${NC}"
  echo "The server is stopped for the swap. The current data is kept aside, not deleted."
  if [[ ${YES:-} != 1 ]]; then
    read -r -p "Restore? [y/N] " -n 1 reply
    echo
    [[ $reply =~ ^[Yy]$ ]] || { echo "${RED}Restore cancelled${NC}"; exit 1; }
  fi

  local ts new aside
  ts=$(date +%Y%m%d_%H%M%S)
  new=$DATA.restore-$ts
  aside=$DATA.before-restore-$ts
  mkdir -p "$new" || die "cannot create $new"
  echo "${YELLOW}[1/4] Unpacking…${NC}"
  tar -xzf "$f" -C "$new" --strip-components=1 || { rm -rf "$new"; die "unpacking failed; nothing was changed"; }
  echo "${YELLOW}[2/4] Checking the database…${NC}"
  check_db "$new/goarmin.db" || { rm -rf "$new"; die "the backup's database is damaged; nothing was changed"; }

  echo "${YELLOW}[3/4] Stopping the server…${NC}"
  local was
  was=$("$HERE/server.sh" pid)
  if [[ -n $was ]]; then
    "$HERE/server.sh" stop || { rm -rf "$new"; die "the server did not stop; nothing was changed"; }
  fi

  echo "${YELLOW}[4/4] Swapping the data in…${NC}"
  if [[ -e $DATA ]]; then
    mv "$DATA" "$aside" || { rm -rf "$new"; die "could not move $DATA aside; nothing was changed"; }
  fi
  mv "$new" "$DATA" && chmod 700 "$DATA" || die "could not move the restored data into place; your previous data is in $aside"
  echo "${GREEN}✓ Restored $(basename "$f") into $DATA${NC}"
  [[ -e $aside ]] && echo "  your previous data is kept in $aside"

  if [[ -n $was && $# -gt 0 ]]; then
    "$HERE/server.sh" start "$@"
  elif [[ -n $was ]]; then
    echo "The server was running; start it again with make start."
  fi
}

cmd=${1:-}
shift || true
case $cmd in
  list) list ;;
  clean) clean ;;
  restore) restore "$@" ;;
  *) echo "usage: backup.sh list | clean | restore ARCHIVE [-- BIN serve-flags…]"; exit 2 ;;
esac
