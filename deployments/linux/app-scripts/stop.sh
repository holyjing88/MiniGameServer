#!/usr/bin/env bash
# Stop minigamesvr (systemd). Installed to /app/minigamesvr/stop.sh
set -euo pipefail

SERVICE_NAME="${SERVICE_NAME:-minigamesvr}"
INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "[stop] please run as root: sudo ${INSTALL_DIR}/stop.sh" >&2
  exit 1
fi

systemctl stop "${SERVICE_NAME}"
echo "[stop] ${SERVICE_NAME} stopped"
systemctl --no-pager --full status "${SERVICE_NAME}" || true
