#!/usr/bin/env bash
# Stop the stack a launch.sh run started, by its recorded PID and process
# group only. Evidence under <run-dir>/evidence is kept.
# Usage: cleanup.sh <run-dir>
set -uo pipefail

RUN="${1:?usage: cleanup.sh <run-dir>}"
# shellcheck disable=SC1091
. "$RUN/stack.env"
PID="$(cat "$RUN/run-dev.pid")"
PGID="${PGID:-$PID}"

# run-dev.sh traps TERM and stops its direct children, then deletes its
# ephemeral DBs and binary.
kill -TERM "$PID" 2>/dev/null || true
for _ in $(seq 1 40); do
  kill -0 "$PID" 2>/dev/null || break
  sleep 0.5
done

# run-dev.sh only signals pnpm, so `next dev` and its workers survive; they
# stay in the session's process group.
if ps -eo pgid= | grep -qx " *$PGID"; then
  kill -TERM -- "-$PGID" 2>/dev/null || true
  sleep 2
  ps -eo pgid= | grep -qx " *$PGID" && kill -KILL -- "-$PGID" 2>/dev/null
fi

# A standalone serve left by an interrupted drive-feedback-modes.sh.
if [[ -s "$RUN/modes-serve.pid" ]]; then
  kill -TERM "$(cat "$RUN/modes-serve.pid")" 2>/dev/null || true
fi

leftover=0
for url in "$FRONTEND_URL" "$RESULTS_URL" "$MLWH_URL"; do
  port="${url##*:}"
  if ss -ltn "( sport = :$port )" | grep -q LISTEN; then
    printf 'cleanup.sh: port %s still listening\n' "$port" >&2
    leftover=1
  fi
done
if ps -eo pgid= | grep -qx " *$PGID"; then
  printf 'cleanup.sh: processes remain in group %s\n' "$PGID" >&2
  leftover=1
fi

printf 'evidence kept: %s/evidence (%s files)\n' "$RUN" "$(find "$RUN/evidence" -type f | wc -l)"
exit "$leftover"
