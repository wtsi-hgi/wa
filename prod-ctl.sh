#!/usr/bin/env bash

# Runs `run-dev.sh --mode prod` in the background and manages it. `make
# prod-start`, `prod-stop`, `prod-status`, and `prod-restart` call this after
# loading the production env files.
#
#   start    Start the stack detached unless it is already healthy or still
#            starting. Quiet when there is nothing to do, so cron can run it
#            every few minutes.
#   stop     Stop the stack's whole process group.
#   status   Print the URLs and exit 0 when healthy; exit non-zero otherwise.
#   restart  Stop, then start.
#
# Every action refuses when .tmp/prod.pid records a stack on another host.
# start and restart refuse when no stack is recorded but a process already
# holds a prod port, so they never rotate the log of a stack they cannot see.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP_DIR="${WA_RUN_DEV_TMP_DIR:-$REPO_ROOT/.tmp}"
PID_FILE="$TMP_DIR/prod.pid"
LOG_FILE="$TMP_DIR/prod.log"
LOCK_FILE="$TMP_DIR/prod.lock"
START_TIMEOUT_SECONDS="${WA_PROD_START_TIMEOUT_SECONDS:-1800}"
STOP_TIMEOUT_SECONDS="${WA_PROD_STOP_TIMEOUT_SECONDS:-30}"
# start probes a running stack this many times, this many seconds apart, before
# it restarts it, so one slow response does not cost a rebuild.
HEALTH_ATTEMPTS="${WA_PROD_HEALTH_ATTEMPTS:-6}"
HEALTH_RETRY_SECONDS="${WA_PROD_HEALTH_RETRY_SECONDS:-5}"
# run-dev.sh prints this once its own services pass their health checks.
READY_LINE="Development environment is ready."
THIS_HOST="$(uname -n)"

action="${1:-}"

frontend_port="${WA_PROD_FRONTEND_PORT:?WA_PROD_FRONTEND_PORT is required; run this through make prod-<action>}"
results_port="${WA_PROD_RESULTS_PORT:?WA_PROD_RESULTS_PORT is required; run this through make prod-<action>}"
# The same probes run-dev.sh uses. The frontend's /api/health also checks that
# it can reach the results API.
RESULTS_HEALTH_URL="${WA_RUN_DEV_RESULTS_HEALTH_URL:-https://127.0.0.1:$results_port/rest/v1/results/stats}"
FRONTEND_HEALTH_URL="${WA_RUN_DEV_FRONTEND_HEALTH_URL:-https://127.0.0.1:$frontend_port/api/health}"

# The pid file records a stack as "<pgid> <host> <stack id>": run-dev.sh leads
# process group <pgid> on <host>, and every process in that group inherits
# WA_PROD_STACK_ID=<stack id> (see launch_stack). .tmp can be on NFS and
# survives reboots, so the pid alone identifies nothing.
read_record() {
  local line

  record_pgid=""
  record_host=""
  record_id=""
  line="$(cat "$PID_FILE" 2>/dev/null || true)"
  read -r record_pgid record_host record_id _ <<<"$line" || true

  [[ "$record_pgid" =~ ^[0-9]+$ && -n "$record_host" && -n "$record_id" ]]
}

# refuse_other_host exits when the pid file records a stack on another host,
# which only that host can inspect or signal, leaving the file in place.
refuse_other_host() {
  if read_record && [[ "$record_host" != "$THIS_HOST" ]]; then
    printf 'prod-ctl.sh: %s records a wa production stack on %s, not %s. Run make prod-%s on %s, or delete the file if that stack is gone.\n' \
      "$PID_FILE" "$record_host" "$THIS_HOST" "$action" "$record_host" >&2
    exit 1
  fi
}

carries_stack_id() {
  local pid="$1"
  local id="$2"

  grep -qzxF -- "WA_PROD_STACK_ID=$id" "/proc/$pid/environ" 2>/dev/null
}

# stack_pid prints the recorded PID while that process is still the stack's
# run-dev.sh.
stack_pid() {
  read_record && [[ "$record_host" == "$THIS_HOST" ]] || return 1
  carries_stack_id "$record_pgid" "$record_id" || return 1

  printf '%s' "$record_pgid"
}

# stack_group prints the recorded PID while its process group still has a
# member carrying the stack ID. Its services outlive a run-dev.sh that was
# killed with SIGKILL. One member is enough: Linux does not reuse a PID while
# it is still a process group ID, and a process can only join a group in its
# own session, which run-dev.sh's setsid created, so every member is the
# stack's.
stack_group() {
  local member

  read_record && [[ "$record_host" == "$THIS_HOST" ]] || return 1

  for member in $(pgrep -g "$record_pgid" || true); do
    if carries_stack_id "$member" "$record_id"; then
      printf '%s' "$record_pgid"
      return 0
    fi
  done

  return 1
}

# -k: these are loopback liveness probes, not a trust decision.
url_is_healthy() {
  curl -fsSk --max-time 5 -A wa-prod-ctl -o /dev/null "$1" >/dev/null 2>&1
}

stack_is_healthy() {
  stack_pid >/dev/null && url_is_healthy "$RESULTS_HEALTH_URL" && url_is_healthy "$FRONTEND_HEALTH_URL"
}

# stack_stays_healthy retries failed probes while run-dev.sh is running, so a
# transient failure does not restart a working stack.
stack_stays_healthy() {
  local attempt

  for (( attempt = 1; attempt <= HEALTH_ATTEMPTS; attempt++ )); do
    if stack_is_healthy; then
      return 0
    fi

    if ! stack_pid >/dev/null; then
      return 1
    fi

    if (( attempt < HEALTH_ATTEMPTS )); then
      sleep "$HEALTH_RETRY_SECONDS"
    fi
  done

  return 1
}

# stack_is_starting succeeds while the recorded run-dev.sh has not yet logged
# that it is ready and started less than START_TIMEOUT_SECONDS ago, for
# example while a start that was interrupted is still building the frontend.
stack_is_starting() {
  local pid
  local age

  pid="$(stack_pid)" || return 1
  if grep -qxF "$READY_LINE" "$LOG_FILE" 2>/dev/null; then
    return 1
  fi

  age="$(ps -o etimes= -p "$pid" 2>/dev/null | tr -d ' ')"
  [[ "$age" =~ ^[0-9]+$ ]] && (( age < START_TIMEOUT_SECONDS ))
}

port_is_listening() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1
}

# report_unrecorded_ports prints, and fails, for each prod port that a process
# is listening on although no stack is recorded.
report_unrecorded_ports() {
  local port
  local found=1

  for port in "$frontend_port" "$results_port"; do
    if port_is_listening "$port"; then
      printf 'prod-ctl.sh: port %s is in use by a process not recorded in %s. Stop that process, or restore the pid file of the stack that holds the port.\n' \
        "$port" "$PID_FILE" >&2
      found=0
    fi
  done

  return "$found"
}

# take_lock waits for the lock, saying so when run from a terminal.
take_lock() {
  exec 9>"$LOCK_FILE"
  if flock -n 9; then
    return 0
  fi

  if [[ -t 1 ]]; then
    printf 'Waiting for another prod-ctl.sh to finish.\n'
  fi
  flock 9
}

print_urls() {
  local host

  host="$(hostname -f 2>/dev/null || hostname)"
  printf 'Frontend: https://%s:%s\n' "$host" "$frontend_port"
  printf 'Results: %s\n' "${WA_RESULTS_SERVER_URL:-https://$host:$results_port}"
  if [[ -n "${WA_MLWH_SERVER_URL:-}" ]]; then
    printf 'MLWH: %s\n' "$WA_MLWH_SERVER_URL"
  fi
  printf 'Log: %s\n' "$LOG_FILE"
}

group_has_exited_within() {
  local pgid="$1"
  local tenths="$2"
  local waited=0

  while kill -0 -- "-$pgid" 2>/dev/null; do
    if (( waited >= tenths )); then
      return 1
    fi

    sleep 0.1
    waited=$((waited + 1))
  done
}

# stop_stack signals the recorded process group, so a build or server is not
# left behind if run-dev.sh is stuck or was killed. The PID file stays until
# the group is gone.
stop_stack() {
  local pgid
  local leader_was_running=0

  if ! pgid="$(stack_group)"; then
    rm -f "$PID_FILE"
    return 0
  fi

  if stack_pid >/dev/null; then
    leader_was_running=1
  fi

  kill -TERM -- "-$pgid" 2>/dev/null || true
  if ! group_has_exited_within "$pgid" $((STOP_TIMEOUT_SECONDS * 10)); then
    printf 'prod-ctl.sh: the stack (pid %s) did not stop after %ss; sending SIGKILL.\n' "$pgid" "$STOP_TIMEOUT_SECONDS" >&2
    kill -KILL -- "-$pgid" 2>/dev/null || true
    if ! group_has_exited_within "$pgid" 50; then
      printf 'prod-ctl.sh: processes in the stack'"'"'s group (pid %s) survived SIGKILL; keeping %s.\n' "$pgid" "$PID_FILE" >&2
      return 1
    fi
  fi

  # run-dev.sh removes its binary and the .tmp/wa link to it on exit; do that
  # for one that was killed before it could.
  if (( ! leader_was_running )); then
    if [[ "$(readlink "$TMP_DIR/wa" 2>/dev/null || true)" == "$TMP_DIR/wa-run-dev-$pgid" ]]; then
      rm -f "$TMP_DIR/wa"
    fi
    rm -f "$TMP_DIR/wa-run-dev-$pgid"
  fi

  rm -f "$PID_FILE"
  printf 'Stopped the wa production stack (pid %s).\n' "$pgid"
}

launch_stack() {
  local id
  local launcher
  local pid=""
  local deadline=$((SECONDS + START_TIMEOUT_SECONDS))

  if report_unrecorded_ports; then
    return 1
  fi

  if [[ -s "$LOG_FILE" ]]; then
    mv -f "$LOG_FILE" "$LOG_FILE.1"
  fi
  rm -f "$PID_FILE"
  id="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"

  # setsid makes run-dev.sh a session and process group leader detached from
  # any terminal, so its group can be signalled as a whole and it survives
  # the caller's exit. A background job of this non-interactive shell is not
  # a group leader, so setsid does not fork and $! is run-dev.sh's PID. fd 9
  # (the lock) is closed so the stack never holds it. The pid file is written
  # through a rename so it is never read half-written.
  (cd "$REPO_ROOT" && export WA_PROD_STACK_ID="$id" && exec setsid bash -c \
    'printf "%s %s %s\n" "$$" "$1" "$WA_PROD_STACK_ID" >"$2.new" && mv -f "$2.new" "$2" && exec "$3" --mode prod' \
    prod-ctl "$THIS_HOST" "$PID_FILE" "$REPO_ROOT/run-dev.sh") \
    </dev/null >>"$LOG_FILE" 2>&1 9>&- &
  launcher="$!"

  while (( SECONDS < deadline )); do
    if [[ -z "$pid" ]] && read_record; then
      pid="$record_pgid"
    fi

    if [[ -z "$pid" ]] && ! kill -0 "$launcher" 2>/dev/null; then
      printf 'prod-ctl.sh: could not launch the wa production stack. Last lines of %s:\n' "$LOG_FILE" >&2
      tail -n 40 "$LOG_FILE" >&2 || true
      return 1
    fi

    if [[ -n "$pid" ]]; then
      if ! stack_pid >/dev/null; then
        printf 'prod-ctl.sh: the wa production stack exited during startup. Last lines of %s:\n' "$LOG_FILE" >&2
        tail -n 40 "$LOG_FILE" >&2 || true
        stop_stack >/dev/null
        return 1
      fi

      # Not the health URLs: another server holding the ports would answer
      # them while this run-dev.sh fails its port check.
      if grep -qxF "$READY_LINE" "$LOG_FILE"; then
        printf 'Started the wa production stack (pid %s).\n' "$pid"
        print_urls
        return 0
      fi
    fi

    sleep 1
  done

  printf 'prod-ctl.sh: the wa production stack was not ready after %ss; stopping it. See %s.\n' "$START_TIMEOUT_SECONDS" "$LOG_FILE" >&2
  stop_stack >/dev/null
  return 1
}

mkdir -p "$TMP_DIR"

case "$action" in
  start)
    exec 9>"$LOCK_FILE"
    # Another start, stop, or restart is in progress: leave it to finish,
    # silently under cron.
    if ! flock -n 9; then
      if [[ -t 1 ]]; then
        printf 'Another prod-ctl.sh is already running; leaving it to finish.\n'
      fi
      exit 0
    fi
    refuse_other_host

    if stack_is_starting; then
      if [[ -t 1 ]]; then
        printf 'The wa production stack (pid %s) is still starting. See %s.\n' "$(stack_pid)" "$LOG_FILE"
      fi
      exit 0
    fi

    if stack_stays_healthy; then
      exit 0
    fi

    if pgid="$(stack_group)"; then
      if stack_pid >/dev/null; then
        printf 'The wa production stack (pid %s) is unhealthy; restarting it.\n' "$pgid"
      else
        printf 'The wa production stack'"'"'s run-dev.sh (pid %s) has exited, leaving its services running; restarting it.\n' "$pgid"
      fi
      stop_stack
    fi

    launch_stack
    ;;
  stop)
    take_lock
    refuse_other_host
    if ! stack_group >/dev/null; then
      rm -f "$PID_FILE"
      printf 'The wa production stack is not running.\n'
      if report_unrecorded_ports; then
        exit 1
      fi
      exit 0
    fi
    stop_stack
    ;;
  restart)
    take_lock
    refuse_other_host
    stop_stack
    launch_stack
    ;;
  status)
    refuse_other_host
    # One probe each, so status answers quickly; start retries before acting.
    if stack_is_healthy; then
      printf 'The wa production stack is running (pid %s).\n' "$(stack_pid)"
      print_urls
      exit 0
    fi

    if pid="$(stack_pid)"; then
      printf 'The wa production stack (pid %s) is running but not healthy. See %s.\n' "$pid" "$LOG_FILE" >&2
    elif pgid="$(stack_group)"; then
      printf 'The wa production stack'"'"'s run-dev.sh (pid %s) has exited, but processes in its group are still running. make prod-start or prod-restart replaces them; make prod-stop stops them.\n' "$pgid" >&2
    else
      printf 'The wa production stack is not running.\n' >&2
    fi
    exit 1
    ;;
  *)
    printf 'Usage: %s start|stop|status|restart\n' "$0" >&2
    exit 64
    ;;
esac
