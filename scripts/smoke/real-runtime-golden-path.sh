#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKSPACE_ID="${AGENTHUB_SMOKE_WORKSPACE_ID:-ws_golden_path}"
MODEL_ID="${AGENTHUB_SMOKE_LLM_MODEL_ID:-k2p5}"
LLM_PROVIDER="${AGENTHUB_SMOKE_LLM_PROVIDER:-anthropic}"
LLM_API_PROTOCOL="${AGENTHUB_SMOKE_LLM_API_PROTOCOL:-anthropic-messages}"
LLM_BASE_URL="${AGENTHUB_SMOKE_LLM_BASE_URL:-}"
LLM_API_KEY="${AGENTHUB_SMOKE_LLM_API_KEY:-}"
CONTROL_PLANE_PORT="${AGENTHUB_SMOKE_CONTROL_PLANE_PORT:-3311}"
AGENT_POD_PORT="${AGENTHUB_SMOKE_AGENT_POD_PORT:-3310}"
POSTGRES_PORT="${AGENTHUB_SMOKE_POSTGRES_PORT:-55432}"
TIMEOUT_SECONDS="${AGENTHUB_SMOKE_TIMEOUT_SECONDS:-180}"
MARKER="${AGENTHUB_SMOKE_MARKER:-AGENTHUB_GOLDEN_PATH_OK}"
REFRESH_MODELS="${AGENTHUB_SMOKE_REFRESH_MODELS:-1}"
SKIP_BUILD="${AGENTHUB_SMOKE_SKIP_BUILD:-0}"
KEEP_ARTIFACTS="${AGENTHUB_SMOKE_KEEP_ARTIFACTS:-0}"
CONTROL_PLANE_URL="http://127.0.0.1:${CONTROL_PLANE_PORT}"
AGENT_POD_URL="http://127.0.0.1:${AGENT_POD_PORT}"
INTERNAL_TOKEN="${AGENTHUB_SMOKE_INTERNAL_TOKEN:-dev-token}"
RUN_ID="$(date -u +%Y%m%d%H%M%S)-$$"
TMP_DIR="${AGENTHUB_SMOKE_TMP_DIR:-${ROOT_DIR}/.agenthub/smoke/${RUN_ID}}"
WORKSPACE_DIR="${TMP_DIR}/workspace"
POSTGRES_CONTAINER="${AGENTHUB_SMOKE_POSTGRES_CONTAINER:-agenthub-smoke-postgres-${RUN_ID}}"
DATABASE_URL="${AGENTHUB_SMOKE_DATABASE_URL:-}"
POSTGRES_STARTED=0
CONTROL_PLANE_PID=""
AGENT_POD_PID=""

log() { printf '[agenthub-smoke] %s\n' "$*" >&2; }
fail() { printf '[agenthub-smoke] ERROR: %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

cleanup() {
  local status=$?
  if [[ -n "${CONTROL_PLANE_PID}" ]] && kill -0 "${CONTROL_PLANE_PID}" 2>/dev/null; then
    kill "${CONTROL_PLANE_PID}" 2>/dev/null || true
    wait "${CONTROL_PLANE_PID}" 2>/dev/null || true
  fi
  if [[ -n "${AGENT_POD_PID}" ]] && kill -0 "${AGENT_POD_PID}" 2>/dev/null; then
    kill "${AGENT_POD_PID}" 2>/dev/null || true
    wait "${AGENT_POD_PID}" 2>/dev/null || true
  fi
  if [[ "${POSTGRES_STARTED}" == "1" ]]; then
    docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  fi
  if [[ "${KEEP_ARTIFACTS}" != "1" && ${status} -eq 0 ]]; then
    rm -rf "${TMP_DIR}" || true
  else
    log "artifacts kept at ${TMP_DIR}"
  fi
}
trap cleanup EXIT

json_get() {
  local expr="$1"
  python3 -c 'import json,sys
expr=sys.argv[1]
data=json.load(sys.stdin)
try:
    value=eval(expr, {"__builtins__": {}}, {"data": data})
except Exception:
    value=""
if value is None:
    value=""
if isinstance(value, (dict, list)):
    print(json.dumps(value, ensure_ascii=False))
else:
    print(value)
' "$expr"
}

http_json() {
  local method="$1"
  local url="$2"
  local payload="${3:-}"
  local output status body
  output="$(mktemp "${TMP_DIR}/curl.XXXXXX")"
  if [[ -n "${payload}" ]]; then
    status="$(curl -sS -o "${output}" -w '%{http_code}' -X "${method}" "${url}" -H 'content-type: application/json' --data-binary "${payload}")"
  else
    status="$(curl -sS -o "${output}" -w '%{http_code}' -X "${method}" "${url}")"
  fi
  body="$(cat "${output}")"
  rm -f "${output}"
  if [[ "${status}" -lt 200 || "${status}" -ge 300 ]]; then
    fail "${method} ${url} returned ${status}: ${body}"
  fi
  printf '%s' "${body}"
}

wait_http_ok() {
  local url="$1"
  local pid="${2:-}"
  local log_file="${3:-}"
  local deadline=$((SECONDS + TIMEOUT_SECONDS))
  until curl -fsS "${url}" >/dev/null 2>&1; do
    if [[ -n "${pid}" ]] && ! kill -0 "${pid}" 2>/dev/null; then
      [[ -n "${log_file}" && -f "${log_file}" ]] && tail -100 "${log_file}" >&2 || true
      fail "process exited while waiting for ${url}"
    fi
    if (( SECONDS >= deadline )); then
      [[ -n "${log_file}" && -f "${log_file}" ]] && tail -100 "${log_file}" >&2 || true
      fail "timeout waiting for ${url}"
    fi
    sleep 1
  done
}

wait_postgres() {
  local deadline=$((SECONDS + TIMEOUT_SECONDS))
  until docker exec "${POSTGRES_CONTAINER}" pg_isready -U agenthub -d agenthub >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      docker logs "${POSTGRES_CONTAINER}" >&2 || true
      fail "timeout waiting for PostgreSQL"
    fi
    sleep 1
  done
}

require_cmd curl
require_cmd python3
require_cmd go
require_cmd npm
mkdir -p "${TMP_DIR}" "${WORKSPACE_DIR}"

if [[ -z "${LLM_BASE_URL}" || -z "${LLM_API_KEY}" ]]; then
  fail "set AGENTHUB_SMOKE_LLM_BASE_URL and AGENTHUB_SMOKE_LLM_API_KEY before running the real LLM smoke"
fi

if [[ "${SKIP_BUILD}" != "1" ]]; then
  log "building ts-runtime-host"
  (cd "${ROOT_DIR}/runtimes/ts-runtime-host" && npm ci >/dev/null && npm run build >/dev/null)
fi

if [[ -z "${DATABASE_URL}" ]]; then
  require_cmd docker
  log "starting PostgreSQL container ${POSTGRES_CONTAINER} on port ${POSTGRES_PORT}"
  docker run -d --name "${POSTGRES_CONTAINER}" \
    -p "127.0.0.1:${POSTGRES_PORT}:5432" \
    -e POSTGRES_USER=agenthub \
    -e POSTGRES_PASSWORD=agenthub \
    -e POSTGRES_DB=agenthub \
    postgres:16-alpine >/dev/null
  POSTGRES_STARTED=1
  wait_postgres
  DATABASE_URL="postgres://agenthub:agenthub@127.0.0.1:${POSTGRES_PORT}/agenthub?sslmode=disable"
else
  log "using provided PostgreSQL URL"
fi

log "starting agent-pod at ${AGENT_POD_URL}"
(
  cd "${ROOT_DIR}"
  AGENT_POD_ADDR=":${AGENT_POD_PORT}" \
  WORKSPACE_ID="${WORKSPACE_ID}" \
  WORKSPACE_DIR="${WORKSPACE_DIR}" \
  AGENTHUB_INTERNAL_TOKEN="${INTERNAL_TOKEN}" \
  AGENTHUB_RUNTIME_COMMAND="node ${ROOT_DIR}/runtimes/ts-runtime-host/dist/main.js --adapter pi-coding-agent" \
  go run ./cmd/agent-pod >"${TMP_DIR}/agent-pod.log" 2>&1
) &
AGENT_POD_PID=$!
wait_http_ok "${AGENT_POD_URL}/health" "${AGENT_POD_PID}" "${TMP_DIR}/agent-pod.log"

log "starting control-plane at ${CONTROL_PLANE_URL}"
(
  cd "${ROOT_DIR}"
  AGENTHUB_CONTROL_PLANE_ADDR=":${CONTROL_PLANE_PORT}" \
  AGENTHUB_DATABASE_URL="${DATABASE_URL}" \
  AGENTHUB_AGENT_POD_BASE_URL_TEMPLATE="${AGENT_POD_URL}" \
  AGENTHUB_DEV_AGENT_POD_TOKEN="${INTERNAL_TOKEN}" \
  go run ./cmd/control-plane >"${TMP_DIR}/control-plane.log" 2>&1
) &
CONTROL_PLANE_PID=$!
wait_http_ok "${CONTROL_PLANE_URL}/health" "${CONTROL_PLANE_PID}" "${TMP_DIR}/control-plane.log"

log "configuring workspace skill and MCP definitions"
SKILL_PAYLOAD="$(python3 - <<'PY'
import json
print(json.dumps({
  "name": "AgentHub Golden Path",
  "description": "Smoke skill used to verify session materialization.",
  "files": [{
    "path": "SKILL.md",
    "content": "# AgentHub Golden Path\n\nWhen the user asks for the golden path marker, reply with AGENTHUB_GOLDEN_PATH_OK exactly.\n"
  }]
}))
PY
)"
http_json PUT "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/skills/agenthub-golden-path" "${SKILL_PAYLOAD}" >/dev/null
MCP_PAYLOAD='{"command":"node","args":["-e","process.exit(0)"],"transport":"stdio","env":{"AGENTHUB_SMOKE":"1"}}'
http_json PUT "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/mcp-servers/golden-path-dummy" "${MCP_PAYLOAD}" >/dev/null

log "configuring workspace LLM connection and model"
CONNECTION_PAYLOAD="$(LLM_PROVIDER="${LLM_PROVIDER}" LLM_API_PROTOCOL="${LLM_API_PROTOCOL}" LLM_BASE_URL="${LLM_BASE_URL}" LLM_API_KEY="${LLM_API_KEY}" python3 - <<'PY'
import json, os
print(json.dumps({
  "provider": os.environ.get("LLM_PROVIDER", "anthropic"),
  "apiProtocol": os.environ.get("LLM_API_PROTOCOL", "anthropic-messages"),
  "baseUrl": os.environ["LLM_BASE_URL"],
  "apiKey": os.environ["LLM_API_KEY"],
}))
PY
)"
http_json PUT "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/llm-connection" "${CONNECTION_PAYLOAD}" >/dev/null

if [[ "${REFRESH_MODELS}" == "1" ]]; then
  log "refreshing remote model list"
  if ! http_json POST "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/llm-models:refresh" '{}' >"${TMP_DIR}/models-refresh.json"; then
    fail "model refresh failed; set AGENTHUB_SMOKE_REFRESH_MODELS=0 to skip remote model listing"
  fi
fi
MODEL_PAYLOAD="$(MODEL_ID="${MODEL_ID}" python3 - <<'PY'
import json, os
print(json.dumps({"modelId": os.environ["MODEL_ID"], "enabled": True}))
PY
)"
http_json PUT "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/llm-models" "${MODEL_PAYLOAD}" >/dev/null

log "creating session and first turn"
SESSION_PAYLOAD="$(MODEL_ID="${MODEL_ID}" MARKER="${MARKER}" python3 - <<'PY'
import json, os
marker=os.environ["MARKER"]
model=os.environ["MODEL_ID"]
print(json.dumps({
  "modelId": model,
  "firstTurn": {"message": f"Use the agenthub-golden-path skill if available. Reply with exactly {marker} and no other words."}
}))
PY
)"
CREATE_RESPONSE="$(http_json POST "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/sessions" "${SESSION_PAYLOAD}")"
printf '%s\n' "${CREATE_RESPONSE}" >"${TMP_DIR}/create-session.json"
SESSION_ID="$(printf '%s' "${CREATE_RESPONSE}" | json_get 'data.get("session", {}).get("sessionId", "")')"
TASK_ID="$(printf '%s' "${CREATE_RESPONSE}" | json_get 'data.get("task", {}).get("taskId", "")')"
RUN_ID_CREATED="$(printf '%s' "${CREATE_RESPONSE}" | json_get 'data.get("run", {}).get("runId", "")')"
[[ -n "${SESSION_ID}" && -n "${TASK_ID}" && -n "${RUN_ID_CREATED}" ]] || fail "create session response missing task/session/run ids: ${CREATE_RESPONSE}"

SKILL_FILE="${WORKSPACE_DIR}/tasks/${TASK_ID}/sessions/${SESSION_ID}/.agents/skills/agenthub-golden-path/SKILL.md"
CLAUDE_SKILL_FILE="${WORKSPACE_DIR}/tasks/${TASK_ID}/sessions/${SESSION_ID}/.claude/skills/agenthub-golden-path/SKILL.md"
MCP_FILE="${WORKSPACE_DIR}/tasks/${TASK_ID}/sessions/${SESSION_ID}/.agents/mcp.json"
[[ -f "${SKILL_FILE}" ]] || fail "skill was not materialized at ${SKILL_FILE}"
[[ -f "${CLAUDE_SKILL_FILE}" ]] || fail "Claude skill mirror was not materialized at ${CLAUDE_SKILL_FILE}"
[[ -f "${MCP_FILE}" ]] || fail "MCP config was not materialized at ${MCP_FILE}"
python3 - <<PY
import json, pathlib
mcp=json.loads(pathlib.Path("${MCP_FILE}").read_text())
assert "golden-path-dummy" in mcp.get("mcpServers", {}), mcp
PY

log "polling session state until run completes"
STATE_FILE="${TMP_DIR}/state.json"
EVENTS_FILE="${TMP_DIR}/events.json"
MESSAGES_FILE="${TMP_DIR}/messages.json"
deadline=$((SECONDS + TIMEOUT_SECONDS))
while true; do
  http_json GET "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/state" >"${STATE_FILE}"
  ACTIVE_RUN_STATUS="$(json_get 'data.get("activeRun", {}).get("status", "")' <"${STATE_FILE}")"
  if [[ -z "${ACTIVE_RUN_STATUS}" ]]; then
    break
  fi
  if [[ "${ACTIVE_RUN_STATUS}" == "failed" || "${ACTIVE_RUN_STATUS}" == "cancelled" ]]; then
    cat "${STATE_FILE}" >&2
    fail "run ended with status ${ACTIVE_RUN_STATUS}"
  fi
  if (( SECONDS >= deadline )); then
    cat "${STATE_FILE}" >&2
    fail "timeout waiting for session ${SESSION_ID} run completion"
  fi
  sleep 2
done

http_json GET "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/events?after=0" >"${EVENTS_FILE}"
http_json GET "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/messages" >"${MESSAGES_FILE}"

MARKER="${MARKER}" STATE_FILE="${STATE_FILE}" EVENTS_FILE="${EVENTS_FILE}" MESSAGES_FILE="${MESSAGES_FILE}" python3 - <<'PY'
import json, os, pathlib
marker = os.environ["MARKER"]
state = json.loads(pathlib.Path(os.environ["STATE_FILE"]).read_text())
events = json.loads(pathlib.Path(os.environ["EVENTS_FILE"]).read_text())
messages = json.loads(pathlib.Path(os.environ["MESSAGES_FILE"]).read_text())
if state.get("activeRun") is not None:
    raise SystemExit("activeRun still present")
if int(state.get("latestEventId") or 0) <= 0:
    raise SystemExit("latestEventId was not advanced")
event_items = events.get("events") or []
if not event_items:
    raise SystemExit("event replay returned no events")
if not any(item.get("type") == "item.completed" for item in event_items):
    raise SystemExit("event replay has no item.completed")
text = json.dumps(messages, ensure_ascii=False)
if marker not in text:
    raise SystemExit(f"marker {marker!r} not found in messages projection")
PY

log "PASS workspace=${WORKSPACE_ID} task=${TASK_ID} session=${SESSION_ID} run=${RUN_ID_CREATED} latestEventId=$(json_get 'data.get("latestEventId", "")' <"${STATE_FILE}")"
