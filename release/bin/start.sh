#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=ctl-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ctl-common.sh"

if [[ ! -x "${BINARY}" ]]; then
  echo "[start] ERROR: binary not found/executable: ${BINARY}" >&2
  exit 1
fi

if is_running; then
  echo "[start] already running pid=$(cat "${PID_FILE}")"
  exit 0
fi

rm -f "${PID_FILE}"
ensure_log_dir
load_env

# Clear our leftovers on listen ports before bind (same logic as stop.sh port sweep).
http_port="$(addr_port "${RANK_HTTP_ADDR:-:8000}")"
grpc_port="$(addr_port "${RANK_GRPC_ADDR:-:8001}")"
for port in ${http_port} ${grpc_port}; do
  [[ -n "${port}" ]] || continue
  while read -r pid; do
    [[ -n "${pid}" ]] || continue
    if is_our_process "${pid}"; then
      echo "[start] freeing :${port} leftover pid=${pid}"
      kill "${pid}" 2>/dev/null || true
      sleep 0.3
      kill -9 "${pid}" 2>/dev/null || true
    else
      echo "[start] ERROR: :${port} already in use by pid=${pid} (not minigamesvr)" >&2
      describe_port "${port}" >&2 || true
      exit 1
    fi
  done < <(pids_on_port "${port}")
done

cd "${APP_ROOT}"
nohup "${BINARY}" >>"${LOG_FILE}" 2>&1 &
echo $! >"${PID_FILE}"
sleep 0.3
if ! is_running; then
  echo "[start] ERROR: process failed to stay up; see ${LOG_FILE}" >&2
  tail -n 30 "${LOG_FILE}" >&2 || true
  for port in ${http_port} ${grpc_port}; do
    [[ -n "${port}" ]] || continue
    if [[ -n "$(pids_on_port "${port}")" ]]; then
      echo "[start] port :${port} holders:" >&2
      describe_port "${port}" >&2 || true
    fi
  done
  rm -f "${PID_FILE}"
  exit 1
fi

echo "[start] started pid=$(cat "${PID_FILE}")"
echo "[start] bin=${BINARY}"
echo "[start] env=${ENV_FILE}"
echo "[start] log=${LOG_FILE}"

base="$(http_base)"
wait_health "${base}" "start"
