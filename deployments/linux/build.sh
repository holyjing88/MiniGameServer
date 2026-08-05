#!/usr/bin/env bash
# Build Linux amd64 binary on the current machine (no Docker, no deploy).
#
# Usage (on Linux):
#   ./deployments/linux/build.sh
#   ./deployments/linux/build.sh --out /app/minigamesvr/minigamesvr
#
# Default output: release/bin/minigamesvr
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
OUT="${OUT:-${REPO_ROOT}/release/bin/minigamesvr}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: build.sh [--out PATH]"
      exit 0
      ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

if ! command -v go >/dev/null 2>&1; then
  echo "[build] ERROR: go not found in PATH" >&2
  exit 1
fi

echo "[build] go: $(go version)"
echo "[build] repo: ${REPO_ROOT}"
echo "[build] out : ${OUT}"

mkdir -p "$(dirname "${OUT}")"
cd "${REPO_ROOT}"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o "${OUT}" ./cmd/minigameserver

chmod +x "${OUT}"
ls -lh "${OUT}"
file "${OUT}" || true
echo "[build] OK"
