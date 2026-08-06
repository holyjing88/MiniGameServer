#!/usr/bin/env bash
# MiniGameServer Linux one-click deploy.
#
# Default (Docker Compose): MySQL init + RankServer process.
#   sudo ./deployments/linux/deploy.sh
#
# Native binary + systemd (MySQL still via Compose):
#   sudo ./deployments/linux/deploy.sh --native
#
# Other:
#   ./deployments/linux/deploy.sh --status
#   ./deployments/linux/deploy.sh --down
#   ./deployments/linux/deploy.sh --logs
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/deployments/docker-compose.yml"
ENV_FILE="${SCRIPT_DIR}/.env"
ENV_EXAMPLE="${SCRIPT_DIR}/env.example"
MYSQL_ENV_FILE="${REPO_ROOT}/deployments/db/mysql.env"
SERVICE_UNIT="${REPO_ROOT}/release/cfg/minigameserver.service"

MODE="compose" # compose | native
ACTION="up"    # up | down | status | logs
REBUILD=0

log()  { printf '[deploy] %s\n' "$*"; }
err()  { printf '[deploy] ERROR: %s\n' "$*" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || err "missing command: $1"; }

usage() {
  cat <<'EOF'
Usage: deploy.sh [options]

Options:
  (default)     Start MySQL + RankServer via Docker Compose
  --native      MySQL via Compose; build binary and run under systemd
  --rebuild     Force image/binary rebuild
  --down        Stop and remove Compose stack (keeps MySQL volume)
  --status      Show service status
  --logs        Tail Compose / journal logs
  -h, --help    Show help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --native)  MODE="native"; shift ;;
    --rebuild) REBUILD=1; shift ;;
    --down)    ACTION="down"; shift ;;
    --status)  ACTION="status"; shift ;;
    --logs)    ACTION="logs"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) err "unknown option: $1 (see --help)" ;;
  esac
done

compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" "$@"
  elif command -v docker-compose >/dev/null 2>&1; then
    docker-compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" "$@"
  else
    err "docker compose not found; install Docker Engine + Compose plugin"
  fi
}

rand_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

sync_mysql_passwords() {
  # Overlay MySQL credentials from deployments/db/mysql.env into linux/.env
  [[ -f "${MYSQL_ENV_FILE}" ]] || return 0
  [[ -f "${ENV_FILE}" ]] || return 0
  local key val line tmp
  for key in MYSQL_ROOT_PASSWORD MYSQL_DATABASE MYSQL_PORT RANK_MYSQL_DSN; do
    val="$(grep -E "^${key}=" "${MYSQL_ENV_FILE}" | head -n1 | cut -d= -f2- || true)"
    [[ -n "${val}" ]] || continue
    # Strip surrounding quotes from mysql.env; re-quote when writing (DSN has () ? &).
    if [[ "${val}" =~ ^\"(.*)\"$ ]]; then
      val="${BASH_REMATCH[1]}"
    elif [[ "${val}" =~ ^\'(.*)\'$ ]]; then
      val="${BASH_REMATCH[1]}"
    fi
    val="${val//\\/\\\\}"
    val="${val//\"/\\\"}"
    line="${key}=\"${val}\""
    tmp="$(mktemp)"
    if grep -qE "^${key}=" "${ENV_FILE}"; then
      awk -v k="${key}" -v l="${line}" '
        index($0, k "=") == 1 { print l; next }
        { print }
      ' "${ENV_FILE}" >"${tmp}" && mv "${tmp}" "${ENV_FILE}"
    else
      printf '%s\n' "${line}" >>"${ENV_FILE}"
      rm -f "${tmp}"
    fi
  done
  log "synced MySQL credentials from ${MYSQL_ENV_FILE}"
}

ensure_env() {
  if [[ -f "${ENV_FILE}" ]]; then
    log "using existing env: ${ENV_FILE}"
    sync_mysql_passwords
    return
  fi
  [[ -f "${ENV_EXAMPLE}" ]] || err "missing ${ENV_EXAMPLE}"
  cp "${ENV_EXAMPLE}" "${ENV_FILE}"
  local sess svc
  sess="$(rand_secret)"
  svc="$(rand_secret)"
  # portable in-place edit
  if sed --version >/dev/null 2>&1; then
    sed -i "s/^RANK_SESSION_SECRET=.*/RANK_SESSION_SECRET=${sess}/" "${ENV_FILE}"
    sed -i "s/^RANK_SERVICE_TOKEN=.*/RANK_SERVICE_TOKEN=${svc}/" "${ENV_FILE}"
  else
    sed -i '' "s/^RANK_SESSION_SECRET=.*/RANK_SESSION_SECRET=${sess}/" "${ENV_FILE}"
    sed -i '' "s/^RANK_SERVICE_TOKEN=.*/RANK_SERVICE_TOKEN=${svc}/" "${ENV_FILE}"
  fi
  sync_mysql_passwords
  chmod 600 "${ENV_FILE}"
  log "created ${ENV_FILE} with generated secrets"
}

load_env() {
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
}

require_root_for_native() {
  if [[ "$(id -u)" -ne 0 ]]; then
    err "--native requires root (systemd install); re-run with sudo"
  fi
}

wait_http() {
  local url="$1" tries="${2:-60}"
  local i
  for ((i = 1; i <= tries; i++)); do
    if curl -sf "${url}" >/dev/null 2>&1; then
      log "healthy: ${url}"
      return 0
    fi
    sleep 1
  done
  err "timeout waiting for ${url}"
}

print_summary() {
  load_env
  local http_port="${HTTP_PORT:-8000}"
  local grpc_port="${GRPC_PORT:-8001}"
  cat <<EOF

========== MiniGameServer ready ==========
  HTTP : http://127.0.0.1:${http_port}
  gRPC : 127.0.0.1:${grpc_port}
  Health: http://127.0.0.1:${http_port}/healthz
  Env   : ${ENV_FILE}
  Mode  : ${MODE}

  MySQL schema: deployments/schema.sql
    - Compose first boot: /docker-entrypoint-initdb.d
    - App start also runs EnsureSchema (CREATE IF NOT EXISTS)

  Tips:
    ${SCRIPT_DIR}/deploy.sh --status
    ${SCRIPT_DIR}/deploy.sh --logs
    ${SCRIPT_DIR}/deploy.sh --down
==========================================
EOF
}

action_down() {
  need_cmd docker
  ensure_env
  log "stopping Compose stack..."
  compose down
  if systemctl list-unit-files 2>/dev/null | grep -q '^minigameserver.service'; then
    if systemctl is-active --quiet minigameserver 2>/dev/null; then
      log "stopping systemd minigameserver..."
      systemctl stop minigameserver || true
    fi
  fi
  log "done (MySQL data volume kept)"
}

action_status() {
  need_cmd docker
  ensure_env
  compose ps || true
  if systemctl list-unit-files 2>/dev/null | grep -q '^minigameserver.service'; then
    systemctl --no-pager status minigameserver || true
  fi
  local http_port
  http_port="$(grep -E '^HTTP_PORT=' "${ENV_FILE}" | cut -d= -f2- || true)"
  http_port="${http_port:-8000}"
  if curl -sf "http://127.0.0.1:${http_port}/healthz" >/dev/null 2>&1; then
    log "HTTP healthz: OK"
  else
    log "HTTP healthz: not ready"
  fi
}

action_logs() {
  need_cmd docker
  ensure_env
  if [[ "${MODE}" == "native" ]] && systemctl is-active --quiet minigameserver 2>/dev/null; then
    journalctl -u minigameserver -f
  else
    compose logs -f --tail=200
  fi
}

deploy_compose() {
  need_cmd docker
  need_cmd curl
  ensure_env
  load_env

  log "repo: ${REPO_ROOT}"
  log "starting MySQL + RankServer (Docker Compose)..."

  local build_args=()
  if [[ "${REBUILD}" -eq 1 ]]; then
    build_args+=(--build --force-recreate)
  else
    build_args+=(--build)
  fi

  compose up -d "${build_args[@]}"

  wait_http "http://127.0.0.1:${HTTP_PORT:-8000}/healthz" 90
  print_summary
}

ensure_go() {
  if command -v go >/dev/null 2>&1; then
    return
  fi
  err "Go toolchain not found (need go 1.22+). Install Go or use default Compose mode."
}

deploy_native() {
  require_root_for_native
  need_cmd docker
  need_cmd curl
  need_cmd systemctl
  ensure_go
  ensure_env
  load_env

  local install_dir="${INSTALL_DIR:-/opt/minigameserver}"
  local http_port="${HTTP_PORT:-8000}"

  log "starting MySQL only via Compose..."
  # Temporarily override: start mysql service only
  compose up -d mysql

  log "waiting for MySQL healthy..."
  local i
  for ((i = 1; i <= 60; i++)); do
    if compose ps mysql 2>/dev/null | grep -qi healthy; then
      break
    fi
    # fallback: mysqladmin via docker exec
    if docker exec mgs-mysql mysqladmin ping -h 127.0.0.1 -uroot -p"${MYSQL_ROOT_PASSWORD:-jgyjgyjgy}" --silent >/dev/null 2>&1; then
      break
    fi
    sleep 2
    if [[ "${i}" -eq 60 ]]; then
      err "MySQL not healthy"
    fi
  done
  log "MySQL is up (schema applied on first volume init; app EnsureSchema as backup)"

  # Do not run app container in native mode
  if docker ps -a --format '{{.Names}}' | grep -qx mgs-app; then
    log "stopping Compose app container (native mode uses systemd)..."
    docker stop mgs-app >/dev/null 2>&1 || true
    docker rm mgs-app >/dev/null 2>&1 || true
  fi

  log "building linux binary..."
  mkdir -p "${install_dir}"
  (
    cd "${REPO_ROOT}"
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
      -o "${install_dir}/minigameserver" ./cmd/minigameserver
  )

  if ! id -u minigameserver >/dev/null 2>&1; then
    useradd --system --home "${install_dir}" --shell /usr/sbin/nologin minigameserver
  fi

  # runtime env for systemd
  cat >"${install_dir}/minigameserver.env" <<EOF
RANK_HTTP_ADDR=${RANK_HTTP_ADDR:-:8000}
RANK_GRPC_ADDR=${RANK_GRPC_ADDR:-:8001}
RANK_STORE=mysql
RANK_MYSQL_DSN=${RANK_MYSQL_DSN:-${MYSQL_ROOT_USER:-root}:${MYSQL_ROOT_PASSWORD:-jgyjgyjgy}@tcp(127.0.0.1:${MYSQL_PORT:-3306})/${MYSQL_DATABASE:-minigameserver}?parseTime=true&loc=UTC&charset=utf8mb4}
RANK_SESSION_SECRET=${RANK_SESSION_SECRET}
RANK_SESSION_TTL_SEC=${RANK_SESSION_TTL_SEC:-86400}
RANK_SERVICE_TOKEN=${RANK_SERVICE_TOKEN}
RANK_TOPN_DEFAULT=${RANK_TOPN_DEFAULT:-100}
RANK_TOPN_MAX=${RANK_TOPN_MAX:-100}
RANK_REFRESH_SEC=${RANK_REFRESH_SEC:-30}
RANK_TZ=${RANK_TZ:-UTC}
RANK_AUTH_MODE=${RANK_AUTH_MODE:-tiktok}
RANK_TT_APP_ID=${RANK_TT_APP_ID:-7657847833046812688}
RANK_TT_CLIENT_KEY=${RANK_TT_CLIENT_KEY:-mg79au52hgl5ggpi}
RANK_TT_CLIENT_SECRET=${RANK_TT_CLIENT_SECRET:-QZAxJCXzGhbK2RWwt3IpXBHN90yNoKto}
RANK_DEFAULT_APP_ID=${RANK_DEFAULT_APP_ID:-parking_smart_brain}
EOF
  chmod 640 "${install_dir}/minigameserver.env"
  chown -R minigameserver:minigameserver "${install_dir}"

  install -m 644 "${SERVICE_UNIT}" /etc/systemd/system/minigameserver.service
  systemctl daemon-reload
  systemctl enable minigameserver
  systemctl restart minigameserver

  wait_http "http://127.0.0.1:${http_port}/healthz" 60
  print_summary
}

# --- main ---
case "${ACTION}" in
  down)   action_down; exit 0 ;;
  status) action_status; exit 0 ;;
  logs)   action_logs; exit 0 ;;
  up)     ;;
  *)      err "unknown action" ;;
esac

case "${MODE}" in
  compose) deploy_compose ;;
  native)  deploy_native ;;
  *)       err "unknown mode: ${MODE}" ;;
esac
