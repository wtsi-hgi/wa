#!/usr/bin/env bash
# wa-env.sh — load the env file matching a single scenario and exec a command.
#
# Usage: scripts/wa-env.sh <test|dev|prod> -- <command> [args...]
#
# Loads exactly ONE of `.env.test`, `.env.dev`, `.env.prod` (relative to the
# repository root) for the chosen scenario and exports its variables, then
# execs the given command with the resulting environment.
#
# Cross-scenario contamination is rejected:
#   - test/dev refuse to start if WA_ENV=production was inherited from the
#     shell.
#   - prod refuses to start if WA_ENV is anything other than `production` once
#     `.env.prod` is loaded, if `.env.prod` is missing, or if any
#     WA_TEST_*_PORT or WA_DEV_*_PORT variable is inherited.
#   - test refuses to start if WA_RESULTS_DB_PATH is inherited (tests must
#     never reuse a configured dev/prod database).

set -euo pipefail

if [[ $# -lt 2 ]]; then
  printf 'usage: %s <test|dev|prod> -- <command> [args...]\n' "$0" >&2
  exit 64
fi

mode="$1"
shift

if [[ "${1:-}" != "--" ]]; then
  printf 'wa-env.sh: expected `--` separator before the command\n' >&2
  exit 64
fi
shift

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

case "$mode" in
  test)
    env_file="$repo_root/.env.test"
    expected_wa_env="test"
    ;;
  dev)
    env_file="$repo_root/.env.dev"
    expected_wa_env="development"
    ;;
  prod)
    env_file="$repo_root/.env.prod"
    expected_wa_env="production"
    ;;
  *)
    printf 'wa-env.sh: unknown scenario %q (expected test|dev|prod)\n' "$mode" >&2
    exit 64
    ;;
esac

# Reject contradictory inherited WA_ENV before we load anything.
inherited_env="${WA_ENV:-}"
if [[ "$mode" != "prod" && "$inherited_env" == "production" ]]; then
  printf 'wa-env.sh: refusing to run `make %s` with WA_ENV=production inherited from the shell.\n' "$mode" >&2
  printf 'Unset WA_ENV (or open a fresh shell) and try again.\n' >&2
  exit 1
fi

if [[ "$mode" == "test" && -n "${WA_RESULTS_DB_PATH:-}" ]]; then
  printf 'wa-env.sh: refusing to run `make test` with WA_RESULTS_DB_PATH=%q inherited from the shell.\n' "$WA_RESULTS_DB_PATH" >&2
  printf 'Tests must never run against a configured dev or production database. Unset WA_RESULTS_DB_PATH and try again.\n' >&2
  exit 1
fi

if [[ "$mode" == "prod" ]]; then
  for var in WA_TEST_FRONTEND_PORT WA_TEST_RESULTS_PORT WA_TEST_SEQMETA_PORT \
             WA_DEV_FRONTEND_PORT WA_DEV_RESULTS_PORT WA_DEV_SEQMETA_PORT; do
    if [[ -n "${!var:-}" ]]; then
      printf 'wa-env.sh: refusing to run `make prod` with %s=%q inherited from the shell.\n' "$var" "${!var}" >&2
      printf 'Open a fresh shell with only the production environment loaded and try again.\n' >&2
      exit 1
    fi
  done
fi

if [[ ! -f "$env_file" ]]; then
  if [[ "$mode" == "prod" ]]; then
    printf 'wa-env.sh: %s is required for `make prod` but does not exist.\n' "$env_file" >&2
    printf 'Copy .env.prod.example to .env.prod and fill in production values.\n' >&2
  else
    printf 'wa-env.sh: %s is required for `make %s` but does not exist.\n' "$env_file" "$mode" >&2
    printf 'Copy .env.%s.example to .env.%s and edit it to match your setup.\n' "$mode" "$mode" >&2
  fi
  exit 1
fi

# Load the scenario env file. `set -a` exports every variable defined in the
# file; we restore the previous flag state immediately after.
set -a
# shellcheck disable=SC1090
source "$env_file"
set +a

# Final sanity check on WA_ENV. We do this after sourcing so the env file
# itself is the authoritative source for WA_ENV.
if [[ "${WA_ENV:-}" != "$expected_wa_env" ]]; then
  printf 'wa-env.sh: %s sets WA_ENV=%q but `make %s` requires WA_ENV=%q.\n' \
    "$env_file" "${WA_ENV:-}" "$mode" "$expected_wa_env" >&2
  exit 1
fi

exec "$@"
