#!/usr/bin/env bash
# Pack release/ + deployments/db into a deployable tarball.
#
# Usage:
#   ./deployments/linux/pack.sh
#   ./deployments/linux/pack.sh --out /tmp/minigamesvr.tgz
#   ./deployments/linux/pack.sh --name minigamesvr-prod
#
# Archive layout:
#   release/...
#   db/...          (from deployments/db)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RELEASE_DIR="${REPO_ROOT}/release"
DB_DIR="${REPO_ROOT}/deployments/db"
DIST_DIR="${DIST_DIR:-${REPO_ROOT}/dist}"
NAME=""
OUT=""

log()  { printf '[pack] %s\n' "$*"; }
err()  { printf '[pack] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: pack.sh [options]

  (default)   Create dist/minigamesvr-<timestamp>.tar.gz
  --out PATH  Output archive path (.tar.gz)
  --name NAME Base name without extension (default: minigamesvr-<timestamp>)
  --dist DIR  Output directory when --out not set (default: <repo>/dist)
  -h, --help  Show help

Packages: release/ and deployments/db/ (as db/).
Requires: tar. Binary should already be built (./deployments/linux/build.sh).
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out)  OUT="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --dist) DIST_DIR="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) err "unknown option: $1 (see --help)" ;;
  esac
done

[[ -d "${RELEASE_DIR}" ]] || err "missing release dir: ${RELEASE_DIR}"
[[ -d "${DB_DIR}" ]] || err "missing db dir: ${DB_DIR}"
[[ -f "${RELEASE_DIR}/bin/minigamesvr" ]] || err "missing binary: ${RELEASE_DIR}/bin/minigamesvr (run build.sh first)"
command -v tar >/dev/null 2>&1 || err "missing command: tar"

ts="$(date +%Y%m%d_%H%M%S)"
if [[ -z "${NAME}" ]]; then
  NAME="minigamesvr-${ts}"
fi
if [[ -z "${OUT}" ]]; then
  mkdir -p "${DIST_DIR}"
  OUT="${DIST_DIR}/${NAME}.tar.gz"
else
  mkdir -p "$(dirname "${OUT}")"
fi

stage="$(mktemp -d)"
cleanup() { rm -rf "${stage}"; }
trap cleanup EXIT

mkdir -p "${stage}/release" "${stage}/db"
# Copy trees; keep modes. Exclude editor junk if any.
cp -a "${RELEASE_DIR}/." "${stage}/release/"
cp -a "${DB_DIR}/." "${stage}/db/"

# Ensure ctl scripts + binary are executable in the package
chmod +x "${stage}/release/bin/minigamesvr" 2>/dev/null || true
chmod +x "${stage}/release/bin/"*.sh 2>/dev/null || true
chmod +x "${stage}/db/init-db.sh" 2>/dev/null || true

# Write a small manifest
cat >"${stage}/MANIFEST.txt" <<EOF
name=${NAME}
packed_at=${ts}
contents=release,db
binary=release/bin/minigamesvr
EOF

log "staging: release + db"
log "archive: ${OUT}"
tar -C "${stage}" -czf "${OUT}" release db MANIFEST.txt
ls -lh "${OUT}"
log "OK"
echo "${OUT}"