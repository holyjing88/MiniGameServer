#!/usr/bin/env bash
# Deploy a package built by pack.sh onto the Linux host.
#
# - Back up /app/minigamesvr first (if non-empty)
# - Unpack release/ and db/ under /app/minigamesvr
# - Install/restart systemd unit from release/cfg/minigamesvr.service
#
# Usage (as root on the server):
#   sudo ./deployments/linux/deploy-package.sh /path/to/minigamesvr-xxx.tar.gz
#   sudo ./deployments/linux/deploy-package.sh --package ./dist/minigamesvr-xxx.tar.gz
#   sudo ./deployments/linux/deploy-package.sh --package xxx.tar.gz --no-restart
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-/app/minigamesvr}"
BACKUP_ROOT="${BACKUP_ROOT:-/app}"
SERVICE_NAME="${SERVICE_NAME:-minigamesvr}"
PACKAGE=""
DO_RESTART=1
DO_SYSTEMD=1
KEEP_ENV=0

log()  { printf '[deploy-pkg] %s\n' "$*"; }
err()  { printf '[deploy-pkg] ERROR: %s\n' "$*" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || err "missing command: $1"; }

usage() {
  cat <<'EOF'
Usage: deploy-package.sh --package PATH.tar.gz [options]

  --package PATH   Package produced by pack.sh (required)
  --dir PATH       Install directory (default: /app/minigamesvr)
  --backup-root D  Backup parent dir (default: /app)
  --no-restart     Unpack only; do not systemctl restart
  --no-systemd     Skip systemd unit install/enable
  --keep-env       Keep previous minigamesvr.env (opt-in; default is full refresh)
  -h, --help       Show help

Layout after deploy:
  /app/minigamesvr/release/...
  /app/minigamesvr/db/...

Before deploy, existing /app/minigamesvr is copied to:
  /app/minigamesvr_backup_<timestamp>
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --package)     PACKAGE="$2"; shift 2 ;;
    --dir)         INSTALL_DIR="$2"; shift 2 ;;
    --backup-root) BACKUP_ROOT="$2"; shift 2 ;;
    --no-restart)  DO_RESTART=0; shift ;;
    --no-systemd)  DO_SYSTEMD=0; DO_RESTART=0; shift ;;
    --keep-env)    KEEP_ENV=1; shift ;;
    -h|--help)     usage; exit 0 ;;
    *)
      if [[ -z "${PACKAGE}" && -f "$1" ]]; then
        PACKAGE="$1"; shift
      else
        err "unknown option: $1 (see --help)"
      fi
      ;;
  esac
done

[[ -n "${PACKAGE}" ]] || err "missing --package PATH (see --help)"
[[ -f "${PACKAGE}" ]] || err "package not found: ${PACKAGE}"
need_cmd tar

if [[ "$(id -u)" -ne 0 ]]; then
  err "please run as root (needed for ${INSTALL_DIR} and systemd)"
fi

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
  log "backing up ${INSTALL_DIR} -> ${bak}"
  cp -a "${INSTALL_DIR}" "${bak}"
  echo "${bak}" >"${BACKUP_ROOT}/.minigamesvr_last_backup"
  log "backup done: ${bak}"
}

ensure_env_file() {
  local env_dst="${INSTALL_DIR}/release/cfg/minigamesvr.env"
  local env_example="${INSTALL_DIR}/release/cfg/minigamesvr.env.example"
  local last_bak=""

  if [[ "${KEEP_ENV}" -eq 1 ]]; then
    if [[ -n "${ENV_SAVE}" && -f "${ENV_SAVE}" ]]; then
      mkdir -p "$(dirname "${env_dst}")"
      cp -a "${ENV_SAVE}" "${env_dst}"
      chmod 600 "${env_dst}"
      log "kept existing env (--keep-env): ${env_dst}"
      return
    fi
    if [[ -f "${BACKUP_ROOT}/.minigamesvr_last_backup" ]]; then
      last_bak="$(cat "${BACKUP_ROOT}/.minigamesvr_last_backup")"
    fi
    local candidates=(
      "${last_bak}/release/cfg/minigamesvr.env"
      "${last_bak}/minigamesvr.env"
    )
    local c
    for c in "${candidates[@]}"; do
      if [[ -n "${c}" && -f "${c}" ]]; then
        mkdir -p "$(dirname "${env_dst}")"
        cp -a "${c}" "${env_dst}"
        chmod 600 "${env_dst}"
        log "restored env from backup (--keep-env): ${c}"
        return
      fi
    done
    log "warn: --keep-env set but no previous env found; seeding from example"
  fi

  [[ -f "${env_example}" ]] || err "missing env example: ${env_example}"
  mkdir -p "$(dirname "${env_dst}")"
  cp -a "${env_example}" "${env_dst}"
  chmod 600 "${env_dst}"
  log "refreshed env from package example: ${env_dst}"
}

extract_package() {
  local stage
  stage="$(mktemp -d)"
  log "extracting ${PACKAGE}"
  tar -C "${stage}" -xzf "${PACKAGE}"
  [[ -d "${stage}/release" ]] || err "package missing release/"
  [[ -d "${stage}/db" ]] || err "package missing db/"

  mkdir -p "${INSTALL_DIR}"
  rm -rf "${INSTALL_DIR}/release" "${INSTALL_DIR}/db"
  cp -a "${stage}/release" "${INSTALL_DIR}/release"
  cp -a "${stage}/db" "${INSTALL_DIR}/db"
  rm -rf "${stage}"

  chmod +x "${INSTALL_DIR}/release/bin/minigamesvr" 2>/dev/null || true
  chmod +x "${INSTALL_DIR}/release/bin/"*.sh 2>/dev/null || true
  chmod +x "${INSTALL_DIR}/db/init-db.sh" 2>/dev/null || true
  log "installed: ${INSTALL_DIR}/release"
  log "installed: ${INSTALL_DIR}/db"
}

install_systemd() {
  need_cmd systemctl
  local unit_src="${INSTALL_DIR}/release/cfg/minigamesvr.service"
  local unit_dst="/etc/systemd/system/${SERVICE_NAME}.service"
  [[ -f "${unit_src}" ]] || err "missing unit: ${unit_src}"

  if [[ "${INSTALL_DIR}" == "/app/minigamesvr" ]]; then
    install -m 644 "${unit_src}" "${unit_dst}"
  else
    sed -e "s|/app/minigamesvr|${INSTALL_DIR}|g" "${unit_src}" >"${unit_dst}"
    chmod 644 "${unit_dst}"
  fi
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}"
  log "systemd unit installed: ${unit_dst}"
}

restart_service() {
  need_cmd systemctl
  systemctl restart "${SERVICE_NAME}"
  log "restarted ${SERVICE_NAME}"
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

========== package deploy done ==========
  dir     : ${INSTALL_DIR}
  release : ${INSTALL_DIR}/release
  db      : ${INSTALL_DIR}/db
  binary  : ${INSTALL_DIR}/release/bin/minigamesvr
  env     : ${INSTALL_DIR}/release/cfg/minigamesvr.env
  service : ${SERVICE_NAME}.service

  ctl:
    sudo ${INSTALL_DIR}/release/bin/start.sh
    sudo ${INSTALL_DIR}/release/bin/stop.sh
    sudo ${INSTALL_DIR}/release/bin/restart.sh
         ${INSTALL_DIR}/release/bin/status.sh
=========================================
EOF
}

ENV_SAVE=""
cleanup() {
  [[ -n "${ENV_SAVE}" && -f "${ENV_SAVE}" ]] && rm -f "${ENV_SAVE}"
}
trap cleanup EXIT

if [[ "${KEEP_ENV}" -eq 1 && -f "${INSTALL_DIR}/release/cfg/minigamesvr.env" ]]; then
  ENV_SAVE="$(mktemp)"
  cp -a "${INSTALL_DIR}/release/cfg/minigamesvr.env" "${ENV_SAVE}"
fi

backup_if_needed
extract_package
ensure_env_file

if [[ "${DO_SYSTEMD}" -eq 1 ]]; then
  install_systemd
fi
if [[ "${DO_RESTART}" -eq 1 ]]; then
  restart_service
fi
print_summary