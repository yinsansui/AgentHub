# 第一阶段开发切片

当前已落下第一条可编译主链骨架：

1. `cmd/control-plane`：control-plane 可执行入口，只做 wire-up。
2. `internal/controlplane`：AgentHub control-plane 核心。
3. `internal/driver/docker`：通过 Docker Engine API 创建、启动、停止、查看 agent-pod container。
4. `internal/controlplane/AgentPodClient`：通过 Docker network + container name 调用 pod 内部 `/turn`。
5. `cmd/agent-pod`：agent-pod 可执行入口，只做 wire-up。
6. `internal/agentpod`：容器内 AgentPodServer。
7. `internal/runtime/process` + `runtimes/ts-runtime-host`：通用进程型 runtime-host；首个 adapter 通过 npm package `@mariozechner/pi-coding-agent` 接入真实 pi-coding-agent runtime。旧的 Go 侧 CLI/JSONL fallback 已删除。
8. `pkg/protocol`：AgentHub `UniversalEvent` 与 `TurnRequest`。
9. `pkg/sse`：SSE 读写工具。
10. `internal/controlplane/EventStore`：PostgreSQL-backed `session_events` event log + `messages` / `message_blocks` blocks-first projection + `tasks` / `sessions` / `session_runs` run lifecycle；`session_events.id` 是全局递增 replay cursor，`message.completed.content` 是最终 blocks 来源，未配置数据库时仅使用内存开发 store。

## 本地验证

```bash
go test ./...
cd runtimes/ts-runtime-host && npm ci && npm run build
make images
```

## Real Runtime Golden Path

真实 runtime 主链路用 `scripts/smoke/real-runtime-golden-path.sh` 验收。它默认启动临时 PostgreSQL container，并用本地进程启动 control-plane 与 AgentPod：

```bash
export AGENTHUB_SMOKE_LLM_BASE_URL=http://example.local:8084
export AGENTHUB_SMOKE_LLM_API_KEY=...
export AGENTHUB_SMOKE_LLM_MODEL_ID=k2p5
make smoke-real-runtime
```

脚本覆盖以下合同：

1. PostgreSQL-backed control-plane 可启动并通过 `/health`。
2. workspace skill 可通过 API 写入，并在新 session cwd 下物理化到 `.agents/skills/<slug>/` 和 `.claude/skills/<slug>/`。
3. workspace MCP server 可通过 API 写入，并在新 session cwd 下物理化到 `.agents/mcp.json`。
4. workspace LLM connection 与 enabled model 可通过 control-plane API 配置。
5. `POST /workspaces/{workspaceId}/sessions` 创建 task + session + first turn，并锁定 `session.modelId`。
6. AgentPod 启动 `ts-runtime-host` / `pi-coding-agent`，使用 control-plane 注入的 LLM env 调真实模型。
7. run 完成后，`GET /sessions/{sessionId}/state` 没有 active run，`latestEventId` 前进。
8. `GET /sessions/{sessionId}/events?after=0` 可 replay，并包含 `run.started`、`message.completed`、`run.completed`。
9. `GET /sessions/{sessionId}/messages` 可读取 message projection，并包含 smoke marker。

常用参数：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `AGENTHUB_SMOKE_LLM_BASE_URL` | 无 | 必填，Anthropic-compatible endpoint |
| `AGENTHUB_SMOKE_LLM_API_KEY` | 无 | 必填，LLM API key |
| `AGENTHUB_SMOKE_LLM_MODEL_ID` | `k2p5` | 创建 session 时锁定的 model |
| `AGENTHUB_SMOKE_REFRESH_MODELS` | `1` | 是否先调用 `/llm-models:refresh` |
| `AGENTHUB_SMOKE_DATABASE_URL` | 无 | 指定后复用已有 Postgres，不启动临时 container |
| `AGENTHUB_SMOKE_KEEP_ARTIFACTS` | `0` | 失败时总是保留日志；成功时设为 `1` 可保留 `.agenthub/smoke/<run>` |
| `AGENTHUB_SMOKE_SKIP_BUILD` | `0` | 设为 `1` 时跳过 `ts-runtime-host` build |
| `AGENTHUB_RUN_TIMEOUT` | `30m` | control-plane 全局 run timeout |

## Cancel Lifecycle Smoke

取消语义用 `scripts/smoke/cancel-lifecycle.sh` 验收。它使用本地 fake runtime 挂起 run，覆盖：

1. 错误 `expectedRunId` 返回 conflict，且不改变当前 active run。
2. 正确 `expectedRunId` 写入 `run.cancelling` 并转发 AgentPod cancel。
3. repeated cancel 不重复写 `run.cancelling`。
4. runtime abort 后写入 `run.cancelled`，`GET /state` 的 `activeRun` 清空。

```bash
make smoke-cancel-lifecycle
```

如果 control-plane 运行在宿主机而不是 Docker network 内，默认的 `http://agent-pod-{workspaceId}:3001` 不能被宿主机 DNS 解析。此时可以先用本地 agent-pod 进程验证 SSE 链路：

```bash
AGENTHUB_INTERNAL_TOKEN=dev-token WORKSPACE_ID=ws_dev go run ./cmd/agent-pod
```

对接 Anthropic-compatible 真实模型时，先构建 TS runtime host。AgentPod 本地进程只需要 runtime-host 启动命令；LLM endpoint、API key 和可用 model 通过 control-plane API 存储到 DB / 内存 store：

```bash
cd runtimes/ts-runtime-host && npm ci && npm run build && cd ../..

AGENTHUB_RUNTIME_COMMAND="node $(pwd)/runtimes/ts-runtime-host/dist/main.js --adapter pi-coding-agent" \
AGENTHUB_INTERNAL_TOKEN=dev-token \
WORKSPACE_ID=ws_dev \
go run ./cmd/agent-pod
```

另一个终端：

```bash
AGENTHUB_AGENT_POD_BASE_URL_TEMPLATE=http://127.0.0.1:3001 \
AGENTHUB_DEV_AGENT_POD_TOKEN=dev-token \
go run ./cmd/control-plane

curl -sS -X PUT http://127.0.0.1:3000/workspaces/ws_dev/llm-connection \
  -H 'content-type: application/json' \
  -d '{"provider":"anthropic","apiProtocol":"anthropic-messages","baseUrl":"http://example.local:8084","apiKey":"..."}'

curl -sS -X PUT http://127.0.0.1:3000/workspaces/ws_dev/llm-models \
  -H 'content-type: application/json' \
  -d '{"modelId":"k2p5","enabled":true}'

curl -sS -X POST http://127.0.0.1:3000/workspaces/ws_dev/sessions \
  -H 'content-type: application/json' \
  -d '{"modelId":"k2p5","firstTurn":{"message":"hello"}}'
```

真正使用 Docker container 时，control-plane 应与 agent-pod 在同一个 Docker network 中。当前 agent-pod 镜像基于 `node:22-slim`，同时包含静态 Go `agent-pod` binary 和构建后的 `ts-runtime-host`。

## 下一步

1. 为 run lifecycle 增加超时、重试与更完整的观测指标。
2. 将 repo 绑定沉到插件扩展点，避免进入平台核心模型。
3. 扩展 MCP bridge 的 transport / 鉴权 / 观测能力。


## Docker 端到端验证

```bash
make images

docker network create agenthub || true
docker run -d --name agenthub-postgres --network agenthub \
  -e POSTGRES_USER=agenthub \
  -e POSTGRES_PASSWORD=agenthub \
  -e POSTGRES_DB=agenthub \
  postgres:16-alpine
docker run -d --name agenthub-control-plane --network agenthub \
  -p 3000:3000 \
  -e AGENTHUB_DATABASE_URL=postgres://agenthub:agenthub@agenthub-postgres:5432/agenthub?sslmode=disable \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD/.agenthub:/data" \
  agenthub-control-plane:dev

# 创建 session 时后端会自动启动 workspace agent-pod，无需显式调用 /start。
SESSION_ID=$(curl -sS -X POST http://127.0.0.1:3000/workspaces/ws_dev/sessions \
  -H 'content-type: application/json' \
  -d '{"firstTurn":{"message":"hello"}}' | jq -r '.session.sessionId')

# 在已有 session 中追加一轮 turn
curl -sS -X POST "http://127.0.0.1:3000/sessions/${SESSION_ID}/turns" \
  -H 'content-type: application/json' \
  -d '{"message":"continue"}'

# 查询当前消息快照
curl "http://127.0.0.1:3000/sessions/${SESSION_ID}/messages"

# 用全局 event id 补洞 / replay
curl "http://127.0.0.1:3000/sessions/${SESSION_ID}/events?after=0"

# live reconnect：浏览器 EventSource 会自动带 Last-Event-ID
curl -N "http://127.0.0.1:3000/sessions/${SESSION_ID}/stream"

# 查询消息快照 + active run + 最新 event cursor
curl "http://127.0.0.1:3000/sessions/${SESSION_ID}/state"

# 中断当前响应；expectedRunId 必须等于 state.activeRun.runId
curl -X POST "http://127.0.0.1:3000/sessions/${SESSION_ID}/interrupt" \
  -H 'content-type: application/json' \
  -d '{"expectedRunId":"<state.activeRun.runId>","reason":"user_stop"}'
```

清理：

```bash
docker rm -f agenthub-control-plane agent-pod-ws_dev agenthub-postgres
```
