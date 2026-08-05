#!/usr/bin/env bash
# Restart minigamesvr (systemd). Installed to /app/minigamesvr/restart.sh
set -euo pipefail

SERVICE_NAME="${SERVICE_NAME:-minigamesvr}"
INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "[restart] please run as root: sudo ${INSTALL_DIR}/restart.sh" >&2
  exit 1
fi

systemctl restart "${SERVICE_NAME}"
systemctl --no-pager --full status "${SERVICE_NAME}" || true

for i in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:8080/healthz" >/dev/null 2>&1; then
    echo "[restart] healthz OK"
    exit 0
  fi
  sleep 1
done
echo "[restart] restarted; healthz not ready yet"
