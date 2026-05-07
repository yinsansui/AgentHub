#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONTROL_PLANE_PORT="${AGENTHUB_CANCEL_SMOKE_CONTROL_PLANE_PORT:-3331}"
AGENT_POD_PORT="${AGENTHUB_CANCEL_SMOKE_AGENT_POD_PORT:-3330}"
POSTGRES_PORT="${AGENTHUB_CANCEL_SMOKE_POSTGRES_PORT:-55434}"
TIMEOUT_SECONDS="${AGENTHUB_CANCEL_SMOKE_TIMEOUT_SECONDS:-60}"
KEEP_ARTIFACTS="${AGENTHUB_CANCEL_SMOKE_KEEP_ARTIFACTS:-0}"
RUN_TIMEOUT="${AGENTHUB_CANCEL_SMOKE_RUN_TIMEOUT:-30s}"
CONTROL_PLANE_URL="http://127.0.0.1:${CONTROL_PLANE_PORT}"
AGENT_POD_URL="http://127.0.0.1:${AGENT_POD_PORT}"
INTERNAL_TOKEN="${AGENTHUB_CANCEL_SMOKE_INTERNAL_TOKEN:-dev-token}"
RUN_ID="$(date -u +%Y%m%d%H%M%S)-$$"
WORKSPACE_NAME="${AGENTHUB_CANCEL_SMOKE_WORKSPACE_NAME:-Cancel Lifecycle Smoke ${RUN_ID}}"
WORKSPACE_ID=""
TMP_DIR="${AGENTHUB_CANCEL_SMOKE_TMP_DIR:-${ROOT_DIR}/.agenthub/smoke-cancel/${RUN_ID}}"
WORKSPACE_DIR="${TMP_DIR}/workspace"
COOKIE_JAR="${TMP_DIR}/cookies.txt"
POSTGRES_CONTAINER="${AGENTHUB_CANCEL_SMOKE_POSTGRES_CONTAINER:-agenthub-cancel-smoke-postgres-${RUN_ID}}"
DATABASE_URL="${AGENTHUB_CANCEL_SMOKE_DATABASE_URL:-}"
POSTGRES_STARTED=0
CONTROL_PLANE_PID=""
AGENT_POD_PID=""

log() { printf '[agenthub-cancel-smoke] %s\n' "$*" >&2; }
fail() { printf '[agenthub-cancel-smoke] ERROR: %s\n' "$*" >&2; exit 1; }
require_cmd() { command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"; }

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
    status="$(curl -sS -b "${COOKIE_JAR}" -c "${COOKIE_JAR}" -o "${output}" -w '%{http_code}' -X "${method}" "${url}" -H 'content-type: application/json' --data-binary "${payload}")"
  else
    status="$(curl -sS -b "${COOKIE_JAR}" -c "${COOKIE_JAR}" -o "${output}" -w '%{http_code}' -X "${method}" "${url}")"
  fi
  body="$(cat "${output}")"
  rm -f "${output}"
  if [[ "${status}" -lt 200 || "${status}" -ge 300 ]]; then
    fail "${method} ${url} returned ${status}: ${body}"
  fi
  printf '%s' "${body}"
}

http_json_status() {
  local method="$1"
  local url="$2"
  local payload="${3:-}"
  local body_file="$4"
  local status
  status="$(curl -sS -b "${COOKIE_JAR}" -c "${COOKIE_JAR}" -o "${body_file}" -w '%{http_code}' -X "${method}" "${url}" -H 'content-type: application/json' --data-binary "${payload}")"
  printf '%s' "${status}"
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

workspace_payload() {
  WORKSPACE_NAME="$1" python3 - <<'PYJSON'
import json, os
print(json.dumps({"name": os.environ["WORKSPACE_NAME"]}, ensure_ascii=False))
PYJSON
}

acquire_workspace() {
  local name="$1"
  local create_response workspace_id
  log "listing workspaces to initialize the default workspace"
  http_json GET "${CONTROL_PLANE_URL}/workspaces?limit=100&offset=0" >"${TMP_DIR}/workspaces-before.json"
  log "creating smoke workspace '${name}'"
  create_response="$(http_json POST "${CONTROL_PLANE_URL}/workspaces" "$(workspace_payload "${name}")")"
  printf '%s\n' "${create_response}" >"${TMP_DIR}/workspace.json"
  workspace_id="$(printf '%s' "${create_response}" | json_get 'data.get("workspace", {}).get("id", "")')"
  [[ -n "${workspace_id}" ]] || fail "create workspace response missing id: ${create_response}"
  [[ "${workspace_id}" =~ ^[0-9a-fA-F-]{36}$ ]] || fail "workspace id is not a UUID: ${workspace_id}"
  printf '%s' "${workspace_id}"
}

require_cmd curl
require_cmd docker
require_cmd go
require_cmd node
require_cmd python3
mkdir -p "${TMP_DIR}" "${WORKSPACE_DIR}"

cat >"${TMP_DIR}/cancel-runtime.mjs" <<'NODE'
import { createInterface } from "node:readline";

const active = new Set();
const rl = createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on("line", (line) => {
  if (!line.trim()) return;
  const command = JSON.parse(line);
  if (command.type === "run") {
    active.add(command.runId);
    emit({ type: "session.started", timestamp: new Date().toISOString(), workspaceId: command.workspaceId, taskId: command.taskId, sessionId: command.sessionId, runId: command.runId });
    emit({ type: "message.started", timestamp: new Date().toISOString(), workspaceId: command.workspaceId, taskId: command.taskId, sessionId: command.sessionId, runId: command.runId, messageId: `msg_${command.runId}_0`, role: "assistant" });
    emit({ type: "text.started", timestamp: new Date().toISOString(), workspaceId: command.workspaceId, taskId: command.taskId, sessionId: command.sessionId, runId: command.runId, messageId: `msg_${command.runId}_0`, contentIndex: 0, role: "assistant" });
    emit({ type: "text.delta", timestamp: new Date().toISOString(), workspaceId: command.workspaceId, taskId: command.taskId, sessionId: command.sessionId, runId: command.runId, messageId: `msg_${command.runId}_0`, contentIndex: 0, role: "assistant", delta: "partial", partial: "partial" });
  } else if (command.type === "abort" && active.has(command.runId)) {
    active.delete(command.runId);
    setTimeout(() => {
      emit({ type: "session.ended", timestamp: new Date().toISOString(), runId: command.runId, metadata: { reason: "aborted", adapter: "cancel-smoke" } });
    }, 1200);
  }
});
setInterval(() => {}, 1000);
function emit(event) {
  process.stdout.write(`${JSON.stringify(event)}\n`);
}
NODE

if [[ -z "${DATABASE_URL}" ]]; then
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
  WORKSPACE_DIR="${WORKSPACE_DIR}" \
  AGENTHUB_INTERNAL_TOKEN="${INTERNAL_TOKEN}" \
  AGENTHUB_RUNTIME_COMMAND="node ${TMP_DIR}/cancel-runtime.mjs" \
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
  AGENTHUB_RUN_TIMEOUT="${RUN_TIMEOUT}" \
  go run ./cmd/control-plane >"${TMP_DIR}/control-plane.log" 2>&1
) &
CONTROL_PLANE_PID=$!
wait_http_ok "${CONTROL_PLANE_URL}/health" "${CONTROL_PLANE_PID}" "${TMP_DIR}/control-plane.log"

log "logging in to control-plane as admin"
http_json POST "${CONTROL_PLANE_URL}/auth/login" '{"username":"admin","password":"admin"}' >/dev/null
WORKSPACE_ID="$(acquire_workspace "${WORKSPACE_NAME}")"
log "using workspace ${WORKSPACE_ID} (${WORKSPACE_NAME})"

log "configuring dummy LLM connection and model"
http_json PUT "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/llm-connection" '{"provider":"anthropic","apiProtocol":"anthropic-messages","baseUrl":"http://unused.local","apiKey":"dummy"}' >/dev/null
http_json PUT "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/llm-models" '{"modelId":"dummy","enabled":true}' >/dev/null

log "creating session with hanging first turn"
CREATE_RESPONSE="$(http_json POST "${CONTROL_PLANE_URL}/workspaces/${WORKSPACE_ID}/sessions" '{"modelId":"dummy","firstTurn":{"message":"hang until cancelled"}}')"
printf '%s\n' "${CREATE_RESPONSE}" >"${TMP_DIR}/create-session.json"
SESSION_ID="$(printf '%s' "${CREATE_RESPONSE}" | json_get 'data.get("session", {}).get("sessionId", "")')"
RUN_ID_CREATED="$(printf '%s' "${CREATE_RESPONSE}" | json_get 'data.get("run", {}).get("runId", "")')"
[[ -n "${SESSION_ID}" && -n "${RUN_ID_CREATED}" ]] || fail "create session response missing session/run: ${CREATE_RESPONSE}"

STATE_FILE="${TMP_DIR}/state.json"
EVENTS_FILE="${TMP_DIR}/events.json"
WRONG_FILE="${TMP_DIR}/wrong-cancel.json"
REPEAT_FILE="${TMP_DIR}/repeat-cancel.json"

log "waiting for active run"
deadline=$((SECONDS + TIMEOUT_SECONDS))
while true; do
  http_json GET "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/state" >"${STATE_FILE}"
  ACTIVE_RUN_ID="$(json_get 'data.get("activeRun", {}).get("runId", "")' <"${STATE_FILE}")"
  ACTIVE_RUN_STATUS="$(json_get 'data.get("activeRun", {}).get("status", "")' <"${STATE_FILE}")"
  if [[ "${ACTIVE_RUN_ID}" == "${RUN_ID_CREATED}" && "${ACTIVE_RUN_STATUS}" == "running" ]]; then
    break
  fi
  if (( SECONDS >= deadline )); then
    cat "${STATE_FILE}" >&2
    fail "timeout waiting for active running run"
  fi
  sleep 1
 done

log "verifying wrong expectedRunId is fenced"
WRONG_STATUS="$(http_json_status POST "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/interrupt" '{"expectedRunId":"run_wrong","reason":"wrong_run"}' "${WRONG_FILE}")"
[[ "${WRONG_STATUS}" == "409" ]] || fail "wrong expectedRunId returned ${WRONG_STATUS}: $(cat "${WRONG_FILE}")"
python3 - <<PY
import json, pathlib
body=json.loads(pathlib.Path("${WRONG_FILE}").read_text())
assert body.get("reason") == "run_mismatch", body
assert body.get("activeRun", {}).get("runId") == "${RUN_ID_CREATED}", body
PY
http_json GET "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/state" >"${STATE_FILE}"
[[ "$(json_get 'data.get("activeRun", {}).get("runId", "")' <"${STATE_FILE}")" == "${RUN_ID_CREATED}" ]] || fail "wrong cancel changed active run"

log "requesting cancel for active run"
CANCEL_RESPONSE="$(http_json POST "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/interrupt" "{\"expectedRunId\":\"${RUN_ID_CREATED}\",\"reason\":\"user_stop\"}")"
printf '%s\n' "${CANCEL_RESPONSE}" >"${TMP_DIR}/cancel.json"
python3 - <<PY
import json, pathlib
body=json.loads(pathlib.Path("${TMP_DIR}/cancel.json").read_text())
assert body.get("interrupted") is True, body
assert body.get("cancelDelivered") is True, body
assert body.get("run", {}).get("status") == "cancelling", body
PY

log "verifying repeated cancel does not append another run.cancelling"
REPEAT_STATUS="$(http_json_status POST "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/interrupt" "{\"expectedRunId\":\"${RUN_ID_CREATED}\",\"reason\":\"user_stop_again\"}" "${REPEAT_FILE}")"
[[ "${REPEAT_STATUS}" == "200" ]] || fail "repeated cancel returned ${REPEAT_STATUS}: $(cat "${REPEAT_FILE}")"
python3 - <<PY
import json, pathlib
body=json.loads(pathlib.Path("${REPEAT_FILE}").read_text())
assert body.get("interrupted") is True, body
assert body.get("run", {}).get("status") == "cancelling", body
PY

log "waiting for cancelled terminal state"
deadline=$((SECONDS + TIMEOUT_SECONDS))
while true; do
  http_json GET "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/state" >"${STATE_FILE}"
  if [[ -z "$(json_get 'data.get("activeRun", {}).get("runId", "")' <"${STATE_FILE}")" ]]; then
    break
  fi
  if (( SECONDS >= deadline )); then
    cat "${STATE_FILE}" >&2
    fail "timeout waiting for active run to clear"
  fi
  sleep 1
 done

http_json GET "${CONTROL_PLANE_URL}/sessions/${SESSION_ID}/events?after=0" >"${EVENTS_FILE}"
RUN_ID_CREATED="${RUN_ID_CREATED}" STATE_FILE="${STATE_FILE}" EVENTS_FILE="${EVENTS_FILE}" python3 - <<'PY'
import json, os, pathlib
state=json.loads(pathlib.Path(os.environ["STATE_FILE"]).read_text())
events=json.loads(pathlib.Path(os.environ["EVENTS_FILE"]).read_text()).get("events") or []
types=[item.get("type") for item in events]
if state.get("activeRun") is not None:
    raise SystemExit(f"activeRun still present: {state}")
if types.count("run.cancelling") != 1:
    raise SystemExit(f"expected exactly one run.cancelling, got {types}")
if "run.cancelled" not in types:
    raise SystemExit(f"missing run.cancelled: {types}")
if "run.timed_out" in types:
    raise SystemExit(f"unexpected timeout: {types}")
for event in events:
    if event.get("type") == "run.cancelled":
        payload=event.get("payload") or {}
        assert payload.get("runId") == os.environ["RUN_ID_CREATED"], payload
        assert payload.get("metadata", {}).get("runStatus") == "cancelled", payload
        break
else:
    raise SystemExit("run.cancelled payload not found")
print("cancel lifecycle ok", types)
PY

log "PASS workspace=${WORKSPACE_ID} session=${SESSION_ID} run=${RUN_ID_CREATED} latestEventId=$(json_get 'data.get("latestEventId", "")' <"${STATE_FILE}")"
