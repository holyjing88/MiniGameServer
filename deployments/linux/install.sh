#!/usr/bin/env bash
# Unpack a minigamesvr package into /app/minigamesvr (Linux only).
# Backs up existing install first.
#
# Usage:
#   # from extracted package directory:
#   sudo ./install.sh
#   # from repo (uses ./release + ./deployments/db):
#   sudo ./deployments/linux/install.sh
#   # from a tar.gz:
#   sudo ./install.sh ./minigamesvr_xxx.tar.gz
#   sudo ./deployments/linux/install.sh --package dist/minigamesvr_xxx.tar.gz
#
#   sudo ./install.sh --dir /app/minigamesvr
#   sudo ./install.sh --no-start
#   sudo ./install.sh --no-systemd
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "[install] ERROR: Linux only" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-/app/minigamesvr}"
BACKUP_ROOT="${BACKUP_ROOT:-/app}"
BACKUP_DIR="${BACKUP_DIR:-${BACKUP_ROOT}/minigamesvr_backup}"
PACKAGE=""
DO_START=1
SERVICE_NAME="${SERVICE_NAME:-minigamesvr}"
USE_SYSTEMD=0

log()  { printf '[install] %s\n' "$*"; }
err()  { printf '[install] ERROR: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "missing command: $1"; }

usage() {
  cat <<'EOF'
Usage: install.sh [options] [PACKAGE.tar.gz]

  (default)           Install from package dir or repo (release/ + deployments/db/)
  --package FILE      Install from tar.gz package
  PACKAGE.tar.gz      Same as --package FILE (positional)
  --dir PATH          Install directory (default: /app/minigamesvr)
  --no-start          Do not start process after install
  --systemd           Install/enable systemd unit (optional)
  --no-systemd        Do not install systemd unit (default)
  -h, --help          Show help

Layout after install:
  <dir>/release/bin/minigamesvr
  <dir>/release/bin/{start,stop,restart,status}.sh
  <dir>/release/cfg/...
  <dir>/deployments/db/...

Backup (when <dir> already has content):
  /app/minigamesvr_backup/minigamesvr_<timestamp>.tar.gz
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --package)    PACKAGE="$2"; shift 2 ;;
    --dir)        INSTALL_DIR="$2"; shift 2 ;;
    --no-start)   DO_START=0; shift ;;
    --systemd)    USE_SYSTEMD=1; shift ;;
    --no-systemd) USE_SYSTEMD=0; shift ;;
    -h|--help)    usage; exit 0 ;;
    -*)           err "unknown option: $1 (see --help)" ;;
    *.tar.gz|*.tgz)
      [[ -z "${PACKAGE}" ]] || err "package already set: ${PACKAGE}"
      PACKAGE="$1"; shift
      ;;
    *) err "unknown argument: $1 (see --help)" ;;
  esac
done

if [[ "$(id -u)" -ne 0 ]]; then
  err "please run as root: sudo $0 ..."
fi

need tar

SRC_DIR=""
TMP_EXTRACT=""
ENV_SAVE=""
cleanup() {
  [[ -n "${TMP_EXTRACT}" && -d "${TMP_EXTRACT}" ]] && rm -rf "${TMP_EXTRACT}"
  [[ -n "${ENV_SAVE}" && -f "${ENV_SAVE}" ]] && rm -f "${ENV_SAVE}"
}
trap cleanup EXIT

resolve_src_dir() {
  if [[ -n "${PACKAGE}" ]]; then
    [[ -f "${PACKAGE}" ]] || err "package not found: ${PACKAGE}"
    TMP_EXTRACT="$(mktemp -d)"
    log "extracting ${PACKAGE}"
    tar -xzf "${PACKAGE}" -C "${TMP_EXTRACT}"
    SRC_DIR="${TMP_EXTRACT}"
    return
  fi
  if [[ -d "${SCRIPT_DIR}/release" && -d "${SCRIPT_DIR}/deployments/db" ]]; then
    SRC_DIR="${SCRIPT_DIR}"
    return
  fi
  local repo_root
  repo_root="$(cd "${SCRIPT_DIR}/../.." && pwd)"
  if [[ -d "${repo_root}/release" && -d "${repo_root}/deployments/db" ]]; then
    SRC_DIR="${repo_root}"
    return
  fi
  err "cannot find release/ + deployments/db (extract package, run from repo, or pass --package)"
}

# Parse KEY=VALUE without shell `source` (DSN contains () ? &).
load_env_file() {
  local file="$1"
  local line key val
  [[ -f "${file}" ]] || return 0
  while IFS= read -r line || [[ -n "${line}" ]]; do
    line="${line%$'\r'}"
    [[ -z "${line}" || "${line}" =~ ^[[:space:]]*# ]] && continue
    [[ "${line}" == *=* ]] || continue
    key="${line%%=*}"
    val="${line#*=}"
    key="${key%"${key##*[![:space:]]}"}"
    key="${key#"${key%%[![:space:]]*}"}"
    [[ "${key}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    if [[ "${val}" =~ ^\"(.*)\"$ ]]; then
      val="${BASH_REMATCH[1]}"
    elif [[ "${val}" =~ ^\'(.*)\'$ ]]; then
      val="${BASH_REMATCH[1]}"
    fi
    export "${key}=${val}"
  done < "${file}"
}

upsert_env_key() {
  local file="$1" key="$2" val="$3"
  local qval line tmp
  # Quote values so DSN with () ? & stays safe if ever sourced.
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

sync_db_credentials() {
  local env_dst="${INSTALL_DIR}/release/cfg/minigamesvr.env"
  local mysql_env="${INSTALL_DIR}/deployments/db/mysql.env"
  local tiktok_env="${INSTALL_DIR}/deployments/db/tiktok.env"
  [[ -f "${env_dst}" ]] || return 0

  if [[ -f "${mysql_env}" ]]; then
    load_env_file "${mysql_env}"
    if [[ -n "${RANK_MYSQL_DSN:-}" ]]; then
      upsert_env_key "${env_dst}" "RANK_MYSQL_DSN" "${RANK_MYSQL_DSN}"
      upsert_env_key "${env_dst}" "RANK_STORE" "mysql"
      log "synced MySQL DSN from deployments/db/mysql.env"
    fi
  fi
  if [[ -f "${tiktok_env}" ]]; then
    load_env_file "${tiktok_env}"
    [[ -n "${RANK_AUTH_MODE:-}" ]] && upsert_env_key "${env_dst}" "RANK_AUTH_MODE" "${RANK_AUTH_MODE}"
    [[ -n "${RANK_TT_APP_ID:-}" ]] && upsert_env_key "${env_dst}" "RANK_TT_APP_ID" "${RANK_TT_APP_ID}"
    [[ -n "${RANK_TT_CLIENT_KEY:-}" ]] && upsert_env_key "${env_dst}" "RANK_TT_CLIENT_KEY" "${RANK_TT_CLIENT_KEY}"
    [[ -n "${RANK_TT_CLIENT_SECRET:-}" ]] && upsert_env_key "${env_dst}" "RANK_TT_CLIENT_SECRET" "${RANK_TT_CLIENT_SECRET}"
    [[ -n "${RANK_DEFAULT_APP_ID:-}" ]] && upsert_env_key "${env_dst}" "RANK_DEFAULT_APP_ID" "${RANK_DEFAULT_APP_ID}"
    log "synced TikTok creds from deployments/db/tiktok.env"
  fi
  chmod 600 "${env_dst}"
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
  mkdir -p "${BACKUP_DIR}"
  bak="${BACKUP_DIR}/minigamesvr_${ts}.tar.gz"
  log "backing up ${INSTALL_DIR} -> ${bak}"
  if [[ -x "${INSTALL_DIR}/release/bin/stop.sh" ]]; then
    "${INSTALL_DIR}/release/bin/stop.sh" || true
  elif command -v systemctl >/dev/null 2>&1; then
    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  fi
  tar -C "${INSTALL_DIR}" -czf "${bak}" .
  chmod 644 "${bak}"
  echo "${bak}" >"${BACKUP_DIR}/.minigamesvr_last_backup"
  # Keep legacy pointer for older scripts that still look under BACKUP_ROOT.
  echo "${bak}" >"${BACKUP_ROOT}/.minigamesvr_last_backup"
  log "backup done: ${bak}"
}

ensure_env_file() {
  local env_dst="${INSTALL_DIR}/release/cfg/minigamesvr.env"
  if [[ -n "${ENV_SAVE}" && -f "${ENV_SAVE}" ]]; then
    cp -a "${ENV_SAVE}" "${env_dst}"
    log "kept existing env: ${env_dst}"
    return
  fi
  local last_bak=""
  if [[ -f "${BACKUP_DIR}/.minigamesvr_last_backup" ]]; then
    last_bak="$(cat "${BACKUP_DIR}/.minigamesvr_last_backup")"
  elif [[ -f "${BACKUP_ROOT}/.minigamesvr_last_backup" ]]; then
    last_bak="$(cat "${BACKUP_ROOT}/.minigamesvr_last_backup")"
  fi
  if [[ -n "${last_bak}" ]]; then
    if [[ -f "${last_bak}" && "${last_bak}" == *.tar.gz ]]; then
      if tar -tzf "${last_bak}" 2>/dev/null | grep -qx 'release/cfg/minigamesvr.env'; then
        tar -xzf "${last_bak}" -O "release/cfg/minigamesvr.env" >"${env_dst}"
        chmod 600 "${env_dst}"
        log "restored env from backup: ${env_dst}"
        return
      fi
    elif [[ -f "${last_bak}/release/cfg/minigamesvr.env" ]]; then
      cp -a "${last_bak}/release/cfg/minigamesvr.env" "${env_dst}"
      log "restored env from backup: ${env_dst}"
      return
    fi
  fi
  if [[ ! -f "${env_dst}" && -f "${INSTALL_DIR}/release/cfg/minigamesvr.env.example" ]]; then
    cp -a "${INSTALL_DIR}/release/cfg/minigamesvr.env.example" "${env_dst}"
    log "created env from example: ${env_dst}"
  fi
}

install_tree() {
  mkdir -p "${INSTALL_DIR}"
  rm -rf "${INSTALL_DIR}/release" "${INSTALL_DIR}/deployments"
  mkdir -p "${INSTALL_DIR}/deployments"
  cp -a "${SRC_DIR}/release" "${INSTALL_DIR}/release"
  cp -a "${SRC_DIR}/deployments/db" "${INSTALL_DIR}/deployments/db"
  if [[ -f "${SRC_DIR}/deployments/schema.sql" ]]; then
    cp -a "${SRC_DIR}/deployments/schema.sql" "${INSTALL_DIR}/deployments/schema.sql"
  elif [[ -f "${SRC_DIR}/deployments/db/schema.sql" ]]; then
    cp -a "${SRC_DIR}/deployments/db/schema.sql" "${INSTALL_DIR}/deployments/schema.sql"
  fi
  chmod 755 "${INSTALL_DIR}/release/bin/minigamesvr" || true
  chmod 755 "${INSTALL_DIR}/release/bin/"*.sh
  ensure_env_file
  sync_db_credentials
}

install_systemd() {
  if [[ "${USE_SYSTEMD}" -ne 1 ]]; then
    log "skip systemd (--no-systemd)"
    return
  fi
  if ! command -v systemctl >/dev/null 2>&1; then
    log "warn: systemctl not found; skip systemd unit"
    return
  fi
  local unit_src="${INSTALL_DIR}/release/cfg/minigamesvr.service"
  local unit_dst="/etc/systemd/system/${SERVICE_NAME}.service"
  [[ -f "${unit_src}" ]] || err "missing unit: ${unit_src}"
  sed -e "s|/app/minigamesvr|${INSTALL_DIR}|g" "${unit_src}" >"${unit_dst}"
  chmod 644 "${unit_dst}"
  systemctl daemon-reload
  systemctl enable "${SERVICE_NAME}" >/dev/null
  log "systemd unit installed: ${unit_dst}"
}

start_app() {
  if [[ "${DO_START}" -ne 1 ]]; then
    log "skip start (--no-start)"
    return
  fi
  local start_sh="${INSTALL_DIR}/release/bin/start.sh"
  local stop_sh="${INSTALL_DIR}/release/bin/stop.sh"
  # Prefer relative ctl scripts so the tree can be relocated.
  if [[ "${USE_SYSTEMD}" -eq 1 ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl stop "${SERVICE_NAME}" 2>/dev/null || true
  fi
  [[ -x "${stop_sh}" ]] && "${stop_sh}" || true
  "${start_sh}"
}

print_summary() {
  cat <<EOF

========== install done ==========
  dir     : ${INSTALL_DIR}
  binary  : ${INSTALL_DIR}/release/bin/minigamesvr
  env     : ${INSTALL_DIR}/release/cfg/minigamesvr.env
  db      : ${INSTALL_DIR}/deployments/db

  ctl (relative paths, portable):
    ${INSTALL_DIR}/release/bin/start.sh
    ${INSTALL_DIR}/release/bin/stop.sh
    ${INSTALL_DIR}/release/bin/restart.sh
    ${INSTALL_DIR}/release/bin/status.sh
==================================
EOF
}

resolve_src_dir
[[ -d "${SRC_DIR}/release" ]] || err "missing ${SRC_DIR}/release"
[[ -d "${SRC_DIR}/deployments/db" ]] || err "missing ${SRC_DIR}/deployments/db"
[[ -f "${SRC_DIR}/release/bin/minigamesvr" ]] || err "missing binary: ${SRC_DIR}/release/bin/minigamesvr"

if [[ -f "${INSTALL_DIR}/release/cfg/minigamesvr.env" ]]; then
  ENV_SAVE="$(mktemp)"
  cp -a "${INSTALL_DIR}/release/cfg/minigamesvr.env" "${ENV_SAVE}"
fi

backup_if_needed
install_tree
install_systemd
start_app
print_summary
