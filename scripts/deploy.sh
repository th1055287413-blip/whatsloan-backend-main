#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/deploy.sh [api|connector|all] --tag <TAG> [--reg <REG>] [--ns <NS>] [--platform <PLATFORM>]

Examples:
  ./scripts/deploy.sh api --tag 20260427-1
  ./scripts/deploy.sh connector --tag 20260427-1
  ./scripts/deploy.sh all --tag 20260427-1

Notes:
  - This script builds & pushes images via docker buildx.
  - After success, it prints the "online" docker compose commands to run on the server.
EOF
}

TARGET="${1:-}"
shift || true

if [[ -z "${TARGET}" || "${TARGET}" == "-h" || "${TARGET}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ "${TARGET}" != "api" && "${TARGET}" != "connector" && "${TARGET}" != "all" ]]; then
  echo "ERROR: invalid target: ${TARGET}" >&2
  usage
  exit 1
fi

REG="crpi-rt5xa8oxnflxi4jx.cn-hangzhou.personal.cr.aliyuncs.com"
NS="xyser"
TAG=""
PLATFORM="linux/amd64"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --reg) REG="${2:-}"; shift 2 ;;
    --ns) NS="${2:-}"; shift 2 ;;
    --tag) TAG="${2:-}"; shift 2 ;;
    --platform) PLATFORM="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "${TAG}" ]]; then
  echo "ERROR: --tag is required" >&2
  usage
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

echo "==> Target: ${TARGET}"
echo "==> REG: ${REG}"
echo "==> NS: ${NS}"
echo "==> TAG: ${TAG}"
echo "==> PLATFORM: ${PLATFORM}"
echo

echo "==> Logging in to registry: ${REG}"
docker login "${REG}"

echo "==> Ensuring docker buildx builder exists"
docker buildx create --name xbuilder --use 2>/dev/null || docker buildx use xbuilder
docker buildx inspect --bootstrap >/dev/null

build_api() {
  local image="${REG}/${NS}/whatsmeow-api:${TAG}"
  echo "==> Building & pushing API image: ${image}"
  docker buildx build \
    --platform "${PLATFORM}" \
    -t "${image}" \
    -f Dockerfile \
    --push \
    .
}

build_connector() {
  local image="${REG}/${NS}/whatsmeow-connector:${TAG}"
  echo "==> Building & pushing Connector image: ${image}"
  docker buildx build \
    --platform "${PLATFORM}" \
    -t "${image}" \
    -f Dockerfile.connector \
    --push \
    .
}

case "${TARGET}" in
  api)
    build_api
    ;;
  connector)
    build_connector
    ;;
  all)
    build_api
    build_connector
    ;;
esac

echo
echo "==> Done. Run the following on the server (in the folder with docker-compose.yml):"
echo

if [[ "${TARGET}" == "api" || "${TARGET}" == "all" ]]; then
  cat <<EOF
TAG=${TAG} docker compose pull api
TAG=${TAG} docker compose up -d api
EOF
  echo
fi

if [[ "${TARGET}" == "connector" || "${TARGET}" == "all" ]]; then
  cat <<EOF
CONNECTOR_TAG=${TAG} docker compose pull connector
CONNECTOR_TAG=${TAG} docker compose up -d connector
EOF
  echo
fi

echo "Tip: If you deployed both, you can also restart together:"
echo "TAG=${TAG} CONNECTOR_TAG=${TAG} docker compose up -d api connector"

