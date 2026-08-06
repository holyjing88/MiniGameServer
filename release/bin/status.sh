#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=ctl-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ctl-common.sh"

echo "app_root : ${APP_ROOT}"
echo "binary   : ${BINARY}"
echo "env      : ${ENV_FILE}"
echo "pid_file : ${PID_FILE}"
echo "log_dir  : ${LOG_DIR}"
echo "log      : ${LOG_FILE}"

if is_running; then
  echo "process  : running pid=$(cat "${PID_FILE}")"
else
  echo "process  : not running"
fi

if [[ -f "${ENV_FILE}" ]]; then
  load_env
  base="$(http_base)"
  if curl -sf "${base}/healthz" >/dev/null 2>&1; then
    echo "healthz  : OK (${base}/healthz)"
  else
    echo "healthz  : not ready (${base}/healthz)"
  fi
else
  echo "healthz  : skipped (no env file)"
fi
