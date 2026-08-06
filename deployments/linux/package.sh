#!/usr/bin/env bash
# Package release/ + deployments/db/ for Linux deploy.
# Linux only.
#
# Usage:
#   ./deployments/linux/package.sh
#   ./deployments/linux/package.sh --out /tmp/minigamesvr.tgz
#
# Outputs:
#   dist/minigamesvr_<ts>.tar.gz   (or --out path)
#   dist/install.sh                (always, for: ./install.sh ./minigamesvr_xxx.tar.gz)
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "[package] ERROR: Linux only" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
DB_DIR="${REPO_ROOT}/deployments/db"
SCHEMA_SQL="${REPO_ROOT}/deployments/schema.sql"
INSTALL_SH="${SCRIPT_DIR}/install.sh"
OUT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: package.sh [--out PATH]"
      exit 0
      ;;
    *) echo "[package] unknown option: $1" >&2; exit 1 ;;
  esac
done

need() { command -v "$1" >/dev/null 2>&1 || { echo "[package] missing: $1" >&2; exit 1; }; }
need tar
need gzip

[[ -d "${RELEASE_DIR}/bin" ]] || { echo "[package] missing ${RELEASE_DIR}/bin" >&2; exit 1; }
[[ -d "${RELEASE_DIR}/cfg" ]] || { echo "[package] missing ${RELEASE_DIR}/cfg" >&2; exit 1; }
[[ -d "${DB_DIR}" ]] || { echo "[package] missing ${DB_DIR}" >&2; exit 1; }
[[ -f "${SCHEMA_SQL}" ]] || { echo "[package] missing ${SCHEMA_SQL}" >&2; exit 1; }
[[ -f "${INSTALL_SH}" ]] || { echo "[package] missing ${INSTALL_SH}" >&2; exit 1; }
[[ -f "${RELEASE_DIR}/bin/minigamesvr" ]] \
  || { echo "[package] missing binary: ${RELEASE_DIR}/bin/minigamesvr (build first)" >&2; exit 1; }

ts="$(date +%Y%m%d_%H%M%S)"
if [[ -z "${OUT}" ]]; then
  mkdir -p "${REPO_ROOT}/dist"
  OUT="${REPO_ROOT}/dist/minigamesvr_${ts}.tar.gz"
fi
mkdir -p "$(dirname "${OUT}")"

stage="$(mktemp -d)"
cleanup() { rm -rf "${stage}"; }
trap cleanup EXIT

mkdir -p "${stage}/release/bin" "${stage}/release/cfg" "${stage}/deployments"

install -m 755 "${RELEASE_DIR}/bin/minigamesvr" "${stage}/release/bin/minigamesvr"
for s in ctl-common.sh start.sh stop.sh restart.sh status.sh; do
  [[ -f "${RELEASE_DIR}/bin/${s}" ]] || { echo "[package] missing ${s}" >&2; exit 1; }
  install -m 755 "${RELEASE_DIR}/bin/${s}" "${stage}/release/bin/${s}"
done

for f in minigamesvr.service minigameserver.service minigamesvr.env.example; do
  [[ -f "${RELEASE_DIR}/cfg/${f}" ]] || { echo "[package] missing cfg/${f}" >&2; exit 1; }
  install -m 644 "${RELEASE_DIR}/cfg/${f}" "${stage}/release/cfg/${f}"
done

cp -a "${DB_DIR}" "${stage}/deployments/db"
install -m 644 "${SCHEMA_SQL}" "${stage}/deployments/schema.sql"
# Keep a copy inside db/ so init-db.sh works when only the db/ tree is copied.
install -m 644 "${SCHEMA_SQL}" "${stage}/deployments/db/schema.sql"
mkdir -p "${stage}/release/log"
: > "${stage}/release/log/.gitkeep"

install -m 755 "${INSTALL_SH}" "${stage}/install.sh"

tar -C "${stage}" -czf "${OUT}" release deployments install.sh
chmod 644 "${OUT}"

# Also drop install.sh next to the archive under dist/ for:
#   ./install.sh ./minigamesvr_xxx.tar.gz
DIST_DIR="${REPO_ROOT}/dist"
mkdir -p "${DIST_DIR}"
install -m 755 "${INSTALL_SH}" "${DIST_DIR}/install.sh"

echo "[package] OK"
echo "[package] out     : ${OUT}"
echo "[package] install : ${DIST_DIR}/install.sh"
ls -lh "${OUT}" "${DIST_DIR}/install.sh"
echo "[package] contents:"
tar -tzf "${OUT}"
