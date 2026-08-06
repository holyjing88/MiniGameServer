#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=ctl-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ctl-common.sh"

stop_pid() {
  local pid="$1"
  [[ -n "${pid}" ]] || return 0
  kill "${pid}" 2>/dev/null || true
  local i
  for i in $(seq 1 20); do
    if ! kill -0 "${pid}" 2>/dev/null; then
      return 0
    fi
    sleep 0.5
  done
  echo "[stop] force kill pid=${pid}"
  kill -9 "${pid}" 2>/dev/null || true
}

stop_pid_once() {
  local pid="$1" reason="$2"
  [[ -n "${pid}" ]] || return 0
  # Skip self / current shell
  [[ "${pid}" == "$$" || "${pid}" == "${PPID}" ]] && return 0
  if ! kill -0 "${pid}" 2>/dev/null; then
    return 0
  fi
  echo "[stop] ${reason} pid=${pid}"
  stop_pid "${pid}"
}

load_env

if is_running; then
  stop_pid_once "$(cat "${PID_FILE}")" "stopping"
else
  echo "[stop] no pidfile process"
fi
rm -f "${PID_FILE}"

# Sweep any leftover minigamesvr / minigameserver (any path, including deleted exe).
if command -v pgrep >/dev/null 2>&1; then
  while read -r pid; do
    [[ -n "${pid}" ]] || continue
    is_our_process "${pid}" || continue
    stop_pid_once "${pid}" "sweeping leftover"
  done < <(pgrep -x minigamesvr 2>/dev/null; pgrep -x minigameserver 2>/dev/null; pgrep -f '[/]minigamesvr( |$)' 2>/dev/null; pgrep -f '[/]minigameserver( |$)' 2>/dev/null || true)
fi

# Free configured listen ports held by our processes (covers orphan after binary replaced).
http_port="$(addr_port "${RANK_HTTP_ADDR:-:8000}")"
grpc_port="$(addr_port "${RANK_GRPC_ADDR:-:8001}")"
for port in ${http_port} ${grpc_port}; do
  [[ -n "${port}" ]] || continue
  while read -r pid; do
    [[ -n "${pid}" ]] || continue
    if is_our_process "${pid}"; then
      stop_pid_once "${pid}" "freeing :${port}"
    else
      # ss may show users:(("minigamesvr",pid=...)) even when /proc exe looks odd
      line="$(describe_port "${port}" | grep -E "pid=${pid}([^0-9]|$)" || true)"
      if [[ "${line}" == *minigamesvr* || "${line}" == *minigameserver* ]]; then
        stop_pid_once "${pid}" "freeing :${port}"
      else
        echo "[stop] warn: :${port} held by non-minigamesvr pid=${pid}" >&2
        describe_port "${port}" >&2 || true
      fi
    fi
  done < <(pids_on_port "${port}")
done

echo "[stop] stopped"
