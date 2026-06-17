#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
frontend_port=""
results_port=""
seqmeta_port=""
scenario="test"
fixtures_requested=0

PROD_REFUSED_INHERITED_ENV_VARS=(
  WA_TEST_FRONTEND_PORT
  WA_TEST_RESULTS_PORT
  WA_TEST_SEQMETA_PORT
  WA_TEST_RESULTS_HOST
  WA_DEV_FRONTEND_PORT
  WA_DEV_RESULTS_PORT
  WA_DEV_SEQMETA_PORT
  WA_DEV_RESULTS_HOST
)

print_prod_refused_env_help() {
  local indent="        "
  local max_width=78
  local line="$indent"
  local var

  for var in "${PROD_REFUSED_INHERITED_ENV_VARS[@]}"; do
    if [[ "$line" == "$indent" ]]; then
      line+="$var"
      continue
    fi

    if (( ${#line} + 2 + ${#var} > max_width )); then
      printf '%s,\n' "$line"
      line="$indent$var"
    else
      line+=", $var"
    fi
  done

  printf '%s.\n' "$line"
}

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
  -s, --seqmeta-port PORT   MLWH API port (legacy flag name; test default: 8091)

Scenario behaviour:
  test  Ephemeral mktemp DB under .tmp/ (deleted on shutdown). Fixtures are
        always seeded. Used by `make test` via Playwright.
  dev   Persistent DB at WA_RESULTS_DB_PATH (created if missing, never
        deleted). Refuses to start if WA_ENV=production. Pass --fixtures to
        also seed demo data.
  prod  Persistent DB at WA_RESULTS_DB_PATH. Requires WA_ENV=production.
        Refuses --fixtures. Refuses these inherited environment variables:
EOF
      print_prod_refused_env_help
      cat <<'EOF'

Remote CLI users should use the Results API URL/port from the output
(`Results` or `Results public`), not the frontend URL/port. The self-signed
dev TLS certificate is created or regenerated with SANs for loopback, this
machine's hostnames, and WA_RESULTS_SERVER_URL/WA_RESULTS_BACKEND_URL hosts.
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

has_nonblank_value() {
  local value="${1:-}"

  value="${value//[[:space:]]/}"
  [[ -n "$value" ]]
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

  for var in "${PROD_REFUSED_INHERITED_ENV_VARS[@]}"; do
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

  if ! has_nonblank_value "${WA_MLWH_SERVER_URL:-}" && \
     ! has_nonblank_value "${WA_MLWH_CACHE_PATH:-}" && \
     ! has_nonblank_value "${WA_MLWH_DSN:-}" && \
     ! has_nonblank_value "${WA_RUN_DEV_SEQMETA_CMD:-}"; then
    printf 'run-dev.sh: --mode prod requires an MLWH query source for results serve.\n' >&2
    printf 'Set WA_MLWH_SERVER_URL or WA_MLWH_CACHE_PATH, or configure WA_MLWH_DSN or WA_RUN_DEV_SEQMETA_CMD for an auto-managed local source.\n' >&2
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
validate_port "$seqmeta_port" "MLWH port"

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

trim_value() {
  local value="${1:-}"

  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

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
    local -a allowed_origins=()
    IFS=',' read -r -a allowed_origins <<< "$WA_DEV_ALLOWED_ORIGINS"
    for entry in "${allowed_origins[@]}"; do
      entry="$(trim_value "$entry")"
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
DEFAULT_DEV_TLS_CERT="$TMP_DIR/wa-dev-cert.pem"
DEFAULT_DEV_TLS_KEY="$TMP_DIR/wa-dev-key.pem"
RESULTS_LOG="$LOG_DIR/results.log"
SEQMETA_LOG="$LOG_DIR/mlwh.log"
FRONTEND_LOG="$LOG_DIR/frontend.log"

SEQMETA_CMD="${WA_RUN_DEV_SEQMETA_CMD:-}"
FRONTEND_HEALTH_MAX_ATTEMPTS="${WA_RUN_DEV_FRONTEND_HEALTH_MAX_ATTEMPTS:-120}"
SEQMETA_HEALTH_MAX_ATTEMPTS="${WA_RUN_DEV_SEQMETA_HEALTH_MAX_ATTEMPTS:-1200}"

RESULTS_HEALTH_URL="${WA_RUN_DEV_RESULTS_HEALTH_URL:-https://127.0.0.1:$results_port/rest/v1/results/stats}"
FRONTEND_HEALTH_URL="${WA_RUN_DEV_FRONTEND_HEALTH_URL:-https://127.0.0.1:$frontend_port/api/health}"
SEQMETA_HEALTH_URL="${WA_RUN_DEV_SEQMETA_HEALTH_URL:-http://127.0.0.1:$seqmeta_port/studies}"

results_bind_host_for_scenario() {
  local host=""

  case "$scenario" in
    dev) host="${WA_DEV_RESULTS_HOST:-}" ;;
    prod) host="${WA_PROD_RESULTS_HOST:-}" ;;
  esac

  host="$(trim_value "$host")"
  if [[ -z "$host" ]]; then
    host="127.0.0.1"
  fi

  printf '%s' "$host"
}

format_host_port() {
  local host="$1"
  local port="$2"

  if [[ "$host" == *:* && "$host" != \[* ]]; then
    printf '[%s]:%s' "$host" "$port"
  else
    printf '%s:%s' "$host" "$port"
  fi
}

results_bind_scope() {
  case "$1" in
    127.0.0.1|localhost|::1) printf 'loopback only' ;;
    *) printf 'listening beyond loopback' ;;
  esac
}

url_host_for_dev_tls_san() {
  local value
  local authority
  local host

  value="$(trim_value "${1:-}")"
  if [[ -z "$value" ]]; then
    return
  fi

  if [[ "$value" == *"://"* ]]; then
    value="${value#*://}"
  fi

  authority="${value%%[/?#]*}"
  authority="${authority##*@}"
  if [[ "$authority" == \[* ]]; then
    host="${authority#\[}"
    host="${host%%\]*}"
  else
    host="${authority%%:*}"
  fi

  printf '%s' "$host"
}

dev_tls_san_entry_for_host() {
  local host
  local lower_host
  local colon_markers
  local san_type="DNS"

  host="$(dev_tls_normalize_san_host_candidate "${1:-}")"
  lower_host="${host,,}"

  if [[ -z "$host" || "$lower_host" == "0.0.0.0" || "$lower_host" == "::" ]]; then
    return
  fi

  if [[ "$host" == *[[:space:],/?#]* || "$host" == *"*"* || "$host" == *"["* || "$host" == *"]"* ]]; then
    return
  fi

  if [[ "$host" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    san_type="IP"
  elif [[ "$host" == *:* ]]; then
    colon_markers="${host//[^:]}"
    if (( ${#colon_markers} < 2 )); then
      return
    fi
    san_type="IP"
  fi

  printf '%s:%s' "$san_type" "$host"
}

dev_tls_normalize_san_host_candidate() {
  local host
  local bracket_host
  local bracket_suffix

  host="$(trim_value "${1:-}")"
  host="${host%.}"

  if [[ "$host" == \[* ]]; then
    bracket_host="${host#\[}"
    if [[ "$bracket_host" != *"]"* ]]; then
      return
    fi

    bracket_suffix="${bracket_host#*\]}"
    bracket_host="${bracket_host%%\]*}"
    if [[ -n "$bracket_suffix" && ! "$bracket_suffix" =~ ^:[0-9]+$ ]]; then
      return
    fi

    host="$bracket_host"
  elif [[ "$host" =~ ^([^:]+):[0-9]+$ ]]; then
    host="${BASH_REMATCH[1]}"
  else
    host="${host#[}"
    host="${host%]}"
  fi

  host="${host%.}"
  printf '%s' "$host"
}

collect_dev_tls_san_entries() {
  local -a candidates=(localhost 127.0.0.1 ::1)
  local -A seen=()
  local fqdn
  local short
  local public_host
  local origin
  local entry

  if fqdn="$(hostname -f 2>/dev/null)" && [[ -n "$fqdn" ]]; then
    candidates+=("$fqdn")
  fi

  if short="$(hostname -s 2>/dev/null)" && [[ -n "$short" ]]; then
    candidates+=("$short")
  fi

  public_host="$(url_host_for_dev_tls_san "${WA_RESULTS_SERVER_URL:-}")"
  if [[ -n "$public_host" ]]; then
    candidates+=("$public_host")
  fi

  public_host="$(url_host_for_dev_tls_san "${WA_RESULTS_BACKEND_URL:-}")"
  if [[ -n "$public_host" ]]; then
    candidates+=("$public_host")
  fi

  candidates+=("$RESULTS_BIND_HOST")

  if [[ -n "${WA_DEV_ALLOWED_ORIGINS:-}" ]]; then
    local -a allowed_origins=()
    IFS=',' read -r -a allowed_origins <<< "$WA_DEV_ALLOWED_ORIGINS"
    for origin in "${allowed_origins[@]}"; do
      candidates+=("$origin")
    done
  fi

  for origin in "${candidates[@]}"; do
    entry="$(dev_tls_san_entry_for_host "$origin")"
    if [[ -n "$entry" && -z "${seen[$entry]:-}" ]]; then
      seen[$entry]=1
      printf '%s\n' "$entry"
    fi
  done
}

RESULTS_BIND_HOST="$(results_bind_host_for_scenario)"
RESULTS_BIND_ADDR="$(format_host_port "$RESULTS_BIND_HOST" "$results_port")"
RESULTS_BIND_SCOPE="$(results_bind_scope "$RESULTS_BIND_HOST")"

repo_absolute_path() {
  local value="$1"

  if [[ "$value" == /* ]]; then
    printf '%s' "$value"
  else
    printf '%s/%s' "$REPO_ROOT" "$value"
  fi
}

DEV_TLS_CERT="$(repo_absolute_path "${WA_RESULTS_SERVER_CERT:-$DEFAULT_DEV_TLS_CERT}")"
DEV_TLS_KEY="$(repo_absolute_path "${WA_RESULTS_SERVER_KEY:-$DEFAULT_DEV_TLS_KEY}")"
mapfile -t DEV_TLS_SAN_ENTRIES < <(collect_dev_tls_san_entries)

if [[ ! "$FRONTEND_HEALTH_MAX_ATTEMPTS" =~ ^[0-9]+$ ]]; then
  printf 'frontend health max attempts must be an integer\n' >&2
  exit 1
fi

if (( FRONTEND_HEALTH_MAX_ATTEMPTS < 1 )); then
  printf 'frontend health max attempts must be at least 1\n' >&2
  exit 1
fi

if [[ ! "$SEQMETA_HEALTH_MAX_ATTEMPTS" =~ ^[0-9]+$ ]]; then
  printf 'MLWH health max attempts must be an integer\n' >&2
  exit 1
fi

if (( SEQMETA_HEALTH_MAX_ATTEMPTS < 1 )); then
  printf 'MLWH health max attempts must be at least 1\n' >&2
  exit 1
fi

ensure_dev_tls_certificate() {
  if ! command -v openssl >/dev/null 2>&1; then
    printf 'run-dev.sh: openssl is required to inspect or create dev TLS certificates.\n' >&2
    exit 1
  fi

  if dev_tls_certificate_has_required_sans; then
    chmod 0600 "$DEV_TLS_KEY"
    chmod 0644 "$DEV_TLS_CERT"
    return
  fi

  if [[ -s "$DEV_TLS_CERT" && -s "$DEV_TLS_KEY" ]]; then
    printf 'Regenerating self-signed dev TLS certificate at %s with current hostnames\n' "$DEV_TLS_CERT"
  else
    printf 'Creating self-signed dev TLS certificate at %s\n' "$DEV_TLS_CERT"
  fi

  local openssl_config
  openssl_config="$(mktemp "$TMP_DIR/wa-dev-cert.XXXXXX.cnf")"
  write_dev_tls_openssl_config "$openssl_config"

  if ! openssl req \
    -x509 \
    -newkey rsa:2048 \
    -nodes \
    -days 365 \
    -keyout "$DEV_TLS_KEY" \
    -out "$DEV_TLS_CERT" \
    -config "$openssl_config" \
    >/dev/null 2>&1; then
    rm -f "$openssl_config"
    printf 'run-dev.sh: failed to create dev TLS certificate at %s.\n' "$DEV_TLS_CERT" >&2
    exit 1
  fi
  rm -f "$openssl_config"

  chmod 0600 "$DEV_TLS_KEY"
  chmod 0644 "$DEV_TLS_CERT"
}

dev_tls_certificate_has_required_sans() {
  local entry
  local san_type
  local san_value

  if [[ ! -s "$DEV_TLS_CERT" || ! -s "$DEV_TLS_KEY" ]]; then
    return 1
  fi

  for entry in "${DEV_TLS_SAN_ENTRIES[@]}"; do
    san_type="${entry%%:*}"
    san_value="${entry#*:}"

    case "$san_type" in
      DNS)
        openssl verify -CAfile "$DEV_TLS_CERT" -verify_hostname "$san_value" "$DEV_TLS_CERT" >/dev/null 2>&1 || return 1
        ;;
      IP)
        openssl verify -CAfile "$DEV_TLS_CERT" -verify_ip "$san_value" "$DEV_TLS_CERT" >/dev/null 2>&1 || return 1
        ;;
    esac
  done

  return 0
}

write_dev_tls_openssl_config() {
  local config_path="$1"
  local entry
  local san_type
  local san_value
  local dns_count=0
  local ip_count=0

  {
    printf '[req]\n'
    printf 'distinguished_name = req_distinguished_name\n'
    printf 'x509_extensions = v3_req\n'
    printf 'prompt = no\n'
    printf '[req_distinguished_name]\n'
    printf 'CN = localhost\n'
    printf '[v3_req]\n'
    printf 'subjectAltName = @alt_names\n'
    printf '[alt_names]\n'

    for entry in "${DEV_TLS_SAN_ENTRIES[@]}"; do
      san_type="${entry%%:*}"
      san_value="${entry#*:}"

      case "$san_type" in
        DNS)
          dns_count=$((dns_count + 1))
          printf 'DNS.%s = %s\n' "$dns_count" "$san_value"
          ;;
        IP)
          ip_count=$((ip_count + 1))
          printf 'IP.%s = %s\n' "$ip_count" "$san_value"
          ;;
      esac
    done
  } >"$config_path"

  chmod 0600 "$config_path"
}

dev_tls_san_display() {
  local joined=""
  local entry
  local san_value

  for entry in "${DEV_TLS_SAN_ENTRIES[@]}"; do
    san_value="${entry#*:}"
    if [[ -z "$joined" ]]; then
      joined="$san_value"
    else
      joined="$joined, $san_value"
    fi
  done

  printf '%s' "$joined"
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

tcp_port_is_open() {
  local port="$1"

  (exec 3<>"/dev/tcp/127.0.0.1/$port") >/dev/null 2>&1
}

ensure_port_available_or_reusable() {
  local label="$1"
  local flag_name="$2"
  local env_name="$3"
  local port="$4"
  local health_url="$5"
  local ca_cert="${6:-}"

  if ! tcp_port_is_open "$port"; then
    return
  fi

  if [[ "$scenario" == "dev" ]] && http_is_healthy "$health_url" "strict" "$ca_cert"; then
    return
  fi

  printf 'run-dev.sh: %s port %s is already in use on 127.0.0.1.\n' "$label" "$port" >&2
  printf 'Stop the process using that port, or choose another port with --%s-port or %s.\n' "$flag_name" "$env_name" >&2
  exit 1
}

preflight_service_ports() {
  ensure_port_available_or_reusable "results" "results" "WA_${scenario_env_prefix}_RESULTS_PORT" "$results_port" "$RESULTS_HEALTH_URL" "$WA_RESULTS_BACKEND_CA_CERT"

  if [[ -n "$SEQMETA_CMD" || -n "${WA_MLWH_DSN:-}" ]]; then
    ensure_port_available_or_reusable "MLWH" "seqmeta" "WA_${scenario_env_prefix}_SEQMETA_PORT" "$seqmeta_port" "$SEQMETA_HEALTH_URL"
  fi

  ensure_port_available_or_reusable "frontend" "frontend" "WA_${scenario_env_prefix}_FRONTEND_PORT" "$frontend_port" "$FRONTEND_HEALTH_URL" "$DEV_TLS_CERT"
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

seed_test_mlwh_cache() {
  local cache_path="$1"

  node --no-warnings - "$REPO_ROOT" "$cache_path" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
const { DatabaseSync } = require("node:sqlite");

const [repoRoot, cachePath] = process.argv.slice(2);
const db = new DatabaseSync(cachePath);
const schemaDir = path.join(repoRoot, "mlwh", "cache_schema", "sqlite");
const schemaNames = [
  "sample_mirror",
  "study_mirror",
  "library_samples",
  "donor_samples",
  "iseq_product_metrics_mirror",
  "seq_product_irods_locations_mirror",
  "sync_state",
  "schema_version",
  "sync_lock",
];
const syncedAt = "2026-05-15T10:00:00Z";

function run(statement, values) {
  db.prepare(statement).run(...values);
}

try {
  db.exec("BEGIN");

  for (const name of schemaNames) {
    db.exec(fs.readFileSync(path.join(schemaDir, `${name}.sql`), "utf8"));
  }

  db.exec("DELETE FROM schema_version");
  run("INSERT INTO schema_version(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", [3]);

  for (const tableName of ["sample", "study", "iseq_flowcell", "iseq_product_metrics", "seq_product_irods_locations"]) {
    run(
      "INSERT OR REPLACE INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)",
      [tableName, syncedAt, syncedAt],
    );
  }

  run(
    `INSERT INTO study_mirror(
      id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name,
      accession_number, study_title, faculty_sponsor, state,
      data_release_strategy, data_access_group, programme, reference_genome,
      ethically_approved, study_type, contains_human_dna,
      contaminated_human_dna, study_visibility, ega_dac_accession_number,
      ega_policy_accession_number, data_release_timing, last_updated
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    [
      1,
      "SQSCP",
      "6568",
      "11111111-2222-3333-4444-555555556568",
      "Study 6568",
      "EGAS00001006568",
      "Study title 6568",
      "Faculty sponsor 6568",
      "active",
      "managed",
      "public",
      "Human genetics",
      "GRCh38",
      1,
      "genomic sequencing",
      1,
      0,
      "visible",
      "",
      "",
      "standard",
      syncedAt,
    ],
  );

  run(
    `INSERT INTO sample_mirror(
      id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name,
      sanger_sample_id, supplier_name, accession_number, donor_id,
      taxon_id, common_name, description, last_updated
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    [
      1,
      "SQSCP",
      "10524782",
      "22222222-2222-3333-4444-555555524782",
      "WTSI_wEMB10524782",
      "WTSI_wEMB10524782",
      "WTSI_wEMB10524782",
      "ERS10524782",
      "DONOR10524782",
      9606,
      "human",
      "run-dev fixture sample",
      syncedAt,
    ],
  );

  run(
    "INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims, library_id, id_library_lims) VALUES (?, ?, ?, ?, ?)",
    ["Chromium single cell 3 prime v3", 1, "6568", "71046409", "SQPP-71046409-G:A1"],
  );

  db.exec("COMMIT");
} catch (error) {
  try {
    db.exec("ROLLBACK");
  } catch (_) {
    // Preserve the original failure.
  }
  throw error;
} finally {
  db.close();
}
NODE
}

case "$scenario" in
  test) scenario_env_prefix="TEST" ;;
  dev) scenario_env_prefix="DEV" ;;
  prod) scenario_env_prefix="PROD" ;;
esac

preflight_service_ports

printf 'Building Go binary at %s\n' "$BIN_PATH"
rm -f "$BIN_PATH"
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

  # When the MLWH server is auto-managed from an MLWH DSN, provide a reusable local
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

if [[ "$scenario" == "test" ]]; then
  seed_test_mlwh_cache "$MLWH_CACHE_PATH"
fi

if ! has_nonblank_value "${WA_MLWH_SERVER_URL:-}" && \
   ! has_nonblank_value "$MLWH_CACHE_PATH" && \
   has_nonblank_value "$SEQMETA_CMD"; then
  export WA_MLWH_SERVER_URL="http://127.0.0.1:$seqmeta_port"
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
unset WA_MLWH_BACKEND_URL

RESULTS_STARTED=0
SEQMETA_STARTED=0
FRONTEND_STARTED=0

: >"$RESULTS_LOG"
: >"$FRONTEND_LOG"

if [[ "$scenario" == "dev" ]] && http_is_healthy "$RESULTS_HEALTH_URL" "strict" "$WA_RESULTS_BACKEND_CA_CERT"; then
  printf 'Reusing existing results server on %s (configured bind=%s)\n' "$WA_RESULTS_BACKEND_URL" "$RESULTS_BIND_ADDR"
  printf 'Reusing existing results server on %s (configured bind=%s)\n' "$WA_RESULTS_BACKEND_URL" "$RESULTS_BIND_ADDR" >"$RESULTS_LOG"
else
  printf 'Starting results server on %s (mode=%s; bind=%s)\n' "$WA_RESULTS_BACKEND_URL" "$scenario" "$RESULTS_BIND_ADDR"
  results_serve_args=(results serve --port "$results_port")

  if [[ "$scenario" == "test" ]]; then
    results_serve_args+=(--mlwh-cache "$MLWH_CACHE_PATH")
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
  export WA_MLWH_BACKEND_URL="http://127.0.0.1:$seqmeta_port"
  : >"$SEQMETA_LOG"
  if [[ "$scenario" == "dev" ]] && http_is_healthy "$SEQMETA_HEALTH_URL" "strict"; then
    printf 'Reusing existing MLWH server on %s\n' "$WA_MLWH_BACKEND_URL"
    printf 'Reusing existing MLWH server on %s\n' "$WA_MLWH_BACKEND_URL" >"$SEQMETA_LOG"
  else
    printf 'Starting MLWH server on %s\n' "$WA_MLWH_BACKEND_URL"
    WA_RUN_DEV_SEQMETA_CMD="$SEQMETA_CMD" \
      bash -lc 'eval "exec $WA_RUN_DEV_SEQMETA_CMD"' \
      >>"$SEQMETA_LOG" 2>&1 &
    PIDS+=("$!")
    SEQMETA_STARTED=1
    printf 'Waiting for MLWH studies readiness at %s' "$SEQMETA_HEALTH_URL"
    if [[ -n "$MLWH_CACHE_PATH" ]]; then
      printf ' (MLWH cache: %s; a cold cache can take a while on first run)' "$MLWH_CACHE_PATH"
    fi
    printf '\n'
    wait_for_http "MLWH server" "$SEQMETA_HEALTH_URL" "strict" "$SEQMETA_HEALTH_MAX_ATTEMPTS" "$!" "$SEQMETA_LOG"
  fi
elif [[ -n "${WA_MLWH_DSN:-}" ]]; then
  export WA_MLWH_BACKEND_URL="http://127.0.0.1:$seqmeta_port"
  : >"$SEQMETA_LOG"
  if [[ "$scenario" == "dev" ]] && http_is_healthy "$SEQMETA_HEALTH_URL" "strict"; then
    printf 'Reusing existing MLWH server on %s\n' "$WA_MLWH_BACKEND_URL"
    printf 'Reusing existing MLWH server on %s\n' "$WA_MLWH_BACKEND_URL" >"$SEQMETA_LOG"
  else
    printf 'Starting MLWH server on %s\n' "$WA_MLWH_BACKEND_URL"
    mlwh_args=(mlwh serve --port "$seqmeta_port")
    "${BIN_PATH}" "${mlwh_args[@]}" >"$SEQMETA_LOG" 2>&1 &
    PIDS+=("$!")
    SEQMETA_STARTED=1
    printf 'Waiting for MLWH studies readiness at %s' "$SEQMETA_HEALTH_URL"
    if [[ -n "$MLWH_CACHE_PATH" ]]; then
      printf ' (MLWH cache: %s; a cold cache can take a while on first run)' "$MLWH_CACHE_PATH"
    fi
    printf '\n'
    wait_for_http "MLWH server" "$SEQMETA_HEALTH_URL" "strict" "$SEQMETA_HEALTH_MAX_ATTEMPTS" "$!" "$SEQMETA_LOG"
  fi
else
	printf 'MLWH server skipped because no explicit command or MLWH DSN is set\n' >"$SEQMETA_LOG"
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
printf 'Results bind: %s (%s)\n' "$RESULTS_BIND_ADDR" "$RESULTS_BIND_SCOPE"
printf 'Dev TLS cert: %s (valid for %s)\n' "$DEV_TLS_CERT" "$(dev_tls_san_display)"
if [[ -n "${WA_RESULTS_SERVER_URL:-}" ]]; then
  printf 'Results public: %s\n' "$WA_RESULTS_SERVER_URL"
elif [[ "$RESULTS_BIND_SCOPE" == "listening beyond loopback" ]]; then
  printf 'Results public: not configured (set WA_RESULTS_SERVER_URL to the reachable HTTPS URL for remote CLI users)\n'
fi
if [[ -n "${WA_MLWH_BACKEND_URL:-}" ]]; then
  printf 'MLWH: %s\n' "$WA_MLWH_BACKEND_URL"
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
