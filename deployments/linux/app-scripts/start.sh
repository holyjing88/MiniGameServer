#!/usr/bin/env bash
# Start minigamesvr (systemd). Installed to /app/minigamesvr/start.sh
set -euo pipefail

SERVICE_NAME="${SERVICE_NAME:-minigamesvr}"
INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "[start] please run as root: sudo ${INSTALL_DIR}/start.sh" >&2
  exit 1
fi

systemctl start "${SERVICE_NAME}"
systemctl --no-pager --full status "${SERVICE_NAME}" || true

if curl -sf "http://127.0.0.1:8080/healthz" >/dev/null 2>&1; then
  echo "[start] healthz OK"
else
  echo "[start] started; healthz not ready yet"
fi
