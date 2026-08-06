#!/usr/bin/env bash
# Shared helpers for release/bin ctl scripts (sourced, not executed).
# Paths are relative to release/bin (script location).

_CTL_CALLER="${BASH_SOURCE[1]:-${BASH_SOURCE[0]}}"
BIN_DIR="$(cd "$(dirname "${_CTL_CALLER}")" && pwd)"
# release/{bin,cfg,log} — all relative to BIN_DIR
RELEASE_DIR="$(cd "${BIN_DIR}/.." && pwd)"
CFG_DIR="$(cd "${BIN_DIR}/../cfg" && pwd)"
LOG_DIR="$(cd "${BIN_DIR}/.." && pwd)/log"   # release/log (created on demand)
APP_ROOT="$(cd "${BIN_DIR}/../.." && pwd)"
BINARY="${BIN_DIR}/minigamesvr"
ENV_FILE="${CFG_DIR}/minigamesvr.env"
ENV_EXAMPLE="${CFG_DIR}/minigamesvr.env.example"
PID_FILE="${BIN_DIR}/minigamesvr.pid"
LOG_FILE="${LOG_DIR}/minigamesvr.log"

ensure_log_dir() {
  mkdir -p "${BIN_DIR}/../log"
  LOG_DIR="$(cd "${BIN_DIR}/../log" && pwd)"
  LOG_FILE="${LOG_DIR}/minigamesvr.log"
}

ensure_env() {
  if [[ ! -f "${ENV_FILE}" ]]; then
    if [[ -f "${ENV_EXAMPLE}" ]]; then
      cp -a "${ENV_EXAMPLE}" "${ENV_FILE}"
      echo "[ctl] created ${ENV_FILE} from example" >&2
    else
      echo "[ctl] ERROR: missing ${ENV_FILE}" >&2
      exit 1
    fi
  fi
}

# Parse KEY=VALUE without shell `source` (DSN contains () ? &).
load_env() {
  ensure_env
  local line key val
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
  done < "${ENV_FILE}"
}

http_base() {
  local addr="${RANK_HTTP_ADDR:-:8000}"
  if [[ "${addr}" == :* ]]; then
    echo "http://127.0.0.1${addr}"
  elif [[ "${addr}" =~ ^[0-9]+$ ]]; then
    echo "http://127.0.0.1:${addr}"
  else
    echo "http://${addr}"
  fi
}

# Extract listen port from addr forms: :8001 | 0.0.0.0:8001 | 8001
addr_port() {
  local addr="${1:-}"
  if [[ "${addr}" =~ :([0-9]+)$ ]]; then
    echo "${BASH_REMATCH[1]}"
  elif [[ "${addr}" =~ ^[0-9]+$ ]]; then
    echo "${addr}"
  fi
}

is_our_process() {
  local pid="$1"
  local cmd="" exe=""
  [[ -n "${pid}" && -d "/proc/${pid}" ]] || return 1
  if [[ -r "/proc/${pid}/cmdline" ]]; then
    cmd="$(tr '\0' ' ' <"/proc/${pid}/cmdline" 2>/dev/null || true)"
  fi
  exe="$(readlink -f "/proc/${pid}/exe" 2>/dev/null || true)"
  [[ "${cmd}" == *minigamesvr* || "${cmd}" == *minigameserver* ]] && return 0
  [[ "${exe}" == *minigamesvr* || "${exe}" == *minigameserver* ]] && return 0
  return 1
}

# PIDs listening on TCP port (LISTEN only).
pids_on_port() {
  local port="$1"
  [[ -n "${port}" ]] || return 0
  if command -v ss >/dev/null 2>&1; then
    # ss columns: Netid State Recv-Q Send-Q Local($5) Peer Process
    # Match Local address ending with :PORT (avoid :80 matching :8001).
    ss -ltnp 2>/dev/null | awk -v p=":${port}" '
      $2 == "LISTEN" {
        addr = $5
        if (substr(addr, length(addr) - length(p) + 1) != p) next
        while (match($0, /pid=[0-9]+/)) {
          print substr($0, RSTART + 4, RLENGTH - 4)
          $0 = substr($0, RSTART + RLENGTH)
        }
      }' | sort -u
  elif command -v lsof >/dev/null 2>&1; then
    lsof -t -iTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true
  elif command -v fuser >/dev/null 2>&1; then
    fuser "${port}/tcp" 2>/dev/null | grep -oE '[0-9]+' || true
  fi
}

describe_port() {
  local port="$1"
  [[ -n "${port}" ]] || return 0
  if command -v ss >/dev/null 2>&1; then
    ss -ltnp 2>/dev/null | awk -v p=":${port}" '
      $2 == "LISTEN" && substr($5, length($5) - length(p) + 1) == p { print }' || true
  elif command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true
  fi
}

is_running() {
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      if is_our_process "${pid}"; then
        return 0
      fi
      return 1
    fi
  fi
  return 1
}

wait_health() {
  local base="$1" label="$2"
  local i
  for i in $(seq 1 30); do
    if ! is_running; then
      echo "[${label}] process exited early; see ${LOG_FILE}" >&2
      if [[ -f "${LOG_FILE}" ]]; then
        tail -n 20 "${LOG_FILE}" >&2 || true
      fi
      return 1
    fi
    if curl -sf "${base}/healthz" >/dev/null 2>&1; then
      echo "[${label}] healthz OK (${base}/healthz)"
      return 0
    fi
    sleep 1
  done
  echo "[${label}] started; healthz not ready yet (${base}/healthz)" >&2
  return 1
}
