#!/usr/bin/env bash
# MiniGameServer MySQL initialization (standalone).
#
# Creates database and applies deployments/db/schema.sql.
# Uses a single MySQL account: root (no separate app user).
#
# Usage:
#   ./deployments/db/init-db.sh
#   ./deployments/db/init-db.sh --docker
#   ./deployments/db/init-db.sh --host 10.0.0.8 --root-password 'jgyjgyjgy'
#   ./deployments/db/init-db.sh --check
#   ./deployments/db/init-db.sh --dry-run
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENTS_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DB_ENV_FILE="${SCRIPT_DIR}/db.env"
DB_ENV_EXAMPLE="${SCRIPT_DIR}/db.env.example"
MYSQL_ENV_FILE="${SCRIPT_DIR}/mysql.env"
LINUX_ENV_FILE="${DEPLOYMENTS_DIR}/linux/.env"
SCHEMA_SQL=""

USE_DOCKER=0
DRY_RUN=0
CHECK_ONLY=0
REINIT=0
ENV_FILE_ARG=""

log()  { printf '[init-db] %s\n' "$*"; }
err()  { printf '[init-db] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: init-db.sh [options]

Options:
  --docker                Run SQL via docker exec into MySQL container
  --host HOST             MySQL host (default: 127.0.0.1)
  --port PORT             MySQL port (default: 3306)
  --root-user USER        MySQL user (default: root)
  --root-password PASS    MySQL password (default: jgyjgyjgy)
  --database NAME         App database (default: minigameserver)
  --container NAME        Docker container name (default: mgs-mysql)
  --env-file PATH         Load DB env file first (flags still override)
  --reinit                DROP DATABASE and recreate from schema.sql (destructive)
  --check                 Verify DB/tables exist; do not change anything
  --dry-run               Print SQL that would be applied, then exit
  -h, --help              Show help

Account: root only. Config: CLI > env vars > mysql.env / db.env > defaults

Schema: deployments/db/schema.sql only (no ALTER / dual path).

Stop the app before --reinit.
EOF
}

load_env_file() {
  local f="$1"
  [[ -f "${f}" ]] || return 0
  set -a
  # shellcheck disable=SC1090
  source "${f}"
  set +a
  log "loaded env: ${f}"
}

ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file)
      ENV_FILE_ARG="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      ARGS+=("$1")
      shift
      ;;
  esac
done
set -- "${ARGS[@]+"${ARGS[@]}"}"

if [[ -n "${ENV_FILE_ARG}" ]]; then
  [[ -f "${ENV_FILE_ARG}" ]] || err "env file not found: ${ENV_FILE_ARG}"
  load_env_file "${ENV_FILE_ARG}"
else
  if [[ -f "${MYSQL_ENV_FILE}" ]]; then
    load_env_file "${MYSQL_ENV_FILE}"
  fi
  if [[ -f "${DB_ENV_FILE}" ]]; then
    load_env_file "${DB_ENV_FILE}"
  elif [[ -f "${LINUX_ENV_FILE}" ]]; then
    load_env_file "${LINUX_ENV_FILE}"
  elif [[ ! -f "${MYSQL_ENV_FILE}" && -f "${DB_ENV_EXAMPLE}" ]]; then
    log "tip: copy ${DB_ENV_EXAMPLE} → ${DB_ENV_FILE} for persistent config"
  fi
fi

MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_ROOT_USER="${MYSQL_ROOT_USER:-root}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-jgyjgyjgy}"
MYSQL_DATABASE="${MYSQL_DATABASE:-minigameserver}"
MYSQL_DOCKER_CONTAINER="${MYSQL_DOCKER_CONTAINER:-mgs-mysql}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --docker)         USE_DOCKER=1; shift ;;
    --host)           MYSQL_HOST="$2"; shift 2 ;;
    --port)           MYSQL_PORT="$2"; shift 2 ;;
    --root-user)      MYSQL_ROOT_USER="$2"; shift 2 ;;
    --root-password)  MYSQL_ROOT_PASSWORD="$2"; shift 2 ;;
    --database)       MYSQL_DATABASE="$2"; shift 2 ;;
    --container)      MYSQL_DOCKER_CONTAINER="$2"; shift 2 ;;
    --check)          CHECK_ONLY=1; shift ;;
    --reinit)         REINIT=1; shift ;;
    --dry-run)        DRY_RUN=1; shift ;;
    --user|--password|--skip-user)
      err "app user options removed; use root only (--root-user / --root-password)"
      ;;
    *)                err "unknown option: $1 (see --help)" ;;
  esac
done

resolve_schema_sql() {
  local c="${SCRIPT_DIR}/schema.sql"
  [[ -f "${c}" ]] || return 1
  SCHEMA_SQL="${c}"
  return 0
}

resolve_schema_sql || err "schema not found: deployments/db/schema.sql"
log "schema: ${SCHEMA_SQL}"

sql_ident() {
  case "$1" in
    *\`*) err "invalid SQL identifier (contains backtick): $1" ;;
  esac
  printf '`%s`' "$1"
}

render_bootstrap() {
  local db_id
  db_id="$(sql_ident "${MYSQL_DATABASE}")"
  if [[ "${REINIT}" -eq 1 ]]; then
    cat <<EOF
DROP DATABASE IF EXISTS ${db_id};
CREATE DATABASE ${db_id}
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;
USE ${db_id};
EOF
  else
    cat <<EOF
CREATE DATABASE IF NOT EXISTS ${db_id}
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;
USE ${db_id};
EOF
  fi
}

mysql_admin() {
  export MYSQL_PWD="${MYSQL_ROOT_PASSWORD}"
  if [[ "${USE_DOCKER}" -eq 1 ]]; then
    command -v docker >/dev/null 2>&1 || err "docker not found"
    docker exec -i -e MYSQL_PWD="${MYSQL_ROOT_PASSWORD}" "${MYSQL_DOCKER_CONTAINER}" \
      mysql -u"${MYSQL_ROOT_USER}" --protocol=TCP "$@"
  else
    command -v mysql >/dev/null 2>&1 || err "mysql client not found (install mysql-client or use --docker)"
    mysql \
      -h"${MYSQL_HOST}" -P"${MYSQL_PORT}" \
      -u"${MYSQL_ROOT_USER}" \
      --protocol=TCP "$@"
  fi
}

EXPECTED_TABLES=(rank_score rank_board_meta player_register)

check_schema() {
  local missing=0 t cnt
  local db_esc="${MYSQL_DATABASE//\'/\'\'}"
  log "checking database ${MYSQL_DATABASE} ..."
  if ! mysql_admin -N -e "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='${db_esc}'" | grep -qx "${MYSQL_DATABASE}"; then
    err "database '${MYSQL_DATABASE}' does not exist"
  fi
  for t in "${EXPECTED_TABLES[@]}"; do
    local t_esc="${t//\'/\'\'}"
    cnt="$(mysql_admin -N -e "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='${db_esc}' AND TABLE_NAME='${t_esc}'" | tr -d '[:space:]')"
    if [[ "${cnt}" == "1" ]]; then
      log "  OK  table ${t}"
    else
      log "  MISSING table ${t}"
      missing=1
    fi
  done
  if [[ "${missing}" -ne 0 ]]; then
    err "schema check failed"
  fi
  log "schema check passed"
}

if [[ "${CHECK_ONLY}" -eq 1 ]]; then
  check_schema
  exit 0
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
FULL_SQL="${TMP_DIR}/init_all.sql"

{
  render_bootstrap
  echo
  echo "USE $(sql_ident "${MYSQL_DATABASE}");"
  echo
  cat "${SCHEMA_SQL}"
  echo
} >"${FULL_SQL}"

if [[ "${DRY_RUN}" -eq 1 ]]; then
  log "dry-run SQL:"
  cat "${FULL_SQL}"
  exit 0
fi

if [[ "${USE_DOCKER}" -eq 1 ]]; then
  log "target: docker container ${MYSQL_DOCKER_CONTAINER}"
  docker inspect "${MYSQL_DOCKER_CONTAINER}" >/dev/null 2>&1 \
    || err "container '${MYSQL_DOCKER_CONTAINER}' not found; start MySQL first"
else
  log "target: ${MYSQL_HOST}:${MYSQL_PORT} (user=${MYSQL_ROOT_USER})"
fi

log "creating database (if needed) and applying schema..."
if [[ "${REINIT}" -eq 1 ]]; then
  log "REINIT: DROP DATABASE ${MYSQL_DATABASE} then recreate (all data lost)"
fi
mysql_admin <"${FULL_SQL}"
log "SQL applied"

check_schema

cat <<EOF

========== DB init done ==========
  database : ${MYSQL_DATABASE}
  user     : ${MYSQL_ROOT_USER}
  tables   : ${EXPECTED_TABLES[*]}
  DSN tip  : ${MYSQL_ROOT_USER}:***@tcp(${MYSQL_HOST}:${MYSQL_PORT})/${MYSQL_DATABASE}?parseTime=true&loc=UTC&charset=utf8mb4
==================================
EOF
