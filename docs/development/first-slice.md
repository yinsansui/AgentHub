# 第一阶段开发切片

当前已落下第一条可编译主链骨架：

1. `cmd/control-plane`：control-plane 可执行入口，只做 wire-up。
2. `internal/controlplane`：AgentHub control-plane 核心。
3. `internal/driver/docker`：通过 Docker Engine API 创建、启动、停止、查看 agent-pod container。
4. `internal/controlplane/AgentPodClient`：通过 Docker network + container name 调用 pod 内部 `/turn`。
5. `cmd/agent-pod`：agent-pod 可执行入口，只做 wire-up。
6. `internal/agentpod`：容器内 AgentPodServer。
7. `internal/runtime/pi`：Pi Agent CLI/JSONL 适配边界；未配置 `PI_AGENT_COMMAND` 时使用 stub 输出。
8. `pkg/protocol`：AgentHub `UniversalEvent` 与 `TurnRequest`。
9. `pkg/sse`：SSE 读写工具。
10. `internal/controlplane/EventStore`：PostgreSQL-backed `session_events` event log + `messages` / `message_blocks` blocks-first projection + `tasks` / `sessions` / `session_runs` run lifecycle；`session_events.id` 是全局递增 replay cursor，`item.completed.item.content` 是最终 blocks 来源，未配置数据库时仅使用内存开发 store。

## 本地验证

```bash
go test ./...
make images
```

如果 control-plane 运行在宿主机而不是 Docker network 内，默认的 `http://agent-pod-{workspaceId}:3001` 不能被宿主机 DNS 解析。此时可以先用本地 agent-pod 进程验证 SSE 链路：

```bash
AGENTHUB_INTERNAL_TOKEN=dev-token WORKSPACE_ID=ws_dev go run ./cmd/agent-pod
```

另一个终端：

```bash
AGENTHUB_AGENT_POD_BASE_URL_TEMPLATE=http://127.0.0.1:3001 \
AGENTHUB_DEV_AGENT_POD_TOKEN=dev-token \
go run ./cmd/control-plane

curl -sS -X POST http://127.0.0.1:3000/workspaces/ws_dev/sessions \
  -H 'content-type: application/json' \
  -d '{"firstTurn":{"message":"hello"}}'
```

真正使用 Docker container 时，control-plane 应与 agent-pod 在同一个 Docker network 中。当前 Dockerfile 使用本机交叉编译出的静态 Go binary + `scratch` 镜像，避免首轮开发依赖 Docker Hub base image 拉取。

## 下一步

1. 将 `PiCLIAdapter` 从 CLI JSONL 接入升级为 Pi Agent SDK/JSON-RPC 接入。
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
