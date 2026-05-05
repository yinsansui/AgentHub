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
10. `internal/controlplane/EventStore`：PostgreSQL-backed `session_events` event log + `messages` / `message_blocks` blocks-first projection + `tasks` / `sessions` / `session_runs` run lifecycle；`session_events.id` 是全局递增 replay cursor，`item.completed.item.content` 是最终 blocks 来源，未配置数据库时仅使用内存开发 store。

## 本地验证

```bash
go test ./...
cd runtimes/ts-runtime-host && npm ci && npm run build
make images
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

1. 将 `.agents/mcp.json` 注册为真实 runtime tool。
2. 为 run lifecycle 增加超时、重试与更完整的观测指标。
3. 将 repo 绑定沉到插件扩展点，避免进入平台核心模型。


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

curl -sS -X POST http://127.0.0.1:3000/workspaces/ws_dev/start -d '{}'
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
