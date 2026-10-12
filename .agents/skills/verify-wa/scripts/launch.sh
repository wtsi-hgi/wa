#!/usr/bin/env bash
# Start an isolated wa stack (results API + real `wa mlwh serve` with feedback
# and usage on + Next.js dev frontend) via `run-dev.sh --mode test`, in its own
# session.
#
# Usage: launch.sh            (ports from WA_VERIFY_PORT_BASE, default 47690)
# Prints RUN=<run dir>. Everything for this run lives under that directory.
set -euo pipefail

REPO="$(cd "$(dirname "$(realpath "$0")")/../../../.." && pwd -P)"
BASE="${WA_VERIFY_PORT_BASE:-47690}"
FRONTEND_PORT=$((BASE + 1))
RESULTS_PORT=$((BASE + 2))
MLWH_PORT=$((BASE + 3))
SPARE_PORT=$((BASE + 4)) # used by drive-feedback-modes.sh

for port in "$FRONTEND_PORT" "$RESULTS_PORT" "$MLWH_PORT" "$SPARE_PORT"; do
  if ss -ltn "( sport = :$port )" | grep -q LISTEN; then
    printf 'launch.sh: port %s is in use; set WA_VERIFY_PORT_BASE to a free range.\n' "$port" >&2
    exit 1
  fi
done

RUN="$REPO/.tmp/verify-wa/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$RUN/evidence" "$RUN/logs"
cd "$REPO"

if [[ ! -x frontend/node_modules/.bin/next ]]; then
  (cd frontend && pnpm install --frozen-lockfile) >"$RUN/logs/pnpm-install.log" 2>&1
fi

# Standalone binary for drive-feedback-modes.sh and the CLIs; run-dev.sh
# builds its own copy and deletes it on shutdown.
CGO_ENABLED=1 go build -o "$RUN/wa" .

# %q so values with spaces, quotes or `$` (checkout path, the dirty-tree
# REVISION suffix) survive `. stack.env`; drive-feedback.mjs reads it via bash.
REVISION="$(git rev-parse HEAD)$(git diff --quiet HEAD -- . ':!.agents' ':!.claude' || printf ' +uncommitted')"
{
  printf 'REPO=%q\n' "$REPO"
  printf 'REVISION=%q\n' "$REVISION"
  printf 'FRONTEND_URL=%q\n' "https://127.0.0.1:$FRONTEND_PORT"
  printf 'RESULTS_URL=%q\n' "https://127.0.0.1:$RESULTS_PORT"
  printf 'MLWH_URL=%q\n' "http://127.0.0.1:$MLWH_PORT"
  printf 'SPARE_PORT=%q\n' "$SPARE_PORT"
  printf 'STATE_HOME=%q\n' "$REPO/.tmp/state-test"
  printf 'CERT=%q\n' "$REPO/.tmp/wa-dev-cert.pem"
} >"$RUN/stack.env"

(
  set -a
  # shellcheck disable=SC1091
  . ./.env.test
  set +a
  export WA_RUN_DEV_LOG_DIR="$RUN/logs"
  exec setsid ./run-dev.sh --mode test \
    --frontend-port "$FRONTEND_PORT" --results-port "$RESULTS_PORT" --seqmeta-port "$MLWH_PORT" \
    >"$RUN/run-dev.out" 2>&1 </dev/null
) &
PID=$!
printf '%s\n' "$PID" >"$RUN/run-dev.pid"

for _ in $(seq 1 600); do
  if grep -q '^Development environment is ready\.' "$RUN/run-dev.out" 2>/dev/null; then
    printf 'PGID=%q\n' "$(ps -o pgid= -p "$PID" | tr -d ' ')" >>"$RUN/stack.env"
    printf 'RUN=%s\n' "$RUN"
    exit 0
  fi
  if ! kill -0 "$PID" 2>/dev/null; then
    printf 'launch.sh: run-dev.sh exited before ready; see %s/run-dev.out and %s/logs\n' "$RUN" "$RUN" >&2
    tail -n 30 "$RUN/run-dev.out" >&2 || true
    exit 1
  fi
  sleep 1
done

printf 'launch.sh: not ready after 600s; run cleanup.sh %s\n' "$RUN" >&2
exit 1
