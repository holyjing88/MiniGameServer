#!/usr/bin/env bash
# Stop stack and optionally wipe MySQL volume.
#   ./uninstall.sh           # stop services, keep data
#   ./uninstall.sh --purge   # also remove MySQL volume + /opt install
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/deployments/docker-compose.yml"
ENV_FILE="${SCRIPT_DIR}/.env"
PURGE=0

[[ "${1:-}" == "--purge" ]] && PURGE=1

log() { printf '[uninstall] %s\n' "$*"; }

compose() {
  if [[ ! -f "${ENV_FILE}" ]]; then
    ENV_FILE="${SCRIPT_DIR}/env.example"
  fi
  if docker compose version >/dev/null 2>&1; then
    docker compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" "$@"
  else
    docker-compose -f "${COMPOSE_FILE}" --env-file "${ENV_FILE}" "$@"
  fi
}

if systemctl list-unit-files 2>/dev/null | grep -q '^minigameserver.service'; then
  log "disable systemd unit"
  systemctl disable --now minigameserver 2>/dev/null || true
  rm -f /etc/systemd/system/minigameserver.service
  systemctl daemon-reload || true
fi

if [[ "${PURGE}" -eq 1 ]]; then
  log "compose down -v (remove volumes)"
  compose down -v || true
  if [[ -d /opt/minigameserver ]]; then
    log "remove /opt/minigameserver"
    rm -rf /opt/minigameserver
  fi
  if id -u minigameserver >/dev/null 2>&1; then
    userdel minigameserver 2>/dev/null || true
  fi
else
  log "compose down (keep volumes)"
  compose down || true
fi

log "done"
