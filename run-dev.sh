#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_PORT=3000
RESULTS_PORT=8090
SEQMETA_PORT=8091

while [[ $# -gt 0 ]]; do
  case "$1" in
    -f|--frontend-port)
      FRONTEND_PORT="${2:?missing value for $1}"
      shift 2
      ;;
    --frontend-port=*)
      FRONTEND_PORT="${1#*=}"
      shift
      ;;
    -r|--results-port)
      RESULTS_PORT="${2:?missing value for $1}"
      shift 2
      ;;
    --results-port=*)
      RESULTS_PORT="${1#*=}"
      shift
      ;;
    -s|--seqmeta-port)
      SEQMETA_PORT="${2:?missing value for $1}"
      shift 2
      ;;
    --seqmeta-port=*)
      SEQMETA_PORT="${1#*=}"
      shift
      ;;
    -h|--help)
      cat <<'EOF'
Usage: ./run-dev.sh [options]

Options:
  -f, --frontend-port PORT  Frontend port (default: 3000)
  -r, --results-port PORT   Results API port (default: 8090)
  -s, --seqmeta-port PORT   Seqmeta API port (default: 8091)
EOF
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

validate_port() {
  local value="$1"
  local name="$2"

  if [[ ! "$value" =~ ^[0-9]+$ ]]; then
    printf '%s must be an integer port number\n' "$name" >&2
    exit 1
  fi

  if (( value < 1 || value > 65535 )); then
    printf '%s must be between 1 and 65535\n' "$name" >&2
    exit 1
  fi
}

validate_port "$FRONTEND_PORT" "frontend port"
validate_port "$RESULTS_PORT" "results port"
validate_port "$SEQMETA_PORT" "seqmeta port"

TMP_DIR="$REPO_ROOT/.tmp"
BIN_PATH="$TMP_DIR/wa"
LOG_DIR="$REPO_ROOT/logs"
SEED_PATH="$REPO_ROOT/.docs/results-web/fixtures/seed.json"
FRONTEND_DIR="${WA_RUN_DEV_FRONTEND_CWD:-$REPO_ROOT/frontend}"
RESULTS_LOG="$LOG_DIR/results.log"
SEQMETA_LOG="$LOG_DIR/seqmeta.log"
FRONTEND_LOG="$LOG_DIR/frontend.log"

FRONTEND_LINT_CMD="${WA_RUN_DEV_FRONTEND_LINT_CMD:-pnpm exec eslint --no-error-on-unmatched-pattern}"
FRONTEND_FORMAT_CMD="${WA_RUN_DEV_FRONTEND_FORMAT_CMD:-pnpm exec prettier --check --ignore-unknown}"
FRONTEND_TEST_CMD="${WA_RUN_DEV_FRONTEND_TEST_CMD:-pnpm test}"
FRONTEND_DEV_CMD="${WA_RUN_DEV_FRONTEND_DEV_CMD:-pnpm dev --port $FRONTEND_PORT}"

RESULTS_HEALTH_URL="${WA_RUN_DEV_RESULTS_HEALTH_URL:-http://127.0.0.1:$RESULTS_PORT/results/stats}"
FRONTEND_HEALTH_URL="${WA_RUN_DEV_FRONTEND_HEALTH_URL:-http://127.0.0.1:$FRONTEND_PORT/api/health}"
SEQMETA_HEALTH_URL="${WA_RUN_DEV_SEQMETA_HEALTH_URL:-http://127.0.0.1:$SEQMETA_PORT/studies}"

cd "$REPO_ROOT"

mkdir -p "$TMP_DIR" "$LOG_DIR"

PIDS=()
DB_PATH=""
CLEANED_UP=0

cleanup() {
  local exit_code="$1"

  if [[ "$CLEANED_UP" -eq 1 ]]; then
    return
  fi

  CLEANED_UP=1
  trap - EXIT INT TERM

  for pid in "${PIDS[@]:-}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done

  for pid in "${PIDS[@]:-}"; do
    if kill -0 "$pid" 2>/dev/null; then
      wait "$pid" 2>/dev/null || true
    fi
  done

  if [[ -n "$DB_PATH" ]]; then
    rm -f "$DB_PATH"
  fi

  return "$exit_code"
}

on_signal() {
  cleanup 0 || true
  exit 0
}

on_exit() {
  local exit_code=$?
  cleanup "$exit_code" || true
}

trap on_signal INT TERM
trap on_exit EXIT

wait_for_http() {
  local label="$1"
  local url="$2"
  local mode="$3"
  local attempt=0

  while (( attempt < 120 )); do
    if [[ "$mode" == "strict" ]]; then
      if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
        return 0
      fi
    else
      if curl -sS --max-time 2 -o /dev/null "$url" 2>/dev/null; then
        return 0
      fi
    fi

    attempt=$((attempt + 1))
    sleep 0.25
  done

  printf 'Timed out waiting for %s at %s\n' "$label" "$url" >&2
  return 1
}

run_frontend_step() {
  local label="$1"
  local command="$2"
  shift 2
  local quoted_args=""

  if (( $# > 0 )); then
    quoted_args=" $(quote_shell_args "$@")"
  fi

  printf '\n[%s]\n' "$label" >>"$FRONTEND_LOG"
  (
    cd "$FRONTEND_DIR"
    eval "$command$quoted_args"
  ) >>"$FRONTEND_LOG" 2>&1
}

quote_shell_args() {
  local quoted=()
  local arg

  for arg in "$@"; do
    quoted+=("$(printf '%q' "$arg")")
  done

  printf '%s' "${quoted[*]}"
}

list_changed_frontend_files() {
  if [[ -n "${WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD:-}" ]]; then
    eval "$WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD"
    return
  fi

  if ! git -C "$REPO_ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return
  fi

  {
    git -C "$REPO_ROOT" diff --name-only --diff-filter=ACMR -- frontend
    git -C "$REPO_ROOT" diff --cached --name-only --diff-filter=ACMR -- frontend
    git -C "$REPO_ROOT" ls-files --others --exclude-standard -- frontend
  } | sed -n 's#^frontend/##p' | awk 'NF' | sort -u
}

is_relevant_frontend_changed_file() {
  local path="$1"

  case "$path" in
    ""|/*|../*|*/../*|.next/*|node_modules/*|test-results/*|coverage/*|dist/*|out/*|*.tsbuildinfo)
      return 1
      ;;
    app/*|components/*|e2e/*|lib/*|tests/*)
      return 0
      ;;
    .env.example|.gitignore|.prettierignore|components.json|eslint.config.mjs|next-env.d.ts|next.config.ts|package.json|playwright.config.ts|pnpm-lock.yaml|postcss.config.cjs|tsconfig.json|vitest.config.ts)
      return 0
      ;;
  esac

  return 1
}

filter_relevant_frontend_changed_files() {
  local path

  for path in "$@"; do
    if is_relevant_frontend_changed_file "$path"; then
      printf '%s\n' "$path"
    fi
  done
}

filter_frontend_lint_files() {
  local path

  for path in "$@"; do
    case "$path" in
      *.js|*.cjs|*.mjs|*.jsx|*.ts|*.tsx|*.cts|*.mts)
        printf '%s\n' "$path"
        ;;
    esac
  done
}

run_frontend_changed_file_checks() {
  local changed_frontend_files=()
  local relevant_frontend_files=()
  local lint_files=()

  mapfile -t changed_frontend_files < <(list_changed_frontend_files)

  if (( ${#changed_frontend_files[@]} == 0 )); then
    printf 'No changed frontend files found; skipping frontend lint and format checks.\n'
    return
  fi

  mapfile -t relevant_frontend_files < <(filter_relevant_frontend_changed_files "${changed_frontend_files[@]}")

  if (( ${#relevant_frontend_files[@]} == 0 )); then
    printf 'No relevant changed frontend files found; skipping frontend lint and format checks.\n'
    return
  fi

  printf 'Running frontend lint on %d changed file(s)\n' "${#relevant_frontend_files[@]}"
  mapfile -t lint_files < <(filter_frontend_lint_files "${relevant_frontend_files[@]}")

  if (( ${#lint_files[@]} == 0 )); then
    printf 'No lintable changed frontend files found; skipping frontend lint.\n'
  else
    run_frontend_step "lint" "$FRONTEND_LINT_CMD" "${lint_files[@]}"
  fi

  printf 'Running frontend format checks on %d changed file(s)\n' "${#relevant_frontend_files[@]}"
  run_frontend_step "format" "$FRONTEND_FORMAT_CMD" "${relevant_frontend_files[@]}"
}

seed_results() {
  node - "$REPO_ROOT" "$SEED_PATH" "$WA_RESULTS_BACKEND_URL" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");

async function main() {
  const [repoRoot, seedPath, resultsBaseUrl] = process.argv.slice(2);
  const registrations = JSON.parse(fs.readFileSync(seedPath, "utf8"));

  for (const registration of registrations) {
    const outputDirectory = path.resolve(repoRoot, registration.output_directory);
    const files = registration.files.map((file) => {
      const absolutePath = path.isAbsolute(file.path)
        ? file.path
        : path.resolve(outputDirectory, file.path);
      const stats = fs.statSync(absolutePath);

      return {
        ...file,
        path: absolutePath,
        mtime: stats.mtime.toISOString(),
        size: stats.size,
      };
    });

    const payload = {
      ...registration,
      output_directory: outputDirectory,
      files,
    };

    const response = await fetch(new URL("/results", resultsBaseUrl), {
      method: "POST",
      headers: {
        "content-type": "application/json",
      },
      body: JSON.stringify(payload),
    });

    if (!response.ok) {
      throw new Error(`seed failed with ${response.status}: ${await response.text()}`);
    }
  }
}

main().catch((error) => {
  console.error(error.message || error);
  process.exit(1);
});
NODE
}

printf 'Building Go binary at %s\n' "$BIN_PATH"
go build -o "$BIN_PATH" .

DB_PATH="$(mktemp "$TMP_DIR/results-dev.XXXXXX.sqlite")"
export WA_RESULTS_DB_PATH="$DB_PATH"
export WA_RESULTS_BACKEND_URL="http://127.0.0.1:$RESULTS_PORT"
unset WA_SEQMETA_BACKEND_URL

: >"$RESULTS_LOG"
: >"$FRONTEND_LOG"

printf 'Starting results server on %s\n' "$WA_RESULTS_BACKEND_URL"
"$BIN_PATH" results serve --port "$RESULTS_PORT" --db "$DB_PATH" >"$RESULTS_LOG" 2>&1 &
PIDS+=("$!")

wait_for_http "results server" "$RESULTS_HEALTH_URL" "strict"

printf 'Seeding fixtures from %s\n' "$SEED_PATH"
seed_results >>"$RESULTS_LOG" 2>&1

if [[ -n "${SAGA_API_TOKEN:-}" ]]; then
  export WA_SEQMETA_BACKEND_URL="http://127.0.0.1:$SEQMETA_PORT"
  : >"$SEQMETA_LOG"
  printf 'Starting seqmeta server on %s\n' "$WA_SEQMETA_BACKEND_URL"
  "${BIN_PATH}" seqmeta serve --port "$SEQMETA_PORT" >"$SEQMETA_LOG" 2>&1 &
  PIDS+=("$!")
  wait_for_http "seqmeta server" "$SEQMETA_HEALTH_URL" "relaxed"
else
  printf 'seqmeta server skipped because SAGA_API_TOKEN is unset\n' >"$SEQMETA_LOG"
fi

run_frontend_changed_file_checks

printf 'Running frontend tests\n'
run_frontend_step "test" "$FRONTEND_TEST_CMD"

printf '\n[dev]\n' >>"$FRONTEND_LOG"
printf 'Starting frontend dev server on http://127.0.0.1:%s\n' "$FRONTEND_PORT"
(
  cd "$FRONTEND_DIR"
  eval "$FRONTEND_DEV_CMD"
) >>"$FRONTEND_LOG" 2>&1 &
PIDS+=("$!")
FRONTEND_PID="$!"

wait_for_http "frontend health" "$FRONTEND_HEALTH_URL" "strict"

printf 'Development environment is ready.\n'
printf 'Results: %s\n' "$WA_RESULTS_BACKEND_URL"
if [[ -n "${WA_SEQMETA_BACKEND_URL:-}" ]]; then
  printf 'Seqmeta: %s\n' "$WA_SEQMETA_BACKEND_URL"
fi
printf 'Frontend: http://127.0.0.1:%s\n' "$FRONTEND_PORT"
printf 'Logs: %s\n' "$LOG_DIR"

wait "$FRONTEND_PID"
