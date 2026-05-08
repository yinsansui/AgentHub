# AgentHub

[English](README.md) | [简体中文](README.zh-CN.md)

AgentHub 是一个 agent runtime 控制面原型。它适合本地开发、架构探索，以及验证基于 Docker 的 `agent-pod` runtime 链路。它还不是生产可用系统。

## 概览

AgentHub 把面向用户的控制面和实际 agent runtime 分开。当前原型包含 Go `control-plane`、Go `agent-pod`、TypeScript runtime host，以及已经实现的 React 用户工作台。

第一条 runtime 链路如下：

```text
user-portal
  -> control-plane
  -> DockerAgentPodDriver
  -> agent-pod
  -> AgentPodServer
  -> ts-runtime-host
  -> pi-coding-agent adapter
  -> UniversalEvent SSE
  -> PostgreSQL session event log and message projections
```

## 当前能力

- 面向用户 API 使用基于 cookie 的原型登录，本地内置账号为 `admin` / `admin`。
- 支持 workspace 列表，以及为当前 owner 自动创建默认 workspace。
- 支持创建 session、追加 turn、实时 SSE stream、持久事件 replay、消息快照和 run interrupt。
- 设置 `AGENTHUB_DATABASE_URL` 时使用 PostgreSQL 存储，未设置时使用内存开发存储。
- 支持 workspace 级 LLM connection 和 model 设置，并在每次 turn 时注入 runtime host 进程。
- `runtimes/ts-runtime-host` 通过 `pi-coding-agent` adapter 接入 `@mariozechner/pi-coding-agent`。
- `web/user-portal` 下的 React 用户工作台可用于本地交互。

## 架构

Go 服务把可执行入口保持在 `cmd/*`，业务逻辑放在 `internal/*`，可复用的 protocol 和 SSE helper 放在 `pkg/*`。

`control-plane` 负责 workspace、session、task、run、event、message、skill、MCP 和 LLM connection 状态。`agent-pod` 负责本地 runtime 边界，并运行配置好的 runtime command。`runtimes/ts-runtime-host` 把 AgentHub runtime request 映射到第一版 TypeScript adapter。

更完整的设计说明见 [docs/architecture.md](docs/architecture.md)。

## 前置条件

- Go 1.26，版本来自 `go.mod`。
- Node.js 和 npm，用于 `runtimes/ts-runtime-host` 和 `web/user-portal`。
- Docker，用于本地 PostgreSQL 和镜像构建。
- `curl`，本地启动脚本会用它做 readiness check。
- 未设置 `AGENTHUB_DATABASE_URL` 时，本地脚本会启动 PostgreSQL。默认本地示例使用用户 `agenthub`、密码 `agenthub`、数据库 `agenthub`、端口 `5432`。

## 快速开始

启动本地开发栈：

```bash
./scripts/start-dev.sh
```

默认情况下，脚本会构建 TypeScript runtime host 和 Go binary，使用 Docker 启动本地 PostgreSQL，然后启动：

```text
ControlPlane: http://127.0.0.1:3000
AgentPod:     http://127.0.0.1:3001
UserPortal:   http://127.0.0.1:5174
Login:        admin / admin
```

打开 `http://127.0.0.1:5174`，使用 `admin` / `admin` 登录。这组账号只用于本地原型。

在启动终端按 `Ctrl+C` 会停止服务。除非设置了 `AGENTHUB_KEEP_POSTGRES=1`，脚本也会停止它启动的 PostgreSQL container。

## 手动运行与验证

运行主要本地检查：

```bash
make check
```

这个命令会运行 `go test ./...`，然后用 npm 安装并构建 `runtimes/ts-runtime-host`。

构建本地 Docker 镜像：

```bash
make images
```

在不需要真实 LLM 的情况下验证 run cancel 生命周期：

```bash
make smoke-cancel-lifecycle
```

只有当你有本地 Anthropic 兼容 endpoint 和一次性 API key 时，才运行真实 runtime smoke：

```bash
export AGENTHUB_SMOKE_LLM_BASE_URL=http://example.local:8084
export AGENTHUB_SMOKE_LLM_API_KEY=replace-with-local-test-key
export AGENTHUB_SMOKE_LLM_MODEL_ID=k2p5
make smoke-real-runtime
```

真实 runtime smoke 脚本会启动 PostgreSQL、`control-plane` 和本地 `agent-pod`，配置 workspace skill、MCP 和 LLM 定义，让真实 LLM 调用本地 stdio MCP tool，然后检查 `/state`、`/messages` 和 `/events?after=0`。

## 配置

常用本地环境变量：

- `AGENTHUB_CONTROL_PLANE_PORT`，默认 `3000`。
- `AGENTHUB_AGENT_POD_PORT`，默认 `3001`。
- `AGENTHUB_USER_PORTAL_PORT`，默认 `5174`。
- `AGENTHUB_POSTGRES_PORT`，默认 `5432`。
- `AGENTHUB_DATABASE_URL`，默认空。为空时，`scripts/start-dev.sh` 会启动 Docker PostgreSQL，并使用 `postgres://agenthub:agenthub@127.0.0.1:5432/agenthub?sslmode=disable`。
- `AGENTHUB_INTERNAL_TOKEN`，默认 `dev-token`，用于本地 control plane 和 AgentPod 通信。
- `AGENTHUB_RUNTIME_COMMAND`，AgentPod 用它启动 runtime host，例如 `node $(pwd)/runtimes/ts-runtime-host/dist/main.js --adapter pi-coding-agent`。
- `AGENTHUB_USER_PORTAL_PROXY_TARGET`，Vite user portal 用它把 API 请求代理到其他 control plane。

Workspace LLM connection 设置通过 control plane API 管理，不需要手工编辑 runtime host 环境变量。当前相关 API 包括：

```text
PUT /workspaces/{workspaceId}/llm-connection
GET /workspaces/{workspaceId}/llm-connection
POST /workspaces/{workspaceId}/llm-models:refresh
PUT /workspaces/{workspaceId}/llm-models
GET /workspaces/{workspaceId}/llm-models
```

## 仓库结构

```text
cmd/control-plane          Go control-plane executable entrypoint
cmd/agent-pod              Go AgentPod executable entrypoint
internal/controlplane      control-plane core
internal/agentpod          AgentPod core
internal/driver            container driver interface
internal/driver/docker     Docker Engine API driver
internal/runtime           runtime interface
internal/runtime/process   process backed runtime host adapter
pkg/protocol               UniversalEvent and turn contracts
pkg/sse                    SSE transport helpers
runtimes/ts-runtime-host   TypeScript runtime host and pi-coding-agent adapter
web/user-portal            已实现的 React 用户工作台
web/admin-console          计划中的管理控制台
deploy                     Dockerfiles and deployment assets
docs                       design and development notes
```

## 安全与原型说明

- AgentHub 仍是原型。不要把当前本地栈暴露到不可信网络。
- `admin` / `admin`、`agenthub` / `agenthub` 和 `dev-token` 都只是本地开发示例，不是真实密钥。
- smoke LLM API key 示例应使用一次性本地测试值。
- Runtime secret 会通过 control plane API 和 runtime 子进程环境传递。不要把真实凭据写进 shell history、文档或提交文件。
- 当前仓库约定说明项目尚未上线，因此不保留旧本地数据库结构兼容性。

## 更多文档

- [docs/architecture.md](docs/architecture.md) 说明架构模型、核心概念、事件模型和当前约束。
- [web/user-portal/README.md](web/user-portal/README.md) 说明已实现的用户工作台。
