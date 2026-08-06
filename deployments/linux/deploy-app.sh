#!/usr/bin/env bash
# Deploy MiniGameServer business process only (no Docker).
#
# Target : /app/minigamesvr
# - directory missing  閳?create
# - directory has files 閳?backup first, then deploy
#
# Usage (on Linux server, usually as root):
#   sudo ./deployments/linux/deploy-app.sh
#   sudo ./deployments/linux/deploy-app.sh --restart
#   sudo ./deployments/linux/deploy-app.sh --status
#   sudo ./deployments/linux/deploy-app.sh --stop
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MYSQL_ENV_FILE="${REPO_ROOT}/deployments/db/mysql.env"
TIKTOK_ENV_FILE="${REPO_ROOT}/deployments/db/tiktok.env"
SERVICE_UNIT_SRC="${REPO_ROOT}/release/cfg/minigamesvr.service"
APP_SCRIPTS_DIR="${REPO_ROOT}/release/bin"

INSTALL_DIR="${INSTALL_DIR:-/app/minigamesvr}"
BACKUP_ROOT="${BACKUP_ROOT:-/app}"
BINARY_NAME="minigamesvr"
ENV_NAME="minigamesvr.env"
ENV_EXAMPLE="${REPO_ROOT}/release/cfg/minigamesvr.env.example"
SERVICE_NAME="minigamesvr"
ACTION="deploy" # deploy | restart | status | stop | logs

log()  { printf '[deploy-app] %s\n' "$*"; }
err()  { printf '[deploy-app] ERROR: %s\n' "$*" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || err "missing command: $1"; }

usage() {
  cat <<'EOF'
Usage: deploy-app.sh [options]

  (default)   Build binary and deploy to /app/minigamesvr (backup if exists)
  --dir PATH  Install directory (default: /app/minigamesvr)
  --restart   systemctl restart only (no rebuild)
  --stop      systemctl stop
  --status    systemctl status + healthz
  --logs      journalctl -u minigamesvr -f
  -h, --help  Show help

No Docker. Requires: Go 1.22+, systemd (for service install), root for /app.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir)     INSTALL_DIR="$2"; shift 2 ;;
    --restart) ACTION="restart"; shift ;;
    --stop)    ACTION="stop"; shift ;;
    --status)  ACTION="status"; shift ;;
    --logs)    ACTION="logs"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) err "unknown option: $1 (see --help)" ;;
  esac
done

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    err "please run as root (needed for ${INSTALL_DIR} and systemd)"
  fi
}

load_mysql_env() {
  if [[ -f "${MYSQL_ENV_FILE}" ]]; then
    set -a
    # shellcheck disable=SC1090
    # mysql.env must quote RANK_MYSQL_DSN (contains () ? &)
    source "${MYSQL_ENV_FILE}"
    set +a
    log "loaded ${MYSQL_ENV_FILE}"
  else
    log "warn: ${MYSQL_ENV_FILE} not found; using built-in defaults"
  fi
  if [[ -f "${TIKTOK_ENV_FILE}" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "${TIKTOK_ENV_FILE}"
    set +a
    log "loaded ${TIKTOK_ENV_FILE}"
  fi
  MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
  MYSQL_PORT="${MYSQL_PORT:-3306}"
  MYSQL_ROOT_USER="${MYSQL_ROOT_USER:-root}"
  MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-jgyjgyjgy}"
  MYSQL_DATABASE="${MYSQL_DATABASE:-minigameserver}"
  RANK_MYSQL_DSN="${RANK_MYSQL_DSN:-${MYSQL_ROOT_USER}:${MYSQL_ROOT_PASSWORD}@tcp(${MYSQL_HOST}:${MYSQL_PORT})/${MYSQL_DATABASE}?parseTime=true&loc=UTC&charset=utf8mb4}"
  RANK_AUTH_MODE="${RANK_AUTH_MODE:-mock,tiktok}"
  RANK_AUTH_CHANNEL_MAP="${RANK_AUTH_CHANNEL_MAP:-tiktok_minis:tiktok,douyin:tiktok,weixin:mock}"
  RANK_TT_APP_ID="${RANK_TT_APP_ID:-7657847833046812688}"
  RANK_TT_CLIENT_KEY="${RANK_TT_CLIENT_KEY:-mg79au52hgl5ggpi}"
  RANK_TT_CLIENT_SECRET="${RANK_TT_CLIENT_SECRET:-QZAxJCXzGhbK2RWwt3IpXBHN90yNoKto}"
  RANK_TT_CLIENT_SECRETS="${RANK_TT_CLIENT_SECRETS:-mg79au52hgl5ggpi:QZAxJCXzGhbK2RWwt3IpXBHN90yNoKto,mgfsbs7zd5qw5o0k:YwBP4Nisjw5FMnAynylbE4OCpjbddFQJ}"
  RANK_DEFAULT_APP_ID="${RANK_DEFAULT_APP_ID:-parking_smart_brain}"
}

upsert_env_key() {
  local file="$1" key="$2" val="$3"
  local qval line tmp
  qval="${val//\\/\\\\}"
  qval="${qval//\"/\\\"}"
  line="${key}=\"${qval}\""
  tmp="$(mktemp)"
  if grep -qE "^${key}=" "${file}"; then
    awk -v k="${key}" -v l="${line}" '
      index($0, k "=") == 1 { print l; next }
      { print }
    ' "${file}" >"${tmp}" && mv "${tmp}" "${file}"
  else
    printf '%s\n' "${line}" >>"${file}"
    rm -f "${tmp}"
  fi
}

write_env_file() {
  local dest="${INSTALL_DIR}/${ENV_NAME}"
  local source_env=""

  if [[ -f "${dest}" ]]; then
    source_env="${dest}"
  elif [[ -f "${BACKUP_ROOT}/.minigamesvr_last_backup" ]]; then
    local last_bak
    last_bak="$(cat "${BACKUP_ROOT}/.minigamesvr_last_backup")"
    if [[ -f "${last_bak}/${ENV_NAME}" ]]; then
      source_env="${last_bak}/${ENV_NAME}"
    fi
  fi

  if [[ -n "${source_env}" ]]; then
    if [[ "${source_env}" != "${dest}" ]]; then
      cp -a "${source_env}" "${dest}"
    fi
  else
    if [[ -f "${ENV_EXAMPLE}" ]]; then
      cp -a "${ENV_EXAMPLE}" "${dest}"
    else
      cat >"${dest}" <<EOF
RANK_HTTP_ADDR=:8000
RANK_GRPC_ADDR=:8001
RANK_STORE=mysql
RANK_MYSQL_DSN=${RANK_MYSQL_DSN}
RANK_SESSION_SECRET=change-me-session-secret
RANK_SERVICE_TOKEN=change-me-service-token
RANK_SESSION_TTL_SEC=86400
RANK_TOPN_DEFAULT=100
RANK_TOPN_MAX=100
RANK_REFRESH_SEC=30
RANK_TZ=UTC
EOF
    fi
    log "wrote new env: ${dest}"
  fi

  upsert_env_key "${dest}" "RANK_STORE" "mysql"
  upsert_env_key "${dest}" "RANK_MYSQL_DSN" "${RANK_MYSQL_DSN}"
  upsert_env_key "${dest}" "RANK_AUTH_MODE" "${RANK_AUTH_MODE}"
  if [[ -n "${RANK_AUTH_CHANNEL_MAP:-}" ]]; then
    upsert_env_key "${dest}" "RANK_AUTH_CHANNEL_MAP" "${RANK_AUTH_CHANNEL_MAP}"
  fi
  upsert_env_key "${dest}" "RANK_TT_APP_ID" "${RANK_TT_APP_ID}"
  upsert_env_key "${dest}" "RANK_TT_CLIENT_KEY" "${RANK_TT_CLIENT_KEY}"
  upsert_env_key "${dest}" "RANK_TT_CLIENT_SECRET" "${RANK_TT_CLIENT_SECRET}"
  if [[ -n "${RANK_TT_CLIENT_SECRETS:-}" ]]; then
    upsert_env_key "${dest}" "RANK_TT_CLIENT_SECRETS" "${RANK_TT_CLIENT_SECRETS}"
  fi
  upsert_env_key "${dest}" "RANK_DEFAULT_APP_ID" "${RANK_DEFAULT_APP_ID}"
  log "synced TikTok + MySQL credentials into ${dest}"
  chmod 600 "${dest}"
}

dir_has_content() {
  local d="$1"
  [[ -d "${d}" ]] || return 1
  [[ -n "$(find "${d}" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null || true)" ]]
}

backup_if_needed() {
  if ! dir_has_content "${INSTALL_DIR}"; then
    log "install dir empty or new: ${INSTALL_DIR}"
    return
  fi
  local ts bak
  ts="$(date +%Y%m%d_%H%M%S)"
  bak="${BACKUP_ROOT}/minigamesvr_backup_${ts}"
  mkdir -p "${BACKUP_ROOT}"
  log "backing up ${INSTALL_DIR} 閳?${bak}"
  cp -a "${INSTALL_DIR}" "${bak}"
  echo "${bak}" >"${BACKUP_ROOT}/.minigamesvr_last_backup"
  log "backup done"
}

install_ctl_scripts() {
  [[ -d "${APP_SCRIPTS_DIR}" ]] || err "missing app scripts: ${APP_SCRIPTS_DIR}"
  local name
  for name in start.sh stop.sh restart.sh status.sh; do
    [[ -f "${APP_SCRIPTS_DIR}/${name}" ]] || err "missing ${APP_SCRIPTS_DIR}/${name}"
    install -m 755 "${APP_SCRIPTS_DIR}/${name}" "${INSTALL_DIR}/${name}"
    log "installed ctl script: ${INSTALL_DIR}/${name}"
  done
}

build_binary() {
  need_cmd go
  local out="${INSTALL_DIR}/${BINARY_NAME}"
  local tmp
  tmp="$(mktemp)"
  log "building ${BINARY_NAME} from ${REPO_ROOT} ..."
  (
    cd "${REPO_ROOT}"
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
      -o "${tmp}" ./cmd/minigameserver
  )
  install -m 755 "${tmp}" "${out}"
  rm -f "${tmp}"
  log "installed binary: ${out}"
}

install_systemd() {
  need_cmd systemctl
  [[ -f "${SERVICE_UNIT_SRC}" ]] || err "missing unit: ${SERVICE_UNIT_SRC}"

  # Rewrite paths if INSTALL_DIR overridden
  local unit_dst="/etc/systemd/system/${SERVICE_NAME}.service"
  if [[ "${INSTALL_DIR}" == "/app/minigamesvr" ]]; then
    install -m 644 "${SERVICE_UNIT_SRC}" "${unit_dst}"
  else
    sed \
      -e "s|/app/minigamesvr|${INSTALL_DIR}|g" \
      "${SERVICE_UNIT_SRC}" >"${unit_dst}"
    chmod 644 "${unit_dst}"
  fi
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}"
  systemctl restart "${SERVICE_NAME}"
  log "systemd service ${SERVICE_NAME} restarted"
}

wait_health() {
  local url="http://127.0.0.1:8000/healthz"
  local i
  for ((i = 1; i <= 30; i++)); do
    if curl -sf "${url}" >/dev/null 2>&1; then
      log "healthy: ${url}"
      return 0
    fi
    sleep 1
  done
  log "warn: healthz not ready yet; check: systemctl status ${SERVICE_NAME}"
}

print_summary() {
  cat <<EOF

========== app deploy done ==========
  dir     : ${INSTALL_DIR}
  binary  : ${INSTALL_DIR}/${BINARY_NAME}
  env     : ${INSTALL_DIR}/${ENV_NAME}
  service : ${SERVICE_NAME}.service
  health  : http://127.0.0.1:8000/healthz
  HTTP    : :8000
  gRPC    : :8001

  ctl scripts:
    sudo ${INSTALL_DIR}/start.sh
    sudo ${INSTALL_DIR}/stop.sh
    sudo ${INSTALL_DIR}/restart.sh
         ${INSTALL_DIR}/status.sh
=====================================
EOF
}

do_deploy() {
  require_root
  need_cmd curl
  load_mysql_env

  mkdir -p "${INSTALL_DIR}"
  backup_if_needed
  mkdir -p "${INSTALL_DIR}"

  build_binary
  write_env_file
  install_ctl_scripts
  install_systemd
  wait_health
  print_summary
}

do_restart() {
  require_root
  need_cmd systemctl
  systemctl restart "${SERVICE_NAME}"
  wait_health
  log "restarted"
}

do_stop() {
  require_root
  need_cmd systemctl
  systemctl stop "${SERVICE_NAME}"
  log "stopped"
}

do_status() {
  need_cmd systemctl
  systemctl --no-pager status "${SERVICE_NAME}" || true
  if curl -sf "http://127.0.0.1:8000/healthz" >/dev/null 2>&1; then
    log "HTTP healthz: OK"
  else
    log "HTTP healthz: not ready"
  fi
}

do_logs() {
  need_cmd journalctl
  journalctl -u "${SERVICE_NAME}" -f
}

case "${ACTION}" in
  deploy)  do_deploy ;;
  restart) do_restart ;;
  stop)    do_stop ;;
  status)  do_status ;;
  logs)    do_logs ;;
  *)       err "unknown action: ${ACTION}" ;;
esac
