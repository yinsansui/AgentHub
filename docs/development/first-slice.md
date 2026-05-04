# 第一阶段开发切片

当前已落下第一条可编译主链骨架：

1. `cmd/control-plane`：AgentHub control-plane HTTP 服务。
2. `internal/controlplane/DockerAgentPodDriver`：通过 Docker Engine API 创建、启动、停止、查看 agent-pod container。
3. `internal/controlplane/AgentPodClient`：通过 Docker network + container name 调用 pod 内部 `/turn`。
4. `cmd/agent-pod`：容器内 AgentPodServer。
5. `internal/agentpod/PiCLIAdapter`：Pi Agent CLI/JSONL 适配边界；未配置 `PI_AGENT_COMMAND` 时使用 stub 输出。
6. `internal/protocol`：AgentHub `UniversalEvent` 与 `TurnRequest`。
7. `internal/controlplane/EventStore`：开发期 NDJSON event log，后续替换成 DB-backed event log。

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

curl -N -X POST http://127.0.0.1:3000/workspaces/ws_dev/turn \
  -H 'content-type: application/json' \
  -d '{"sessionId":"sess_dev","runId":"run_dev","message":"hello"}'
```

真正使用 Docker container 时，control-plane 应与 agent-pod 在同一个 Docker network 中。当前 Dockerfile 使用本机交叉编译出的静态 Go binary + `scratch` 镜像，避免首轮开发依赖 Docker Hub base image 拉取。

## 下一步

1. 将 `EventStore` 替换为 DB-backed `session_events + messages projection`。
2. 将 `PiCLIAdapter` 从 CLI JSONL 接入升级为 Pi Agent SDK/JSON-RPC 接入。
3. 为 running sink 增加真实 reconnect buffer。
4. 将 per-pod token 从内存迁移到 DB/runtime 表。


## Docker 端到端验证

```bash
make images

docker network create agenthub || true
docker run -d --name agenthub-control-plane --network agenthub   -p 3000:3000   -v /var/run/docker.sock:/var/run/docker.sock   -v "$PWD/.agenthub:/data"   agenthub-control-plane:dev

curl -sS -X POST http://127.0.0.1:3000/workspaces/ws_dev/start -d '{}'
curl -N -X POST http://127.0.0.1:3000/workspaces/ws_dev/turn   -H 'content-type: application/json'   -d '{"sessionId":"sess_dev","runId":"run_dev","message":"hello"}'
```

清理：

```bash
docker rm -f agenthub-control-plane agent-pod-ws_dev
```
