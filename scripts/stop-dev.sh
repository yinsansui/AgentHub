#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTROL_PLANE_PORT="${AGENTHUB_CONTROL_PLANE_PORT:-3000}"
AGENT_POD_PORT="${AGENTHUB_AGENT_POD_PORT:-3001}"
USER_PORTAL_PORT="${AGENTHUB_USER_PORTAL_PORT:-5174}"
POSTGRES_CONTAINER="${AGENTHUB_POSTGRES_CONTAINER:-agenthub-dev-postgres}"
DATABASE_URL="${AGENTHUB_DATABASE_URL:-}"
KEEP_POSTGRES="${AGENTHUB_KEEP_POSTGRES:-0}"
BIN_DIR="${ROOT_DIR}/.agenthub/bin"

log() { printf '[agenthub-stop] %s\n' "$*" >&2; }

port_pids() {
  local port="$1"
  lsof -nP -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true
}

process_command() {
  local pid="$1"
  ps -p "${pid}" -o command= 2>/dev/null || true
}

process_cwd() {
  local pid="$1"
  local line=""
  while IFS= read -r line; do
    case "${line}" in
      n*) printf '%s\n' "${line#n}"; return 0 ;;
    esac
  done < <(lsof -a -p "${pid}" -d cwd -Fn 2>/dev/null || true)
}

is_control_plane() {
  local pid="$1"
  local command cwd
  command="$(process_command "${pid}")"
  cwd="$(process_cwd "${pid}")"
  [[ "${command}" == *"${BIN_DIR}/control-plane"* ]] || [[ "${cwd}" == "${ROOT_DIR}" && "${command}" == *"/control-plane"* ]]
}

is_agent_pod() {
  local pid="$1"
  local command cwd
  command="$(process_command "${pid}")"
  cwd="$(process_cwd "${pid}")"
  [[ "${command}" == *"${BIN_DIR}/agent-pod"* ]] || [[ "${cwd}" == "${ROOT_DIR}" && "${command}" == *"/agent-pod"* ]]
}

is_user_portal() {
  local pid="$1"
  local command cwd
  command="$(process_command "${pid}")"
  cwd="$(process_cwd "${pid}")"
  [[ "${cwd}" == "${ROOT_DIR}/web/user-portal" && ( "${command}" == *"vite"* || "${command}" == *"node"* ) ]]
}

pid_still_matches() {
  local service="$1"
  local pid="$2"
  case "${service}" in
    control-plane) is_control_plane "${pid}" ;;
    agent-pod) is_agent_pod "${pid}" ;;
    user-portal) is_user_portal "${pid}" ;;
    *) return 1 ;;
  esac
}

stop_port_service() {
  local service="$1"
  local label="$2"
  local port="$3"
  local candidates=()
  local pid

  while IFS= read -r pid; do
    [[ -n "${pid}" ]] || continue
    if pid_still_matches "${service}" "${pid}"; then
      candidates+=("${pid}")
    else
      log "skipping PID ${pid} on port ${port}; it does not look like AgentHub ${label}"
    fi
  done < <(port_pids "${port}")

  if [[ ${#candidates[@]} -eq 0 ]]; then
    log "${label} not running on port ${port}"
    return 0
  fi

  log "stopping ${label} on port ${port}: ${candidates[*]}"
  for pid in "${candidates[@]}"; do
    kill "${pid}" 2>/dev/null || true
  done

  local deadline=$((SECONDS + 10))
  while (( SECONDS < deadline )); do
    local still_running=0
    for pid in "${candidates[@]}"; do
      if kill -0 "${pid}" 2>/dev/null && pid_still_matches "${service}" "${pid}"; then
        still_running=1
        break
      fi
    done
    [[ ${still_running} -eq 0 ]] && return 0
    sleep 0.3
  done

  for pid in "${candidates[@]}"; do
    if kill -0 "${pid}" 2>/dev/null && pid_still_matches "${service}" "${pid}"; then
      log "force stopping ${label} PID ${pid}"
      kill -KILL "${pid}" 2>/dev/null || true
    fi
  done
}

stop_postgres() {
  if [[ -n "${DATABASE_URL}" ]]; then
    log "external PostgreSQL configured; skipping container cleanup"
    return 0
  fi
  if [[ "${KEEP_POSTGRES}" == "1" ]]; then
    log "AGENTHUB_KEEP_POSTGRES=1; leaving ${POSTGRES_CONTAINER} running"
    return 0
  fi
  if docker ps -a --format '{{.Names}}' | grep -qx "${POSTGRES_CONTAINER}"; then
    log "removing PostgreSQL container ${POSTGRES_CONTAINER}"
    docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  else
    log "PostgreSQL container ${POSTGRES_CONTAINER} not found"
  fi
}

stop_port_service user-portal UserPortal "${USER_PORTAL_PORT}"
stop_port_service control-plane ControlPlane "${CONTROL_PLANE_PORT}"
stop_port_service agent-pod AgentPod "${AGENT_POD_PORT}"
stop_postgres
log "dev services stopped"
