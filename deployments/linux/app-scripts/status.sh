#!/usr/bin/env bash
# Status for minigamesvr. Installed to /app/minigamesvr/status.sh
set -euo pipefail

SERVICE_NAME="${SERVICE_NAME:-minigamesvr}"

systemctl --no-pager --full status "${SERVICE_NAME}" || true
echo
if curl -sf "http://127.0.0.1:8080/healthz" >/dev/null 2>&1; then
  echo "[status] healthz: OK"
else
  echo "[status] healthz: not ready"
fi
