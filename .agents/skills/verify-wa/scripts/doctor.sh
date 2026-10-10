#!/usr/bin/env bash
# Non-mutating check that a stack started by launch.sh is worth driving.
# Usage: doctor.sh <run-dir>    Exit 0 only when every check passes.
set -uo pipefail

RUN="${1:?usage: doctor.sh <run-dir>}"
# shellcheck disable=SC1091
. "$RUN/stack.env"
PID="$(cat "$RUN/run-dev.pid")"
fail=0

check() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s\n' "$name"
    fail=1
  fi
}

status() { curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$@"; }

check "run-dev.sh pid $PID alive and is run-dev.sh" bash -c "ps -o args= -p $PID | grep -q run-dev.sh"
check "stack is this checkout ($REPO)" test "$(git -C "$REPO" rev-parse HEAD)" = "${REVISION%% *}"
check "results API answers over TLS" test "$(status --cacert "$CERT" "$RESULTS_URL/rest/v1/results/stats")" = 200
check "mlwh serve answers /freshness" test "$(status "$MLWH_URL/freshness")" = 200
check "mlwh serve has feedback on (GET /feedback without token is 401, not 503)" test "$(status "$MLWH_URL/feedback")" = 401
check "feedback admin token file exists" test -s "$STATE_HOME/.wa-mlwh-server.token"
check "results owner token file exists" test -s "$STATE_HOME/.wa-results-server.token"
check "frontend /api/health is 200" test "$(status -k "$FRONTEND_URL/api/health")" = 200

printf 'revision %s\n' "$REVISION"
exit "$fail"
