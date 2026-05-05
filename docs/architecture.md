# AgentHub 整体架构与核心定义

## 1. 文档目标

本文档用于沉淀 AgentHub 第一版的整体架构设计、模块职责、核心概念和当前约束。

当前目标不是一次性覆盖所有最终形态，而是先定义一套可以落地实现的基础模型，后续在此基础上逐步扩展。

---

## 2. 第一版整体目标

AgentHub 是一个用于承载和调度多种 Agent Runtime 的平台。

第一版聚焦以下能力：

1. 提供普通用户入口和管理入口
2. 提供统一的控制面来管理 task、workspace 和 runtime
3. 通过 Docker 容器提供长期存在的 workspace
4. 以 `agent-pod` 抽象承载封装后的 agent runtime
5. 第一阶段先通过 Docker container 接入 Pi Agent
6. 统一接收并持久化 `UniversalEvent` 事件流

---

## 3. 顶层模块划分

当前顶层模块定义如下：

```text
cmd/                         # 所有可执行入口；每个 main.go 只做 wire-up
  control-plane/main.go
  agent-pod/main.go

internal/                    # 不对外暴露的业务逻辑
  controlplane/              # 控制面核心
    server.go                # HTTP 路由
    workspace.go             # workspace 生命周期；后续从 server.go 拆出
    session.go               # session/turn 处理；后续从 server.go 拆出
    store.go                 # 事件持久化
  agentpod/                  # agent-pod 核心
    server.go                # HTTP 路由
    runner.go                # turn 执行、取消、重载；后续从 server.go 拆出
  driver/                    # 容器驱动，可替换
    driver.go                # interface Driver
    docker/                  # Docker Engine API 实现
      client.go
      driver.go
  runtime/                   # AI 运行时适配，可替换
    runtime.go               # interface Runtime
    pi/                      # Pi CLI / SDK 实现
      adapter.go

pkg/                         # 可对外复用的纯工具包
  protocol/                  # 事件协议定义
    events.go
  sse/                       # SSE 读写工具
    sse.go

web/                         # 前端，非 Go
  admin-console/
  user-portal/

deploy/                      # 部署相关
  Dockerfile.control-plane
  Dockerfile.agent-pod

docs/
  architecture.md
  development/
```

第一阶段先不做 user/admin Web，也不同时接多 runtime；但目录先按 Go 服务最佳实践和产品边界对齐。即使某些模块当前没有代码，也保留明确目录入口，避免后续把职责混进错误位置。

### 3.1 目录边界原则

1. `cmd/*` 只放可执行入口和依赖组装，不放业务逻辑。`main.go` 应该只读取配置、创建 server、启动监听。
2. `internal/controlplane` 放控制面业务核心，包括 workspace 生命周期、session/turn 处理、AgentPod client、事件持久化。
3. `internal/agentpod` 放容器内本地控制层，包括 HTTP 路由、turn runner、cancel、reload、shutdown。
4. `internal/driver` 定义容器驱动接口；`internal/driver/docker` 是 Docker 实现。未来 K8s/VM/local process 只能新增 driver，不应污染 control-plane 主逻辑。
5. `internal/runtime` 定义 AI runtime 接口；`internal/runtime/pi` 是 Pi Agent 适配。未来 Codex/Claude 只能新增 runtime adapter，不应改 AgentPodServer 主合同。
6. `pkg/*` 只放可对外复用的纯合同或通用工具。当前只允许 `protocol` 和 `sse`，避免把业务代码放进 pkg。
7. `web/*` 只放前端；当前可以为空目录。
8. `deploy/*` 放 Dockerfile、compose、部署脚本等环境相关文件。

当前允许 `server.go` 先承载多一点逻辑以保持第一阶段可运行，但后续一旦继续开发，应按上面标注拆成 `workspace.go`、`session.go`、`runner.go` 等文件，避免再次形成大文件。

---

## 4. 顶层模块职责

### 4.1 `user-portal`

面向普通用户的前端入口。

职责：

1. 创建 task
2. 查看 task 执行过程
3. 查看 agent 事件流输出
4. 查看 task 关联的 repo、docs 和结果

---

### 4.2 `admin-console`

面向平台管理员的前端入口。

职责：

1. 查看 workspace 状态
2. 查看 runtime 状态
3. 查看 task 分布情况
4. 进行基础运维和排障

---

### 4.3 `control-plane`

平台总控服务。

职责：

1. 管理 task 生命周期
2. 为 task 分配或绑定 workspace
3. 管理 workspace 的创建、复用和回收
4. 调用 workspace 内的本地代理服务
5. 管理 agent-pod 的启动、停止和状态
6. 收集并保存 agent 事件流
7. 组装 task 上下文

`control-plane` 是平台的控制中心，但不直接深入理解每个 agent-core 的私有协议细节。

---

## 5. 运行时结构

第一版的运行关系如下：

```text
user-portal / admin-console
          |
          v
     control-plane
          |
          v
 agent-pod container
    (Docker 容器)
          |
          v
   AgentPodServer / Supervisor
          |
          v
   PiAgentCoreAdapter
          |
          v
       pi-agent
```

说明：

1. `agent-pod` 是平台抽象，不等同于 Kubernetes Pod。
2. 第一阶段实现形态是 Docker container，后续可替换为 K8s/VM/local process driver。
3. 容器内 `AgentPodServer / Supervisor` 是本地控制进程，用于衔接 `control-plane` 和 `PiAgentCoreAdapter`。
4. 第一阶段 `1 workspace = 1 agent-pod`，一个 workspace 可有多个 session，但 turn 先串行。

---

## 6. 核心概念定义

### 6.1 `task`

`task` 是 AgentHub 的顶层业务对象，表示平台接收到的一项工作。

可以理解为：

- 一个目标
- 一个工作单
- 一个用户请求

示例：

- 修复某个 Bug
- 分析某个仓库问题
- 完成某个需求
- 执行一次 Code Review

第一版约束：

1. 一个 `task` 固定绑定一个 `workspace_id`
2. 一个 `task` 可以关联多个 repo
3. 一个 `task` 拥有自己的独立工作目录

---

### 6.2 `workspace`

`workspace` 是 Agent 工作的执行环境，对应一个 Docker 容器。

它的作用不是表达一个任务本身，而是提供：

1. 文件系统
2. 代码仓库目录
3. 技能目录
4. 文档目录
5. 运行时进程环境

第一版约束：

1. `workspace` 暂时只支持共享模式。
2. 一个 `workspace` 对应一个 `agent-pod`。
3. 一个 `workspace` 可以运行多个 `task` / session。
4. 每个 `task` 在同一个 `workspace` 内有自己独立的任务目录。
5. workspace 文件通过 host path bind mount 挂载到容器内 `/workspace`。

---

### 6.3 `AgentPodServer / Supervisor`

`AgentPodServer / Supervisor` 是运行在 `agent-pod` 容器内的本地代理进程。

它不是具体的 agent runtime，而是本地控制层。

主要职责：

1. 提供 `/health`、`/info`、`/turn`、`/sessions/:sessionId/reconnect`、`/sessions/:sessionId/cancel`、`/reload`、`/shutdown`。
2. 初始化 workspace/task 目录。
3. 管理 Pi Agent Core 的执行、中断和 reload。
4. 将 Pi Agent 原始事件映射为 AgentHub `UniversalEvent`。
5. 做基础健康检查。

之所以需要这一层，是为了避免 `control-plane` 直接通过容器命令去硬控底层 runtime 进程，降低耦合并统一容器内控制逻辑。

---

### 6.4 `runtime adapter`

`runtime adapter` 是 AgentHub 针对不同 agent-core 的适配实现。

第一阶段只实现：

- `PiAgentCoreAdapter`

未来再考虑：

- Codex adapter
- Claude adapter

它的职责不是单纯做协议转发，而是统一承担：

1. 将平台统一输入协议转换为具体 agent-core 协议
2. 将具体 agent-core 的原生事件映射为 AgentHub 统一事件模型
3. 管理该 runtime 的启动、执行、中断和退出

因此它更接近“运行时适配层”，而不是简单的 provider。

---

### 6.5 `agent-core`

`agent-core` 指底层实际被调用的 agent runtime 能力来源，例如：

- Codex
- Claude Agent SDK
- pi-agent

这一层不直接暴露给 `control-plane`，而是由 `PiAgentCoreAdapter` / runtime adapter 封装接入。

---

## 7. Task 目录模型

`task` 的目录模型是任务级隔离模型，而不是 workspace 全局共享根目录模型。

建议在容器内采用如下结构：

```text
/workspace/tasks/<taskId>/
  .agents/skills/
  .claude/skills/
  repos/
  docs/
  AGENTS.md
  CLAUDE.md
```

说明：

1. `repos/` 为当前 task 可访问的代码仓库目录
2. 一个 `task` 可以关联多个 repo
3. `docs/` 为当前 task 的文档目录
4. `.agents/skills/` 和 `.claude/skills/` 为 task 可用技能目录
5. `AGENTS.md` 和 `CLAUDE.md` 为当前 task 的任务级指令文件

这意味着即使多个 task 运行在同一个 workspace 中，它们的任务目录也必须彼此隔离。

---

## 8. Prompt 与上下文组装边界

`control-plane` 不直接负责拼接最终给某个具体 agent-core 的原始 prompt 文本。

推荐边界如下：

### 8.1 `control-plane`

负责组装结构化上下文，例如：

1. task 目标
2. 用户输入
3. 当前 task 目录
4. repo 列表
5. docs 内容
6. `AGENTS.md`
7. `CLAUDE.md`
8. 平台约束和执行策略

也就是输出一个结构化的 `TaskContext`。

---

### 8.2 `runtime adapter`

负责把 `TaskContext` 渲染为底层 runtime 所需的具体输入格式，例如：

1. system prompt
2. messages
3. tool schema
4. runtime-specific instructions

这样可以避免：

1. `control-plane` 深度耦合某个 runtime 的私有输入协议
2. 不同 runtime 的 prompt 拼接逻辑分散在平台各处

---

## 9. 事件模型

第一版采用 `UniversalEvent` 作为平台内部事件合同。

当前统一事件类型至少包括：

1. `session.started`
2. `item.started`
3. `item.delta`
4. `item.completed`
5. `session.ended`
6. `error`

这些事件由 `PiAgentCoreAdapter` 输出，`AgentPodServer` 负责以 SSE 转发，`control-plane` 负责 intercept、持久化和回放。

每条事件至少保留以下基础字段：

1. `workspaceId`
2. `sessionId`
3. `runId`
4. `timestamp`

事件事实进入 PostgreSQL append-only `session_events` event log；`session_events.id` 使用数据库全局递增序列作为跨 session 的 replay cursor。`messages` 保存消息级 projection 元数据，`message_blocks` 保存最终可展示内容块。`item.delta` 只表示实时流式增量，不直接落成 block；`item.completed` 必须携带完整 `item.content`，projection 以该完整内容重建对应 message blocks。

前端读取采用三层合同：

1. `POST /workspaces/{workspaceId}/sessions`：显式创建 session；可选 `firstTurn` 用于创建 session 后立即启动第一轮 run。
2. `POST /sessions/{sessionId}/turns`：在已有 session 中追加一轮用户输入并启动新的 run。
3. `GET /sessions/{sessionId}/messages`：当前可展示消息快照。
4. `GET /sessions/{sessionId}/events?after=<eventId>`：按 `session_events.id` 补洞 / replay。
5. `GET /sessions/{sessionId}/stream`：live SSE，服务端写出 SSE `id:`，浏览器重连时用 `Last-Event-ID` 继续。

执行态进入 `sessions + session_runs`：`sessionId` 与 `runId` 均由 control-plane 生成；同一个 session 第一版只允许一个 active run；`GET /sessions/{sessionId}/state` 返回 `messages + activeRun + latestEventId`；`POST /sessions/{sessionId}/interrupt` 表示 Stop current response，必须携带 `expectedRunId`，只有与 `sessions.active_run_id` 匹配时才转发到 agent-pod cancel，避免误中断后续 run。control-plane 不再提供 workspace 级 turn 入口，也不再通过 turn 请求隐式创建 session。

---

## 10. `AgentPodServer / Supervisor` 是否必须存在

当前判断：必须存在，但第一阶段可以把本地控制层和 Pi adapter 合并在同一个二进制内。

原因：

1. `control-plane` 不适合直接深入容器内部管理底层 runtime 进程。
2. task/workspace 目录初始化逻辑天然属于容器本地。
3. Pi Agent Core 的执行、中断、reload 更适合由本地进程控制。
4. 中断、健康检查和事件桥接也更适合在容器内收口。

第一阶段实现为 `cmd/agent-pod` 单进程入口 + `internal/agentpod` 核心；后续如果 Pi Agent SDK/JSON-RPC 接入复杂度上升，再拆成 Supervisor + adapter 进程。

---

## 11. 技术选型建议

### 11.1 WebUI

前端统一使用：

- TypeScript
- React

---

### 11.2 `control-plane`

建议使用：

- Go

原因：

1. 更适合做平台控制服务
2. 更适合管理并发、长连接和事件流
3. 更适合处理 Docker/容器类编排逻辑
4. 部署形态简单

---

### 11.3 `AgentPodServer / Supervisor`

建议使用：

- Go

原因：

1. 适合做本地常驻进程
2. 适合做进程生命周期管理
3. 适合做本地控制和健康检查

---

### 11.4 `runtime adapter`

建议固定使用：

- TypeScript / Node.js

原因：

1. 更适合做协议转换和事件流映射
2. 更容易对接 Pi Agent、Codex、Claude Agent SDK 等生态
3. 流式 JSON 和消息处理实现成本更低

---

## 12. 当前第一版已确定约束

当前已确认的第一版约束如下：

1. 第一阶段先对接 Pi Agent。
2. `agent-pod` 是平台抽象，不绑定 Kubernetes。
3. 当前 driver 使用 Docker API。
4. control-plane 通过 Docker network + container name 访问 agent-pod。
5. workspace 文件通过 host path bind mount 挂载到 `/workspace`。
6. 镜像先使用 `agenthub-pi-agent-pod:dev`。
7. `1 workspace = 1 agent-pod`，turn 先串行。
8. AgentPodServer 内部接口使用 per-pod bearer token。
9. 事件流采用 `UniversalEvent`。
10. 第一阶段 platform MCP tools 只做 `read_memory` 和 `update_memory`。

---

## 13. 当前仍待后续细化的部分

以下内容本轮先不展开，但后续需要继续细化：

1. Pi Agent SDK/JSON-RPC 的正式接入方式
2. run 超时、重试与更完整观测模型
3. `TaskContext` 的字段结构
4. task 与 repo 的持久化关系模型
5. workspace 的复用、回收和资源限制策略
6. 多 runtime 协作模型

---

## 14. 总结

第一版的核心原则是：

1. 先把平台控制面、workspace 和 runtime 接入关系理顺
2. 先统一 agent 自身事件流
3. 先支持共享 workspace + task 目录隔离
4. 先通过 AgentPodServer / runtime adapter 接入底层 agent-core

在这个基础上，后续再逐步扩展多 runtime、更复杂调度和更强的平台事件模型。
