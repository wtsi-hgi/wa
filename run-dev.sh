#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
frontend_port=""
results_port=""
seqmeta_port=""
scenario="test"
fixtures_requested=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    -f|--frontend-port)
      frontend_port="${2:?missing value for $1}"
      shift 2
      ;;
    --frontend-port=*)
      frontend_port="${1#*=}"
      shift
      ;;
    -r|--results-port)
      results_port="${2:?missing value for $1}"
      shift 2
      ;;
    --results-port=*)
      results_port="${1#*=}"
      shift
      ;;
    -s|--seqmeta-port)
      seqmeta_port="${2:?missing value for $1}"
      shift 2
      ;;
    --seqmeta-port=*)
      seqmeta_port="${1#*=}"
      shift
      ;;
    -m|--mode)
      scenario="${2:?missing value for $1}"
      shift 2
      ;;
    --mode=*)
      scenario="${1#*=}"
      shift
      ;;
    --fixtures)
      fixtures_requested=1
      shift
      ;;
    -h|--help)
      cat <<'EOF'
Usage: ./run-dev.sh [options]

Options:
  -m, --mode SCENARIO       One of test (default), dev, prod
      --fixtures            Seed demo fixtures (only valid in --mode dev)
  -f, --frontend-port PORT  Frontend port (test default: 3000)
  -r, --results-port PORT   Results API port (test default: 8090)
  -s, --seqmeta-port PORT   Seqmeta API port (test default: 8091)

Scenario behaviour:
  test  Ephemeral mktemp DB under .tmp/ (deleted on shutdown). Fixtures are
        always seeded. Used by `make test` via Playwright.
  dev   Persistent DB at WA_RESULTS_DB_PATH (created if missing, never
        deleted). Refuses to start if WA_ENV=production. Pass --fixtures to
        also seed demo data.
  prod  Persistent DB at WA_RESULTS_DB_PATH. Requires WA_ENV=production.
        Refuses --fixtures. Refuses if any WA_TEST_*_PORT is set.
EOF
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      exit 1
      ;;
  esac
done

case "$scenario" in
  test|dev|prod) ;;
  *)
    printf 'run-dev.sh: unknown --mode %q (expected test, dev, or prod)\n' "$scenario" >&2
    exit 64
    ;;
esac

KNOWN_DEVELOPMENT_MLWH_DSN='mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse'
KNOWN_DEVELOPMENT_MLWH_PASSWORD='mlwh_humgen_is_secure'

mlwh_cache_path_resolves_under_repo_tmp() {
  local value="${1:-}"
  [[ "$value" == "$REPO_ROOT/.tmp/"* || "$value" == .tmp/* || "$value" == */.tmp/* ]]
}

mlwh_cache_path_looks_test() {
  local value="${1:-}"
  [[ "$value" == "/tmp" || "$value" == /tmp/* || "$value" == *"wa-test-mlwh"* ]]
}

mlwh_dsn_looks_nonprod() {
  local value="${1:-}"
  local lower_value="${value,,}"

  [[ -n "$value" && ( "$value" == "$KNOWN_DEVELOPMENT_MLWH_DSN" || "$lower_value" == *"localhost"* || "$lower_value" == *"127.0.0.1"* || "$lower_value" == *"_test"* ) ]]
}

# Scenario-specific guards. These run before any port allocation so a wrong
# environment can never spawn servers.
if [[ "$scenario" == "dev" && "${WA_ENV:-}" == "production" ]]; then
  printf 'run-dev.sh: refusing to run --mode dev with WA_ENV=production.\n' >&2
  exit 1
fi

if [[ "$scenario" == "test" && "${WA_ENV:-}" == "production" ]]; then
  printf 'run-dev.sh: refusing to run --mode test with WA_ENV=production.\n' >&2
  exit 1
fi

if [[ "$scenario" == "prod" ]]; then
  if [[ "${WA_ENV:-}" != "production" ]]; then
    printf 'run-dev.sh: --mode prod requires WA_ENV=production (got %q).\n' "${WA_ENV:-}" >&2
    exit 1
  fi

  if (( fixtures_requested )); then
    printf 'run-dev.sh: --fixtures is not permitted in --mode prod.\n' >&2
    exit 1
  fi

  for var in WA_TEST_FRONTEND_PORT WA_TEST_RESULTS_PORT WA_TEST_SEQMETA_PORT WA_DEV_FRONTEND_PORT WA_DEV_RESULTS_PORT WA_DEV_SEQMETA_PORT; do
    if [[ -n "${!var:-}" ]]; then
      printf 'run-dev.sh: refusing to run --mode prod with %s set.\n' "$var" >&2
      exit 1
    fi
  done
fi

if (( fixtures_requested )) && [[ "$scenario" != "dev" ]]; then
  printf 'run-dev.sh: --fixtures is only valid with --mode dev.\n' >&2
  exit 1
fi

if [[ "$scenario" == "test" && -n "${WA_RESULTS_DB_PATH:-}" ]]; then
  printf 'run-dev.sh: refusing to run --mode test with WA_RESULTS_DB_PATH set.\n' >&2
  printf 'Test mode always uses an ephemeral mktemp database.\n' >&2
  exit 1
fi

if [[ "$scenario" != "test" && -z "${WA_RESULTS_DB_PATH:-}" ]]; then
  printf 'run-dev.sh: --mode %s requires WA_RESULTS_DB_PATH to be set.\n' "$scenario" >&2
  exit 1
fi

if [[ "$scenario" == "dev" && -z "${WA_MLWH_DSN:-}" ]]; then
  printf 'run-dev.sh: --mode dev requires WA_MLWH_DSN to be set.\n' >&2
  exit 1
fi

if [[ "$scenario" == "test" ]]; then
  if [[ -n "${WA_MLWH_DSN:-}" ]]; then
    printf 'run-dev.sh: refusing to run --mode test with WA_MLWH_DSN set.\n' >&2
    exit 1
  fi

  if [[ -n "${WA_MLWH_PASSWORD:-}" ]]; then
    printf 'run-dev.sh: refusing to run --mode test with WA_MLWH_PASSWORD set.\n' >&2
    exit 1
  fi

  if [[ -n "${WA_MLWH_CACHE_PASSWORD:-}" ]]; then
    printf 'run-dev.sh: refusing to run --mode test with WA_MLWH_CACHE_PASSWORD set.\n' >&2
    exit 1
  fi

  if [[ -n "${WA_MLWH_CACHE_PATH:-}" ]] && ! mlwh_cache_path_resolves_under_repo_tmp "${WA_MLWH_CACHE_PATH}"; then
    printf 'run-dev.sh: refusing to run --mode test with WA_MLWH_CACHE_PATH set.\n' >&2
    printf 'Test mode always uses an ephemeral MLWH cache under .tmp/.\n' >&2
    exit 1
  fi
fi

if [[ "$scenario" == "prod" ]]; then
  if [[ -n "${WA_MLWH_CACHE_PATH:-}" ]] && mlwh_cache_path_looks_test "${WA_MLWH_CACHE_PATH}"; then
    printf 'run-dev.sh: refusing to run --mode prod with test-shaped WA_MLWH_CACHE_PATH.\n' >&2
    exit 1
  fi

  if mlwh_dsn_looks_nonprod "${WA_MLWH_DSN:-}"; then
    printf 'run-dev.sh: refusing to run --mode prod with development or test-shaped WA_MLWH_DSN.\n' >&2
    exit 1
  fi

  if [[ "${WA_MLWH_PASSWORD:-}" == "$KNOWN_DEVELOPMENT_MLWH_PASSWORD" ]]; then
    printf 'run-dev.sh: refusing to run --mode prod with development or test-shaped WA_MLWH_PASSWORD.\n' >&2
    exit 1
  fi

  if [[ "${WA_MLWH_CACHE_PASSWORD:-}" == "$KNOWN_DEVELOPMENT_MLWH_PASSWORD" ]]; then
    printf 'run-dev.sh: refusing to run --mode prod with development or test-shaped WA_MLWH_CACHE_PASSWORD.\n' >&2
    exit 1
  fi
fi

# Default ports per scenario, only used when no explicit flag was passed.
default_port_for() {
  local kind="$1"
  case "$scenario" in
    test)
      case "$kind" in
        frontend) printf '%s' "${WA_TEST_FRONTEND_PORT:-3000}" ;;
        results)  printf '%s' "${WA_TEST_RESULTS_PORT:-8090}" ;;
        seqmeta)  printf '%s' "${WA_TEST_SEQMETA_PORT:-8091}" ;;
      esac
      ;;
    dev)
      case "$kind" in
        frontend) printf '%s' "${WA_DEV_FRONTEND_PORT:?WA_DEV_FRONTEND_PORT required for --mode dev}" ;;
        results)  printf '%s' "${WA_DEV_RESULTS_PORT:?WA_DEV_RESULTS_PORT required for --mode dev}" ;;
        seqmeta)  printf '%s' "${WA_DEV_SEQMETA_PORT:?WA_DEV_SEQMETA_PORT required for --mode dev}" ;;
      esac
      ;;
    prod)
      case "$kind" in
        frontend) printf '%s' "${WA_PROD_FRONTEND_PORT:?WA_PROD_FRONTEND_PORT required for --mode prod}" ;;
        results)  printf '%s' "${WA_PROD_RESULTS_PORT:?WA_PROD_RESULTS_PORT required for --mode prod}" ;;
        seqmeta)  printf '%s' "${WA_PROD_SEQMETA_PORT:?WA_PROD_SEQMETA_PORT required for --mode prod}" ;;
      esac
      ;;
  esac
}

frontend_port="${frontend_port:-$(default_port_for frontend)}"
results_port="${results_port:-$(default_port_for results)}"
seqmeta_port="${seqmeta_port:-$(default_port_for seqmeta)}"

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

validate_port "$frontend_port" "frontend port"
validate_port "$results_port" "results port"
validate_port "$seqmeta_port" "seqmeta port"

# Always export WA_ENV so child processes (Next.js, Go binaries, tests) can
# branch on the active scenario. The wrapper sets it to the canonical value
# matching this run.
case "$scenario" in
  test) export WA_ENV="test" ;;
  dev)  export WA_ENV="development" ;;
  prod) export WA_ENV="production" ;;
esac

# WA_TEST_*_PORT is only exported in test mode so dev/prod children cannot
# accidentally pick up the test-scoped vars.
if [[ "$scenario" == "test" ]]; then
  export WA_TEST_FRONTEND_PORT="$frontend_port"
  export WA_TEST_RESULTS_PORT="$results_port"
  export WA_TEST_SEQMETA_PORT="$seqmeta_port"
else
  unset WA_TEST_FRONTEND_PORT WA_TEST_RESULTS_PORT WA_TEST_SEQMETA_PORT
fi

# Assemble the Next.js dev server's allowedDevOrigins so developers hitting the
# dev server from a non-localhost host (e.g. a remote workstation or laptop
# over SSH forwarding) are not blocked from fetching /_next/* chunks, HMR, or
# RSC payloads. Without this, React never hydrates and onClick handlers are
# dead. Defaults cover loopback plus the current `hostname -f`/`hostname -s`;
# any caller-provided WA_DEV_ALLOWED_ORIGINS entries (comma separated) are
# appended.
collect_dev_origins() {
  local -a origins=(localhost 127.0.0.1)
  local fqdn
  local short

  if fqdn="$(hostname -f 2>/dev/null)" && [[ -n "$fqdn" ]]; then
    origins+=("$fqdn")
  fi

  if short="$(hostname -s 2>/dev/null)" && [[ -n "$short" ]]; then
    origins+=("$short")
  fi

  if [[ -n "${WA_DEV_ALLOWED_ORIGINS:-}" ]]; then
    local entry
    local IFS=','
    for entry in $WA_DEV_ALLOWED_ORIGINS; do
      entry="${entry#"${entry%%[![:space:]]*}"}"
      entry="${entry%"${entry##*[![:space:]]}"}"
      if [[ -n "$entry" ]]; then
        origins+=("$entry")
      fi
    done
  fi

  local -A seen=()
  local -a unique=()
  local value
  for value in "${origins[@]}"; do
    if [[ -z "${seen[$value]:-}" ]]; then
      seen[$value]=1
      unique+=("$value")
    fi
  done

  local IFS=','
  printf '%s' "${unique[*]}"
}

WA_DEV_ALLOWED_ORIGINS="$(collect_dev_origins)"
export WA_DEV_ALLOWED_ORIGINS

TMP_DIR="$REPO_ROOT/.tmp"
BIN_PATH="$TMP_DIR/wa"
LOG_DIR="$REPO_ROOT/logs"
SEED_PATH="$REPO_ROOT/.docs/results-web/fixtures/seed.json"
FRONTEND_DIR="${WA_RUN_DEV_FRONTEND_CWD:-$REPO_ROOT/frontend}"
DEV_TLS_CERT="$TMP_DIR/wa-dev-cert.pem"
DEV_TLS_KEY="$TMP_DIR/wa-dev-key.pem"
RESULTS_LOG="$LOG_DIR/results.log"
SEQMETA_LOG="$LOG_DIR/seqmeta.log"
FRONTEND_LOG="$LOG_DIR/frontend.log"

SEQMETA_CMD="${WA_RUN_DEV_SEQMETA_CMD:-}"
FRONTEND_HEALTH_MAX_ATTEMPTS="${WA_RUN_DEV_FRONTEND_HEALTH_MAX_ATTEMPTS:-120}"
SEQMETA_HEALTH_MAX_ATTEMPTS="${WA_RUN_DEV_SEQMETA_HEALTH_MAX_ATTEMPTS:-1200}"

RESULTS_HEALTH_URL="${WA_RUN_DEV_RESULTS_HEALTH_URL:-https://127.0.0.1:$results_port/rest/v1/results/stats}"
FRONTEND_HEALTH_URL="${WA_RUN_DEV_FRONTEND_HEALTH_URL:-https://127.0.0.1:$frontend_port/api/health}"
SEQMETA_HEALTH_URL="${WA_RUN_DEV_SEQMETA_HEALTH_URL:-http://127.0.0.1:$seqmeta_port/studies}"

if [[ ! "$FRONTEND_HEALTH_MAX_ATTEMPTS" =~ ^[0-9]+$ ]]; then
  printf 'frontend health max attempts must be an integer\n' >&2
  exit 1
fi

if (( FRONTEND_HEALTH_MAX_ATTEMPTS < 1 )); then
  printf 'frontend health max attempts must be at least 1\n' >&2
  exit 1
fi

if [[ ! "$SEQMETA_HEALTH_MAX_ATTEMPTS" =~ ^[0-9]+$ ]]; then
  printf 'seqmeta health max attempts must be an integer\n' >&2
  exit 1
fi

if (( SEQMETA_HEALTH_MAX_ATTEMPTS < 1 )); then
  printf 'seqmeta health max attempts must be at least 1\n' >&2
  exit 1
fi

ensure_dev_tls_certificate() {
  if [[ -s "$DEV_TLS_CERT" && -s "$DEV_TLS_KEY" ]]; then
    chmod 0600 "$DEV_TLS_KEY"
    chmod 0644 "$DEV_TLS_CERT"

    return
  fi

  if ! command -v openssl >/dev/null 2>&1; then
    printf 'run-dev.sh: openssl is required to create dev TLS certificates.\n' >&2
    exit 1
  fi

  printf 'Creating self-signed dev TLS certificate at %s\n' "$DEV_TLS_CERT"
  openssl req \
    -x509 \
    -newkey rsa:2048 \
    -nodes \
    -days 365 \
    -keyout "$DEV_TLS_KEY" \
    -out "$DEV_TLS_CERT" \
    -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    >/dev/null 2>&1

  chmod 0600 "$DEV_TLS_KEY"
  chmod 0644 "$DEV_TLS_CERT"
}

cd "$REPO_ROOT"

mkdir -p "$TMP_DIR" "$LOG_DIR"
ensure_dev_tls_certificate

export WA_RESULTS_BACKEND_CA_CERT="$DEV_TLS_CERT"
export WA_RESULTS_SERVER_CERT="$DEV_TLS_CERT"
export WA_RESULTS_SERVER_KEY="$DEV_TLS_KEY"

if [[ -n "${WA_RUN_DEV_FRONTEND_DEV_CMD:-}" ]]; then
  FRONTEND_DEV_CMD="$WA_RUN_DEV_FRONTEND_DEV_CMD"
else
  FRONTEND_DEV_CMD="pnpm dev --port $frontend_port --experimental-https --experimental-https-key $DEV_TLS_KEY --experimental-https-cert $DEV_TLS_CERT"
fi

if [[ "$scenario" == "test" ]]; then
  export XDG_STATE_HOME="$TMP_DIR/state-test"
  mkdir -p "$XDG_STATE_HOME"
fi

PIDS=()
DB_PATH=""
DB_EPHEMERAL=0
MLWH_CACHE_PATH=""
MLWH_CACHE_EPHEMERAL=0
CLEANED_UP=0

process_is_running() {
  local pid="$1"
  local stat

  if ! kill -0 "$pid" 2>/dev/null; then
    return 1
  fi

  stat="$(ps -p "$pid" -o stat= 2>/dev/null || true)"
  if [[ -z "$stat" || "$stat" == Z* ]]; then
    return 1
  fi

  return 0
}

wait_for_process_exit() {
  local pid="$1"
  local max_attempts="${2:-20}"
  local attempt=0

  while (( attempt < max_attempts )); do
    if ! process_is_running "$pid"; then
      return 0
    fi

    attempt=$((attempt + 1))
    sleep 0.1
  done

  return 1
}

terminate_child_process() {
  local pid="$1"

  if ! process_is_running "$pid"; then
    wait "$pid" 2>/dev/null || true
    return
  fi

  kill "$pid" 2>/dev/null || true
  if ! wait_for_process_exit "$pid"; then
    printf 'run-dev.sh: process %s did not exit after SIGTERM; sending SIGKILL.\n' "$pid" >&2
    kill -KILL "$pid" 2>/dev/null || true
  fi

  wait "$pid" 2>/dev/null || true
}

cleanup() {
  local exit_code="$1"

  if [[ "$CLEANED_UP" -eq 1 ]]; then
    return
  fi

  CLEANED_UP=1
  trap - EXIT INT TERM

  for pid in "${PIDS[@]:-}"; do
    terminate_child_process "$pid"
  done

  if (( DB_EPHEMERAL )) && [[ -n "$DB_PATH" ]]; then
    rm -f "$DB_PATH"
  fi

  if (( MLWH_CACHE_EPHEMERAL )) && [[ -n "$MLWH_CACHE_PATH" ]]; then
    rm -f "$MLWH_CACHE_PATH"
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

print_service_log_tail() {
  local log_path="$1"

  if [[ -z "$log_path" ]]; then
    return
  fi

  if [[ ! -e "$log_path" ]]; then
    printf 'Log file %s does not exist yet.\n' "$log_path" >&2

    return
  fi

  if [[ ! -s "$log_path" ]]; then
    printf 'Log file %s is empty.\n' "$log_path" >&2

    return
  fi

  printf 'Last 40 lines from %s:\n' "$log_path" >&2
  tail -n 40 "$log_path" >&2 || true
}

print_service_wait_diagnostics() {
  local label="$1"
  local pid="$2"
  local log_path="$3"

  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    printf '%s process status:\n' "$label" >&2
    ps -p "$pid" -o pid=,etime=,stat=,pcpu=,pmem=,cmd= >&2 || true
  fi

  print_service_log_tail "$log_path"
}

curl_probe() {
  local url="$1"
  local ca_cert="${2:-}"
  local -a args=(-fsS --max-time 2 -o /dev/null)

  if [[ "$url" == https://* && -n "$ca_cert" ]]; then
    args+=(--cacert "$ca_cert")
  fi

  curl "${args[@]}" "$url" >/dev/null 2>&1
}

wait_for_http() {
  local label="$1"
  local url="$2"
  local mode="$3"
  local max_attempts="${4:-120}"
  local pid="${5:-}"
  local log_path="${6:-}"
  local ca_cert="${7:-}"
  local attempt=0
  local exit_status

  while (( attempt < max_attempts )); do
    if curl_probe "$url" "$ca_cert"; then
      return 0
    fi

    if [[ -n "$pid" ]] && ! kill -0 "$pid" 2>/dev/null; then
      if wait "$pid"; then
        exit_status=0
      else
        exit_status=$?
      fi

      printf '%s exited before becoming ready at %s (exit=%s).\n' "$label" "$url" "$exit_status" >&2
      print_service_wait_diagnostics "$label" "$pid" "$log_path"

      return 1
    fi

    attempt=$((attempt + 1))
    sleep 0.25
  done

  printf 'Timed out waiting for %s at %s\n' "$label" "$url" >&2
  print_service_wait_diagnostics "$label" "$pid" "$log_path"

  return 1
}

http_is_healthy() {
  local url="$1"
  local mode="${2:-strict}"
  local ca_cert="${3:-}"

  curl_probe "$url" "$ca_cert"
}

seed_results() {
  NODE_EXTRA_CA_CERTS="$WA_RESULTS_BACKEND_CA_CERT" node - "$REPO_ROOT" "$SEED_PATH" "$WA_RESULTS_BACKEND_URL" <<'NODE'
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

async function ownerJWT(resultsBaseUrl) {
  const tokenDir = process.env.XDG_STATE_HOME || os.homedir();
  const tokenPath = path.join(tokenDir, ".wa-results-server.token");
  const token = fs.readFileSync(tokenPath, "utf8").trim();
  const form = new URLSearchParams({
    username: os.userInfo().username,
    password: token,
  });
  const response = await fetch(new URL("/rest/v1/jwt", resultsBaseUrl), {
    method: "POST",
    headers: {
      "content-type": "application/x-www-form-urlencoded",
    },
    body: form,
  });

  if (!response.ok) {
    throw new Error(`owner login failed with ${response.status}: ${await response.text()}`);
  }

  return response.json();
}

async function main() {
  const [repoRoot, seedPath, resultsBaseUrl] = process.argv.slice(2);
  const registrations = JSON.parse(fs.readFileSync(seedPath, "utf8"));
  const jwt = await ownerJWT(resultsBaseUrl);

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

    const response = await fetch(new URL("/rest/v1/auth/results", resultsBaseUrl), {
      method: "POST",
      headers: {
        authorization: `Bearer ${jwt}`,
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

# Choose the database path per scenario: test gets a throwaway file under
# .tmp/ that is removed on shutdown; dev/prod use the persistent path the
# operator configured in their env file.
if [[ "$scenario" == "test" ]]; then
  DB_PATH="$(mktemp "$TMP_DIR/results-dev.XXXXXX.sqlite")"
  DB_EPHEMERAL=1
else
  DB_PATH="$WA_RESULTS_DB_PATH"
  DB_EPHEMERAL=0
  # Only create the parent directory for SQLite-style paths. MySQL DSNs
  # contain `@tcp(` or `@unix(` and must not be touched as filesystem paths.
  if [[ "$DB_PATH" != *"@tcp("* && "$DB_PATH" != *"@unix("* ]]; then
    db_dir="$(dirname "$DB_PATH")"
    if [[ -n "$db_dir" && "$db_dir" != "." ]]; then
      mkdir -p "$db_dir"
    fi
  fi
fi
export WA_RESULTS_DB_PATH="$DB_PATH"

if [[ "$scenario" == "test" ]]; then
  MLWH_CACHE_PATH="$(mktemp "$TMP_DIR/mlwh-test.XXXXXX.sqlite")"
  MLWH_CACHE_EPHEMERAL=1
else
  MLWH_CACHE_PATH="${WA_MLWH_CACHE_PATH:-}"
  MLWH_CACHE_EPHEMERAL=0

  # When seqmeta is auto-managed from an MLWH DSN, provide a reusable local
  # cache path if the operator did not configure one explicitly.
  if [[ -z "$MLWH_CACHE_PATH" && -n "${WA_MLWH_DSN:-}" ]]; then
    MLWH_CACHE_PATH="$TMP_DIR/mlwh-$scenario.sqlite"
  fi

  if [[ -n "$MLWH_CACHE_PATH" && "$MLWH_CACHE_PATH" != *"@tcp("* && "$MLWH_CACHE_PATH" != *"@unix("* ]]; then
    mlwh_cache_dir="$(dirname "$MLWH_CACHE_PATH")"
    if [[ -n "$mlwh_cache_dir" && "$mlwh_cache_dir" != "." ]]; then
      mkdir -p "$mlwh_cache_dir"
    fi
  fi
fi

if [[ -n "$MLWH_CACHE_PATH" ]]; then
  if [[ "$scenario" != "test" ]]; then
    export WA_MLWH_CACHE_PATH="$MLWH_CACHE_PATH"
  else
    unset WA_MLWH_CACHE_PATH
  fi
else
  unset WA_MLWH_CACHE_PATH
fi

export WA_RESULTS_BACKEND_URL="https://127.0.0.1:$results_port"
unset WA_SEQMETA_BACKEND_URL

RESULTS_STARTED=0
SEQMETA_STARTED=0
FRONTEND_STARTED=0

: >"$RESULTS_LOG"
: >"$FRONTEND_LOG"

if [[ "$scenario" == "dev" ]] && http_is_healthy "$RESULTS_HEALTH_URL" "strict" "$WA_RESULTS_BACKEND_CA_CERT"; then
  printf 'Reusing existing results server on %s\n' "$WA_RESULTS_BACKEND_URL"
  printf 'Reusing existing results server on %s\n' "$WA_RESULTS_BACKEND_URL" >"$RESULTS_LOG"
else
  printf 'Starting results server on %s (mode=%s)\n' "$WA_RESULTS_BACKEND_URL" "$scenario"
  results_serve_args=(results serve --port "$results_port" --cert "$DEV_TLS_CERT" --key "$DEV_TLS_KEY")

  if [[ "$scenario" == "test" ]]; then
    results_serve_args+=(--ldap_server "${WA_RESULTS_LDAP_SERVER:-wa-test-ldap.invalid}")
    results_serve_args+=(--ldap_dn "${WA_RESULTS_LDAP_DN:-uid=%s,ou=people,dc=example,dc=org}")
  else
    if [[ -n "${WA_RESULTS_LDAP_SERVER:-}" ]]; then
      results_serve_args+=(--ldap_server "$WA_RESULTS_LDAP_SERVER")
    fi

    if [[ -n "${WA_RESULTS_LDAP_DN:-}" ]]; then
      results_serve_args+=(--ldap_dn "$WA_RESULTS_LDAP_DN")
    fi
  fi

  "$BIN_PATH" "${results_serve_args[@]}" >"$RESULTS_LOG" 2>&1 &
  PIDS+=("$!")
  RESULTS_STARTED=1

  wait_for_http "results server" "$RESULTS_HEALTH_URL" "strict" 120 "$!" "$RESULTS_LOG" "$WA_RESULTS_BACKEND_CA_CERT"
fi

seed_fixtures=0
case "$scenario" in
  test) seed_fixtures=1 ;;
  dev)  seed_fixtures=$fixtures_requested ;;
  prod) seed_fixtures=0 ;;
esac

if (( seed_fixtures )); then
  printf 'Seeding fixtures from %s\n' "$SEED_PATH"
  seed_results >>"$RESULTS_LOG" 2>&1
else
  printf 'Skipping fixture seed (mode=%s, --fixtures not requested)\n' "$scenario"
fi

if [[ -n "$SEQMETA_CMD" ]]; then
  export WA_SEQMETA_BACKEND_URL="http://127.0.0.1:$seqmeta_port"
  : >"$SEQMETA_LOG"
  if [[ "$scenario" == "dev" ]] && http_is_healthy "$SEQMETA_HEALTH_URL" "strict"; then
    printf 'Reusing existing seqmeta server on %s\n' "$WA_SEQMETA_BACKEND_URL"
    printf 'Reusing existing seqmeta server on %s\n' "$WA_SEQMETA_BACKEND_URL" >"$SEQMETA_LOG"
  else
    printf 'Starting seqmeta server on %s\n' "$WA_SEQMETA_BACKEND_URL"
    WA_RUN_DEV_SEQMETA_CMD="$SEQMETA_CMD" \
      bash -lc 'eval "exec $WA_RUN_DEV_SEQMETA_CMD"' \
      >>"$SEQMETA_LOG" 2>&1 &
    PIDS+=("$!")
    SEQMETA_STARTED=1
    printf 'Waiting for seqmeta studies readiness at %s' "$SEQMETA_HEALTH_URL"
    if [[ -n "$MLWH_CACHE_PATH" ]]; then
      printf ' (MLWH cache: %s; a cold cache can take a while on first run)' "$MLWH_CACHE_PATH"
    fi
    printf '\n'
    wait_for_http "seqmeta server" "$SEQMETA_HEALTH_URL" "strict" "$SEQMETA_HEALTH_MAX_ATTEMPTS" "$!" "$SEQMETA_LOG"
  fi
elif [[ -n "${WA_MLWH_DSN:-}" ]]; then
  export WA_SEQMETA_BACKEND_URL="http://127.0.0.1:$seqmeta_port"
  : >"$SEQMETA_LOG"
  if [[ "$scenario" == "dev" ]] && http_is_healthy "$SEQMETA_HEALTH_URL" "strict"; then
    printf 'Reusing existing seqmeta server on %s\n' "$WA_SEQMETA_BACKEND_URL"
    printf 'Reusing existing seqmeta server on %s\n' "$WA_SEQMETA_BACKEND_URL" >"$SEQMETA_LOG"
  else
    printf 'Starting seqmeta server on %s\n' "$WA_SEQMETA_BACKEND_URL"
    seqmeta_args=(seqmeta serve --port "$seqmeta_port")
    "${BIN_PATH}" "${seqmeta_args[@]}" >"$SEQMETA_LOG" 2>&1 &
    PIDS+=("$!")
    SEQMETA_STARTED=1
    printf 'Waiting for seqmeta studies readiness at %s' "$SEQMETA_HEALTH_URL"
    if [[ -n "$MLWH_CACHE_PATH" ]]; then
      printf ' (MLWH cache: %s; a cold cache can take a while on first run)' "$MLWH_CACHE_PATH"
    fi
    printf '\n'
    wait_for_http "seqmeta server" "$SEQMETA_HEALTH_URL" "strict" "$SEQMETA_HEALTH_MAX_ATTEMPTS" "$!" "$SEQMETA_LOG"
  fi
else
	printf 'seqmeta server skipped because no explicit command or MLWH DSN is set\n' >"$SEQMETA_LOG"
fi

printf '\n[dev]\n' >>"$FRONTEND_LOG"
if [[ -n "$MLWH_CACHE_PATH" ]]; then
  export WA_MLWH_CACHE_PATH="$MLWH_CACHE_PATH"
fi
if [[ "$scenario" == "dev" ]] && http_is_healthy "$FRONTEND_HEALTH_URL" "strict" "$DEV_TLS_CERT"; then
  printf 'Reusing existing frontend dev server on https://127.0.0.1:%s\n' "$frontend_port"
  printf 'Reusing existing frontend dev server on https://127.0.0.1:%s\n' "$frontend_port" >>"$FRONTEND_LOG"
else
  printf 'Starting frontend dev server on https://127.0.0.1:%s\n' "$frontend_port"
  WA_RUN_DEV_FRONTEND_DEV_CMD="$FRONTEND_DEV_CMD" \
    bash -lc 'cd "$1" && eval "exec $WA_RUN_DEV_FRONTEND_DEV_CMD"' -- "$FRONTEND_DIR" \
    >>"$FRONTEND_LOG" 2>&1 &
  PIDS+=("$!")
  FRONTEND_PID="$!"
  FRONTEND_STARTED=1

  wait_for_http "frontend health" "$FRONTEND_HEALTH_URL" "strict" "$FRONTEND_HEALTH_MAX_ATTEMPTS" "$FRONTEND_PID" "$FRONTEND_LOG" "$DEV_TLS_CERT"
fi

printf 'Development environment is ready.\n'
printf 'Results: %s\n' "$WA_RESULTS_BACKEND_URL"
if [[ -n "${WA_SEQMETA_BACKEND_URL:-}" ]]; then
  printf 'Seqmeta: %s\n' "$WA_SEQMETA_BACKEND_URL"
fi
printf 'Frontend: https://127.0.0.1:%s\n' "$frontend_port"
printf 'Logs: %s\n' "$LOG_DIR"

if (( ${#PIDS[@]} > 0 )); then
  wait "${PIDS[@]}"
else
  while true; do
    read -r -t 3600 _ || true
  done
fi
