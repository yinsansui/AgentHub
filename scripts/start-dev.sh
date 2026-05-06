#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTROL_PLANE_PORT="${AGENTHUB_CONTROL_PLANE_PORT:-3000}"
AGENT_POD_PORT="${AGENTHUB_AGENT_POD_PORT:-3001}"
USER_PORTAL_PORT="${AGENTHUB_USER_PORTAL_PORT:-5174}"
POSTGRES_PORT="${AGENTHUB_POSTGRES_PORT:-5432}"
WORKSPACE_ID="${AGENTHUB_WORKSPACE_ID:-ws_dev}"
POSTGRES_CONTAINER="${AGENTHUB_POSTGRES_CONTAINER:-agenthub-dev-postgres}"
DATABASE_URL="${AGENTHUB_DATABASE_URL:-}"
SKIP_BUILD="${AGENTHUB_SKIP_BUILD:-0}"
KEEP_POSTGRES="${AGENTHUB_KEEP_POSTGRES:-0}"

CONTROL_PLANE_URL="http://127.0.0.1:${CONTROL_PLANE_PORT}"
AGENT_POD_URL="http://127.0.0.1:${AGENT_POD_PORT}"
USER_PORTAL_URL="http://127.0.0.1:${USER_PORTAL_PORT}"
INTERNAL_TOKEN="${AGENTHUB_INTERNAL_TOKEN:-dev-token}"

PIDS=()
POSTGRES_STARTED=0

log() { printf '[agenthub-startup] %s\n' "$*" >&2; }
fail() { printf '[agenthub-startup] ERROR: %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

cleanup() {
  local status=$?
  log "stopping services..."
  for pid in "${PIDS[@]}"; do
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      kill "${pid}" 2>/dev/null || true
      wait "${pid}" 2>/dev/null || true
    fi
  done
  if [[ "${POSTGRES_STARTED}" == "1" && "${KEEP_POSTGRES}" != "1" ]]; then
    docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
    log "PostgreSQL container removed"
  fi
  if [[ ${status} -eq 0 ]]; then
    log "all services stopped"
  else
    log "exited with error (exit ${status})"
  fi
}
trap cleanup EXIT INT TERM

wait_http_ok() {
  local url="$1"
  local name="$2"
  local pid="${3:-}"
  local deadline=$((SECONDS + 60))
  until curl -fsS "${url}" >/dev/null 2>&1; do
    if [[ -n "${pid}" ]] && ! kill -0 "${pid}" 2>/dev/null; then
      fail "${name} exited before becoming ready"
    fi
    if (( SECONDS >= deadline )); then
      fail "timeout waiting for ${name} (${url})"
    fi
    sleep 0.5
  done
  log "${name} ready: ${url}"
}

wait_postgres() {
  local deadline=$((SECONDS + 60))
  until docker exec "${POSTGRES_CONTAINER}" pg_isready -U agenthub -d agenthub >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      docker logs "${POSTGRES_CONTAINER}" >&2 || true
      fail "timeout waiting for PostgreSQL"
    fi
    sleep 0.5
  done
  log "PostgreSQL ready"
}

require_cmd go
require_cmd npm
require_cmd curl
require_cmd docker

if [[ "${SKIP_BUILD}" != "1" ]]; then
  log "building ts-runtime-host..."
  (cd "${ROOT_DIR}/runtimes/ts-runtime-host" && npm ci >/dev/null && npm run build >/dev/null)
  log "ts-runtime-host built"
fi

if [[ -z "${DATABASE_URL}" ]]; then
  if docker ps -a --format '{{.Names}}' | grep -qx "${POSTGRES_CONTAINER}"; then
    log "reusing existing PostgreSQL container ${POSTGRES_CONTAINER}"
    docker start "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
    POSTGRES_STARTED=1
  else
    log "starting PostgreSQL container ${POSTGRES_CONTAINER} (port ${POSTGRES_PORT})..."
    docker run -d --name "${POSTGRES_CONTAINER}" \
      -p "127.0.0.1:${POSTGRES_PORT}:5432" \
      -e POSTGRES_USER=agenthub \
      -e POSTGRES_PASSWORD=agenthub \
      -e POSTGRES_DB=agenthub \
      postgres:16-alpine >/dev/null
    POSTGRES_STARTED=1
  fi
  wait_postgres
  DATABASE_URL="postgres://agenthub:agenthub@127.0.0.1:${POSTGRES_PORT}/agenthub?sslmode=disable"
else
  log "using external PostgreSQL"
fi

log "starting AgentPod (port ${AGENT_POD_PORT})..."
(
  cd "${ROOT_DIR}"
  AGENT_POD_ADDR=":${AGENT_POD_PORT}" \
  WORKSPACE_ID="${WORKSPACE_ID}" \
  WORKSPACE_DIR="${ROOT_DIR}/.agenthub/workspaces/${WORKSPACE_ID}" \
  AGENTHUB_INTERNAL_TOKEN="${INTERNAL_TOKEN}" \
  AGENTHUB_RUNTIME_COMMAND="node ${ROOT_DIR}/runtimes/ts-runtime-host/dist/main.js --adapter pi-coding-agent" \
  go run ./cmd/agent-pod >/dev/null 2>&1
) &
AGENT_POD_PID=$!
PIDS+=("${AGENT_POD_PID}")
wait_http_ok "${AGENT_POD_URL}/health" "AgentPod" "${AGENT_POD_PID}"

log "starting ControlPlane (port ${CONTROL_PLANE_PORT})..."
(
  cd "${ROOT_DIR}"
  AGENTHUB_CONTROL_PLANE_ADDR=":${CONTROL_PLANE_PORT}" \
  AGENTHUB_DATABASE_URL="${DATABASE_URL}" \
  AGENTHUB_AGENT_POD_BASE_URL_TEMPLATE="${AGENT_POD_URL}" \
  AGENTHUB_DEV_AGENT_POD_TOKEN="${INTERNAL_TOKEN}" \
  go run ./cmd/control-plane >/dev/null 2>&1
) &
CONTROL_PLANE_PID=$!
PIDS+=("${CONTROL_PLANE_PID}")
wait_http_ok "${CONTROL_PLANE_URL}/health" "ControlPlane" "${CONTROL_PLANE_PID}"

log "starting UserPortal (port ${USER_PORTAL_PORT})..."
(
  cd "${ROOT_DIR}/web/user-portal"
  AGENTHUB_USER_PORTAL_PROXY_TARGET="${CONTROL_PLANE_URL}" \
  npx vite --host 127.0.0.1 --port "${USER_PORTAL_PORT}" >/dev/null 2>&1
) &
USER_PORTAL_PID=$!
PIDS+=("${USER_PORTAL_PID}")

DEADLINE=$((SECONDS + 30))
until curl -fsS "${USER_PORTAL_URL}" >/dev/null 2>&1; do
  if ! kill -0 "${USER_PORTAL_PID}" 2>/dev/null; then
    fail "UserPortal exited before becoming ready"
  fi
  if (( SECONDS >= DEADLINE )); then
    fail "timeout waiting for UserPortal"
  fi
  sleep 0.5
done
log "UserPortal ready: ${USER_PORTAL_URL}"

echo ""
echo "========================================"
echo "  AgentHub dev environment started"
echo "========================================"
echo ""
echo "  ControlPlane: ${CONTROL_PLANE_URL}"
echo "  AgentPod:     ${AGENT_POD_URL}"
echo "  UserPortal:   ${USER_PORTAL_URL}"
echo "  Workspace:    ${WORKSPACE_ID}"
echo ""
echo "  Frontend: ${USER_PORTAL_URL}?workspaceId=${WORKSPACE_ID}"
echo ""
echo "  Press Ctrl+C to stop all services"
echo ""

wait
