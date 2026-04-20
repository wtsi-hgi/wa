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

FRONTEND_DEV_CMD="${WA_RUN_DEV_FRONTEND_DEV_CMD:-pnpm dev --port $FRONTEND_PORT}"

find_seqmeta_probe_identifier() {
  node - "$SEED_PATH" <<'NODE'
const fs = require("node:fs");

try {
  const seedPath = process.argv[2];
  const registrations = JSON.parse(fs.readFileSync(seedPath, "utf8"));

(function emitFirstSeqmetaIdentifier() {
  for (const registration of registrations) {
    const metadata = registration && typeof registration === "object" ? registration.metadata : null;
    if (!metadata || typeof metadata !== "object") {
      continue;
    }

    for (const [key, value] of Object.entries(metadata)) {
      if (!key.startsWith("seqmeta_")) {
        continue;
      }

      const identifier = String(value ?? "").trim();
      if (identifier !== "") {
        process.stdout.write(encodeURIComponent(identifier));
        return;
      }
    }
  }
})();
} catch {
  // Fall back to the broader health endpoint if the fixture file is unavailable.
}
NODE
}

default_seqmeta_health_url() {
  local probe_identifier=""

  probe_identifier="$(find_seqmeta_probe_identifier)"
  if [[ -n "$probe_identifier" ]]; then
    printf 'http://127.0.0.1:%s/validate/%s' "$SEQMETA_PORT" "$probe_identifier"
    return
  fi

  printf 'http://127.0.0.1:%s/studies' "$SEQMETA_PORT"
}

RESULTS_HEALTH_URL="${WA_RUN_DEV_RESULTS_HEALTH_URL:-http://127.0.0.1:$RESULTS_PORT/results/stats}"
FRONTEND_HEALTH_URL="${WA_RUN_DEV_FRONTEND_HEALTH_URL:-http://127.0.0.1:$FRONTEND_PORT/api/health}"
SEQMETA_HEALTH_URL="${WA_RUN_DEV_SEQMETA_HEALTH_URL:-$(default_seqmeta_health_url)}"

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
      if curl -fsS --max-time 2 -o /dev/null "$url" 2>/dev/null; then
        return 0
      fi
    fi

    attempt=$((attempt + 1))
    sleep 0.25
  done

  printf 'Timed out waiting for %s at %s\n' "$label" "$url" >&2
  return 1
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
  wait_for_http "seqmeta server" "$SEQMETA_HEALTH_URL" "strict"
else
  printf 'seqmeta server skipped because SAGA_API_TOKEN is unset\n' >"$SEQMETA_LOG"
fi

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
