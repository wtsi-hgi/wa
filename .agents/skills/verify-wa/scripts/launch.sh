#!/usr/bin/env bash
# Start an isolated wa stack (results API + real `wa mlwh serve` with feedback
# on + Next.js dev frontend) via `run-dev.sh --mode test`, in its own session.
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

{
  printf 'REPO=%s\n' "$REPO"
  printf 'REVISION=%s%s\n' "$(git rev-parse HEAD)" "$(git diff --quiet HEAD -- . ':!.agents' ':!.claude' || printf ' +uncommitted')"
  printf 'FRONTEND_URL=https://127.0.0.1:%s\n' "$FRONTEND_PORT"
  printf 'RESULTS_URL=https://127.0.0.1:%s\n' "$RESULTS_PORT"
  printf 'MLWH_URL=http://127.0.0.1:%s\n' "$MLWH_PORT"
  printf 'SPARE_PORT=%s\n' "$SPARE_PORT"
  printf 'STATE_HOME=%s/.tmp/state-test\n' "$REPO"
  printf 'CERT=%s/.tmp/wa-dev-cert.pem\n' "$REPO"
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
    printf 'PGID=%s\n' "$(ps -o pgid= -p "$PID" | tr -d ' ')" >>"$RUN/stack.env"
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
