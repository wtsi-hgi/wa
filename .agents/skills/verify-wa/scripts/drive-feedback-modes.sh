#!/usr/bin/env bash
# Drive `wa mlwh serve` feedback configuration outside run-dev.sh, on a fresh
# never-synced cache: feedback off (no --feedback-db, no env var), feedback on
# through WA_MLWH_FEEDBACK_PATH, and a rejected :memory: path.
# Usage: drive-feedback-modes.sh <run-dir>   (needs $RUN/wa from launch.sh)
set -uo pipefail

RUN="${1:?usage: drive-feedback-modes.sh <run-dir>}"
# shellcheck disable=SC1091
. "$RUN/stack.env"
WA="$RUN/wa"
M="$RUN/modes"
EV="$RUN/evidence"
URL="http://127.0.0.1:$SPARE_PORT"
mkdir -p "$M/state"
fail=0

pass_if() {
  if [[ "$2" == "$3" ]]; then printf 'PASS %s\n' "$1"; else printf 'FAIL %s (got %s, want %s)\n' "$1" "$2" "$3"; fail=1; fi
}

start_serve() { # <log> [env assignments...]
  local log="$1"
  shift
  env -u WA_MLWH_FEEDBACK_PATH -u WA_MLWH_SERVER_TOKEN -u WA_ENV XDG_STATE_HOME="$M/state" "$@" \
    "$WA" mlwh serve --mlwh-cache "$M/cache.sqlite" --port "$SPARE_PORT" >"$log" 2>&1 &
  SERVE_PID=$!
  printf '%s\n' "$SERVE_PID" >"$RUN/modes-serve.pid"
  for _ in $(seq 1 60); do
    curl -s -o /dev/null "$URL/freshness" && return 0
    kill -0 "$SERVE_PID" 2>/dev/null || return 1
    sleep 0.5
  done
  return 1
}

stop_serve() {
  kill -TERM "$SERVE_PID" 2>/dev/null
  wait "$SERVE_PID" 2>/dev/null
  rm -f "$RUN/modes-serve.pid"
}

body='{"category":"other","description":"verify-wa modes probe"}'

# 1. Feedback off.
if start_serve "$EV/10-serve-disabled.log"; then
  code=$(curl -s -o "$EV/10-post-disabled.json" -w '%{http_code}' -X POST -H 'content-type: application/json' -d "$body" "$URL/feedback")
  pass_if "feedback off: POST /feedback is 503" "$code" 503
  pass_if "feedback off: body code is feedback_disabled" "$(grep -o '"code":"[a-z_]*"' "$EV/10-post-disabled.json")" '"code":"feedback_disabled"'
  code=$(curl -s -o "$EV/10-get-disabled.json" -w '%{http_code}' "$URL/feedback")
  pass_if "feedback off: admin GET /feedback is 503" "$code" 503
  pass_if "feedback off: no admin token file created" "$(test -e "$M/state/.wa-mlwh-server.token" && echo created || echo absent)" absent
  code=$(curl -s -o "$EV/10-freshness-never-synced.json" -w '%{http_code}' "$URL/freshness")
  pass_if "fresh cache: serve starts and /freshness is 200" "$code" 200
  stop_serve
else
  printf 'FAIL feedback off: serve did not start (see %s)\n' "$EV/10-serve-disabled.log"
  fail=1
fi

# 2. Feedback on through the env var only, same never-synced cache.
if start_serve "$EV/11-serve-envvar.log" WA_MLWH_FEEDBACK_PATH="$M/feedback/feedback.sqlite"; then
  code=$(curl -s -o "$EV/11-post-envvar.json" -w '%{http_code}' -X POST -H 'content-type: application/json' -d "$body" "$URL/feedback")
  pass_if "WA_MLWH_FEEDBACK_PATH: POST /feedback is 201" "$code" 201
  pass_if "WA_MLWH_FEEDBACK_PATH: feedback DB created mode 0600" "$(stat -c %a "$M/feedback/feedback.sqlite" 2>/dev/null)" 600
  pass_if "WA_MLWH_FEEDBACK_PATH: admin token created mode 0600" "$(stat -c %a "$M/state/.wa-mlwh-server.token" 2>/dev/null)" 600
  code=$(curl -s -o "$EV/11-admin-list-envvar.json" -w '%{http_code}' -H "Authorization: Bearer $(cat "$M/state/.wa-mlwh-server.token")" "$URL/feedback")
  pass_if "WA_MLWH_FEEDBACK_PATH: admin list is 200" "$code" 200
  code=$(curl -s -o "$EV/11-post-invalid.json" -w '%{http_code}' -X POST -d '{"category":"nope","description":"x"}' "$URL/feedback")
  pass_if "invalid category is 400" "$code" 400
  stop_serve
else
  printf 'FAIL env-var feedback: serve did not start (see %s)\n' "$EV/11-serve-envvar.log"
  fail=1
fi

# 3. Rejected path: exits non-zero. stderr is captured to show what the
# operator sees.
env -u WA_ENV XDG_STATE_HOME="$M/state" timeout 10 "$WA" mlwh serve --mlwh-cache "$M/cache.sqlite" \
  --port "$SPARE_PORT" --feedback-db :memory: >"$EV/12-serve-memory.log" 2>&1
pass_if "--feedback-db :memory: exits 1" "$?" 1
printf 'info stderr for :memory: rejection: %s bytes (%s)\n' "$(wc -c <"$EV/12-serve-memory.log")" "$EV/12-serve-memory.log"

exit "$fail"
