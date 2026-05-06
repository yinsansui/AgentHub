# 普通用户 Workbench 前端对接合同

本文档面向接下来实现普通用户 Workbench 的前端 Agent，目标是把当前 AgentHub 后端已经具备的能力按“页面功能 → 接口 → 数据消费规则”整理清楚。

当前 Workbench 的核心闭环是：

```text
workspace 准备
  -> LLM / skill / MCP 配置确认
  -> 创建 session，可选 firstTurn
  -> 读取 /state 初始化
  -> /events 补洞
  -> /stream 实时消费
  -> /turns 追加输入
  -> /interrupt 安全停止当前 activeRun
```

## 1. 当前约束

1. API base 暂按 control-plane 同源或代理前缀处理，本文只写相对路径。
2. 当前 control-plane 没有用户认证、鉴权、多用户隔离接口；前端先按单用户 / dev workspace 处理。
3. `workspaceId` 由前端或外层入口提供；当前没有 workspace 列表接口。
4. session 创建时会自动创建默认 task；当前没有独立创建 task 的接口。
5. session 创建后 `modelId` 锁定；只要已经有一次 turn，不支持切换 model。
6. session skill / MCP 在创建 session 时物理化到 session cwd；修改 workspace skill / MCP 只影响新 session。
7. repo plugin 暂不在本阶段范围内。
8. 当前 DB 规则是不使用外键；前端不要从外键关系做推断。

## 2. 页面 / 功能拆分

### 2.1 Workbench Shell

职责：承载 workspace 入口、状态提示、当前 session、消息区、输入框、Stop 按钮。

需要的数据：

- `workspaceId`
- 当前 `sessionId`
- 当前 session `messages`
- 当前 `activeRun`
- 当前 `latestEventId`
- LLM / skill / MCP 是否已配置
- agent-pod 是否可用

### 2.2 Workspace Runtime 状态

普通用户页可以弱化成“工作区运行状态”，不用暴露所有运维细节。

| 功能 | 接口 | 说明 |
| --- | --- | --- |
| 健康检查 | `GET /health` | control-plane 可用性；DB 配置存在时也检查 DB ping。 |
| workspace pod 默认启动 | 创建 session / turn 时自动 ensure | 不向普通用户暴露显式 start；后端在需要执行前自动启动 agent-pod 并保存 token。 |
| 查看 workspace pod | `GET /workspaces/{workspaceId}/pod` | 用于展示 pod status，例如 `running` / `not_found`。 |
| 停止 workspace pod | `POST /workspaces/{workspaceId}/stop` | 普通用户 MVP 可不暴露，或放到高级操作。 |
| 查看日志 | `GET /workspaces/{workspaceId}/logs?tail=100` | 普通用户 MVP 可不暴露，调试面板可用。返回 `text/plain`。 |

#### 默认启动行为

`POST /workspaces/{workspaceId}/sessions` 和 `POST /sessions/{sessionId}/turns` 会在执行前自动确保 workspace agent-pod 已启动并保存执行 token；前端不需要、也不应该调用显式 start 接口。

#### `GET /workspaces/{workspaceId}/pod`

响应：

```json
{
  "workspaceId": "ws_dev",
  "name": "agent-pod-ws_dev",
  "image": "agenthub-agent-pod:dev",
  "network": "agenthub",
  "status": "running",
  "endpoint": "http://agent-pod-ws_dev:3001"
}
```

## 3. LLM 配置功能

Workbench 创建 session 前必须保证 workspace 已配置 LLM connection，并至少有一个 enabled model。

### 3.1 接口清单

| 功能 | 接口 | 说明 |
| --- | --- | --- |
| 查看 LLM connection | `GET /workspaces/{workspaceId}/llm-connection` | API key 已脱敏，另有 `apiKeySet`。 |
| 保存 LLM connection | `PUT /workspaces/{workspaceId}/llm-connection` | 保存 provider / apiProtocol / baseUrl / apiKey。 |
| 刷新远端 models | `POST /workspaces/{workspaceId}/llm-models:refresh` | 通过 connection 的 `/models` 或 `/v1/models` 查询。失败返回 502。 |
| 列出 workspace models | `GET /workspaces/{workspaceId}/llm-models` | 返回 discovered/manual model 列表。 |
| 手动新增或更新 model | `PUT /workspaces/{workspaceId}/llm-models` | 用 `modelId` upsert，支持 enabled。 |

### 3.2 `GET /workspaces/{workspaceId}/llm-connection`

未配置时：`404 llm connection not configured`。

成功响应：

```json
{
  "connection": {
    "id": "llm_conn_xxx",
    "userId": "",
    "workspaceId": "ws_dev",
    "provider": "anthropic",
    "apiProtocol": "anthropic-messages",
    "baseUrl": "http://example.local:8084",
    "createdAt": "2026-05-06T...Z",
    "updatedAt": "2026-05-06T...Z"
  },
  "apiKeySet": true
}
```

注意：响应里不会返回 `apiKey` 明文。

### 3.3 `PUT /workspaces/{workspaceId}/llm-connection`

请求：

```json
{
  "provider": "anthropic",
  "apiProtocol": "anthropic-messages",
  "baseUrl": "http://example.local:8084",
  "apiKey": "sk-***"
}
```

字段规则：

- `provider` 默认 `anthropic`
- `apiProtocol` 默认 `anthropic-messages`
- `baseUrl` 必填
- `apiKey` 必填

响应同 `GET /llm-connection`。

### 3.4 `POST /workspaces/{workspaceId}/llm-models:refresh`

响应：

```json
{
  "models": [
    {
      "id": "llm_model_xxx",
      "connectionId": "llm_conn_xxx",
      "modelId": "k2p5",
      "source": "discovered",
      "enabled": false,
      "raw": {},
      "lastSeenAt": "2026-05-06T...Z",
      "createdAt": "2026-05-06T...Z",
      "updatedAt": "2026-05-06T...Z"
    }
  ]
}
```

刷新只发现模型，不默认启用新发现模型；是否启用由用户勾选后调用 `PUT /llm-models`。

### 3.5 `PUT /workspaces/{workspaceId}/llm-models`

请求：

```json
{
  "modelId": "k2p5",
  "source": "manual",
  "enabled": true,
  "raw": {}
}
```

字段规则：

- `modelId` 必填
- `source` 默认 `manual`；远端刷新产生的是 `discovered`
- `enabled` 默认 `true`
- 如果远端不支持 models 查询，前端提供手动填写 modelId 的入口即可。

## 4. Skill 管理功能

普通用户 Workbench 是否暴露 skill 编辑可以产品上再收敛；但如果要做“当前 workspace 可用 skill 查看 / 编辑”，对接以下接口。

### 4.1 接口清单

| 功能 | 接口 | 说明 |
| --- | --- | --- |
| 列出 workspace skills | `GET /workspaces/{workspaceId}/skills` | 当前只返回 workspace source 的 skills。 |
| 查看单个 skill | `GET /workspaces/{workspaceId}/skills/{slug}` | 返回 definition + files。 |
| 新建 / 更新 skill | `PUT /workspaces/{workspaceId}/skills/{slug}` | 覆盖式 upsert。 |
| 删除 skill | `DELETE /workspaces/{workspaceId}/skills/{slug}` | 删除后只影响新 session。 |

### 4.2 Skill 数据结构

响应：

```json
{
  "skill": {
    "definition": {
      "id": "skill_xxx",
      "slug": "my-skill",
      "source": "workspace",
      "scopeType": "workspace",
      "scopeId": "ws_dev",
      "name": "My Skill",
      "description": "What this skill does",
      "version": 1,
      "contentHash": "...",
      "createdAt": "2026-05-06T...Z",
      "updatedAt": "2026-05-06T...Z"
    },
    "files": [
      {
        "id": "skill_file_xxx",
        "skillId": "skill_xxx",
        "path": "SKILL.md",
        "content": "# My Skill\n...",
        "contentHash": "...",
        "createdAt": "2026-05-06T...Z",
        "updatedAt": "2026-05-06T...Z"
      }
    ]
  }
}
```

### 4.3 `PUT /workspaces/{workspaceId}/skills/{slug}`

请求：

```json
{
  "name": "My Skill",
  "description": "What this skill does",
  "files": [
    {
      "path": "SKILL.md",
      "content": "# My Skill\n..."
    }
  ]
}
```

校验规则：

- `slug` 必须是安全 path segment：不能为空，不能是 `.` / `..`，不能包含 `/` 或 `\\`。
- `files` 必填且非空。
- `path` 会被清洗为相对路径；不能重复。
- 创建 session 时，skill 会物理化到 session cwd 下：
  - `.agents/skills/<slug>/...`
  - `.claude/skills/<slug>/...`
- 不做实时 reload；已存在 session 的 skill membership 不因为列表变化而改变。

## 5. MCP 管理功能

MCP 与 skill 来源保持一致，当前前端只需要处理 workspace source。

### 5.1 接口清单

| 功能 | 接口 | 说明 |
| --- | --- | --- |
| 列出 MCP servers | `GET /workspaces/{workspaceId}/mcp-servers` | 返回当前 workspace MCP 配置。 |
| 查看单个 MCP server | `GET /workspaces/{workspaceId}/mcp-servers/{name}` | 返回 definition + env。 |
| 新建 / 更新 MCP server | `PUT /workspaces/{workspaceId}/mcp-servers/{name}` | 覆盖式 upsert。 |
| 删除 MCP server | `DELETE /workspaces/{workspaceId}/mcp-servers/{name}` | 删除后只影响新 session。 |

### 5.2 MCP 数据结构

响应：

```json
{
  "mcpServer": {
    "definition": {
      "id": "mcp_xxx",
      "name": "local-tool",
      "source": "workspace",
      "scopeType": "workspace",
      "scopeId": "ws_dev",
      "command": "node",
      "args": ["/path/to/server.js"],
      "transport": "stdio",
      "version": 1,
      "contentHash": "...",
      "createdAt": "2026-05-06T...Z",
      "updatedAt": "2026-05-06T...Z"
    },
    "env": [
      {
        "id": "mcp_env_xxx",
        "serverId": "mcp_xxx",
        "name": "TOKEN",
        "value": "plain-value",
        "createdAt": "2026-05-06T...Z",
        "updatedAt": "2026-05-06T...Z"
      }
    ]
  }
}
```

注意：当前 `mcp_server_env` 不支持 sensitive 字段，前端不要做 sensitive toggle。

### 5.3 `PUT /workspaces/{workspaceId}/mcp-servers/{name}`

请求：

```json
{
  "command": "node",
  "args": ["/path/to/server.js"],
  "transport": "stdio",
  "env": {
    "TOKEN": "plain-value"
  }
}
```

校验规则：

- `name` 必须是安全 path segment。
- `command` 必填。
- `transport` 默认 `stdio`。
- 创建 session 时，MCP 会物理化到 session cwd 下 `.agents/mcp.json`。
- `ts-runtime-host` 读取 `.agents/mcp.json`，启动 stdio MCP server，并把工具注册为 `server__tool` 命名空间。

## 6. Session / Turn / Run 功能

### 6.1 接口清单

| 功能 | 接口 | 说明 |
| --- | --- | --- |
| 创建 session | `POST /workspaces/{workspaceId}/sessions` | 自动创建 task + session；可带 firstTurn 并立即启动 run。 |
| 追加 turn | `POST /sessions/{sessionId}/turns` | 已有 session 下追加用户输入并启动新 run。 |
| 读取 session state | `GET /sessions/{sessionId}/state` | Workbench 初始化首选接口。 |
| 读取消息快照 | `GET /sessions/{sessionId}/messages` | 只要消息列表，不需要 activeRun 时使用。 |
| replay events | `GET /sessions/{sessionId}/events?after={eventId}&limit={limit}` | 补洞 / 历史 replay。 |
| live stream | `GET /sessions/{sessionId}/stream?after={eventId}` | SSE live；也支持浏览器重连 `Last-Event-ID`。 |
| 中断当前 run | `POST /sessions/{sessionId}/interrupt` | 必须带 `expectedRunId`，防止误停新 run。 |

### 6.2 `POST /workspaces/{workspaceId}/sessions`

创建空 session：

```json
{
  "modelId": "k2p5"
}
```

创建 session 并立即发起第一轮：

```json
{
  "modelId": "k2p5",
  "firstTurn": {
    "message": "帮我总结一下这个项目",
    "source": "user"
  }
}
```

字段：

- `modelId` 可选；不传时后端选择 workspace 第一个 enabled model。
- `firstTurn.message` 非空时会立即启动 run。
- `firstTurn.source` 可选；默认 `api`。普通用户 UI 建议传 `user`。
- `title` / `metadata` 当前后端类型仍保留，但产品前面已决定暂时不需要；Workbench 不建议展示或依赖。

成功响应，无 firstTurn：

```json
{
  "task": {
    "taskId": "task_20260506100000.000000000",
    "workspaceId": "ws_dev",
    "createdAt": "2026-05-06T...Z",
    "updatedAt": "2026-05-06T...Z"
  },
  "session": {
    "sessionId": "sess_20260506100000.000000000",
    "taskId": "task_20260506100000.000000000",
    "workspaceId": "ws_dev",
    "modelId": "k2p5",
    "metadata": {},
    "createdAt": "2026-05-06T...Z",
    "updatedAt": "2026-05-06T...Z"
  }
}
```

成功响应，有 firstTurn：

```json
{
  "task": { "taskId": "task_...", "workspaceId": "ws_dev" },
  "session": { "sessionId": "sess_...", "taskId": "task_...", "workspaceId": "ws_dev", "modelId": "k2p5" },
  "run": {
    "runId": "run_...",
    "taskId": "task_...",
    "sessionId": "sess_...",
    "workspaceId": "ws_dev",
    "status": "running",
    "startedAt": "2026-05-06T...Z",
    "lastEventId": 2,
    "createdAt": "2026-05-06T...Z",
    "updatedAt": "2026-05-06T...Z"
  },
  "streamUrl": "/sessions/sess_.../stream"
}
```

常见错误：

| HTTP | 场景 | 前端处理 |
| --- | --- | --- |
| 400 | `firstTurn.message is required` / model 未启用 / LLM 未配置 | 提示用户补配置或输入。 |
| 409 | active run 冲突等业务冲突 | 按响应错误提示处理；workspace pod/token 由后端自动准备。 |
| 500 | prepare session / runtime 启动异常 | 展示错误，并可提供 logs 入口。 |

### 6.3 `POST /sessions/{sessionId}/turns`

请求：

```json
{
  "message": "继续",
  "source": "user"
}
```

字段：

- `message` 必填。
- `source` 可选；默认 `api`，普通用户 UI 建议传 `user`。
- 前端不能指定 `runId`，由 control-plane 生成。

成功响应：

```json
{
  "session": {
    "sessionId": "sess_...",
    "taskId": "task_...",
    "workspaceId": "ws_dev",
    "modelId": "k2p5"
  },
  "run": {
    "runId": "run_...",
    "status": "running",
    "lastEventId": 123
  },
  "streamUrl": "/sessions/sess_.../stream"
}
```

冲突响应：

```json
{
  "error": "active_run_exists",
  "activeRun": {
    "runId": "run_current",
    "status": "running"
  }
}
```

前端处理：如果当前 session 已有 activeRun，不允许发送新 turn；展示当前 run 正在处理，并连接 stream。

## 7. State / Messages / Events / Stream 消费规则

### 7.1 `GET /sessions/{sessionId}/state`

Workbench 初始化首选：

```json
{
  "sessionId": "sess_...",
  "messages": [
    {
      "sessionId": "sess_...",
      "messageId": "user_run_...",
      "workspaceId": "ws_dev",
      "runId": "run_...",
      "role": "user",
      "status": "completed",
      "blocks": [
        { "type": "text", "text": "你好" }
      ],
      "createdAt": "2026-05-06T...Z",
      "updatedAt": "2026-05-06T...Z"
    }
  ],
  "activeRun": {
    "runId": "run_...",
    "status": "running",
    "lastEventId": 123
  },
  "latestEventId": 123
}
```

前端规则：

1. 用 `messages` 初始化消息快照。
2. 用 `activeRun` 决定是否展示 Thinking / Stop。
3. 用 `latestEventId` 作为 replay / stream 起始 cursor。
4. 如果 `activeRun.status = cancelling`，Stop 按钮显示 Cancelling，不再重复触发多个取消请求。

### 7.2 `GET /sessions/{sessionId}/messages`

响应：

```json
{
  "messages": [
    {
      "messageId": "assistant_...",
      "role": "assistant",
      "status": "completed",
      "blocks": [
        { "type": "thinking", "text": "..." },
        { "type": "text", "text": "最终回答" },
        { "type": "tool_call", "name": "repo__search", "input": "{...}" }
      ]
    }
  ]
}
```

用途：只刷新消息快照，不关心 activeRun 时使用。Workbench 主页面仍建议优先 `/state`。

### 7.3 `GET /sessions/{sessionId}/events?after={eventId}&limit={limit}`

响应：

```json
{
  "events": [
    {
      "id": 124,
      "type": "text.delta",
      "payload": {
        "type": "text.delta",
        "timestamp": "2026-05-06T...Z",
        "workspaceId": "ws_dev",
        "taskId": "task_...",
        "sessionId": "sess_...",
        "runId": "run_...",
        "messageId": "assistant_...",
        "contentIndex": 0,
        "delta": "hello",
        "partial": "hello"
      },
      "createdAt": "2026-05-06T...Z"
    }
  ],
  "nextCursor": 124
}
```

规则：

- `after` 默认 `0`。
- `limit` 默认 `500`，最大 `1000`。
- `session_events.id` 是全局递增 replay cursor，不是 session 内局部序号。
- 前端本地 cursor 应使用返回事件的 `id` 或 `nextCursor`。

### 7.4 `GET /sessions/{sessionId}/stream?after={eventId}`

SSE 输出：

```text
id: 124
event: text.delta
data: {"type":"text.delta", ...}

```

规则：

1. 服务端会先 replay `after` 之后的历史事件，再挂 live。
2. 浏览器 `EventSource` 原生重连会带 `Last-Event-ID`；服务端支持该 header。
3. 首次建立连接建议使用 `?after=<latestEventId>`。
4. 每收到一个 SSE，前端把 SSE `id` 作为最新 cursor。
5. 如果 SSE 断开：
   - 先 `GET /events?after=<lastCursor>` 补洞；
   - 再重新打开 `/stream?after=<newCursor>`。

## 8. Event 类型与 UI 投影规则

事件总结构：

```json
{
  "type": "text.delta",
  "timestamp": "2026-05-06T...Z",
  "workspaceId": "ws_dev",
  "taskId": "task_...",
  "sessionId": "sess_...",
  "runId": "run_...",
  "messageId": "assistant_...",
  "contentIndex": 0,
  "role": "assistant",
  "content": [],
  "block": { "type": "text", "text": "..." },
  "delta": "...",
  "partial": "...",
  "error": { "code": "...", "message": "..." },
  "metadata": {}
}
```

### 8.1 run 事件

| 事件 | UI 行为 |
| --- | --- |
| `run.started` | 设置 activeRun，显示 Thinking / Stop。 |
| `run.cancelling` | activeRun.status 视为 `cancelling`，Stop 按钮变 disabled / Cancelling。 |
| `run.completed` | 清空 activeRun，隐藏 Stop。 |
| `run.cancelled` | 清空 activeRun，保留已收到的 partial 展示；后续以快照为准。 |
| `run.failed` | 清空 activeRun，展示错误状态。 |
| `run.timed_out` | 清空 activeRun，展示超时错误。 |

`run.*.metadata.runStatus` 是后端 run 状态的辅助信息。

### 8.2 message 事件

| 事件 | UI 行为 |
| --- | --- |
| `message.started` | 创建 assistant 消息气泡，状态 streaming。 |
| `message.completed` | 使用 `content[]` 覆盖该 message 的完整 blocks，状态 completed。 |

重要规则：`message.completed.content` 是最终 blocks 来源。delta 只服务实时动画和断线补洞，不作为最终 DB projection 真相源。

### 8.3 block 事件

| 事件 | 字段 | UI 行为 |
| --- | --- | --- |
| `text.started` | `messageId`, `contentIndex`, `partial` | 创建 text block 占位。 |
| `text.delta` | `delta`, `partial` | 用 `partial` 覆盖当前 block 展示；没有 partial 时 append delta。 |
| `text.completed` | `block` 或 `partial` | 固化 text block。 |
| `thinking.started` | `messageId`, `contentIndex` | 创建 thinking block，默认可折叠。 |
| `thinking.delta` | `delta`, `partial` | 更新 thinking 内容。 |
| `thinking.completed` | `block` 或 `partial` | 固化 thinking block。 |
| `tool_call.started` | `messageId`, `contentIndex` | 创建 tool call block。 |
| `tool_call.delta` | `delta`, `partial` | 展示参数 JSON 增量，可用 monospace。 |
| `tool_call.completed` | `block.name`, `block.input` | 固化 tool call。 |

Block 渲染建议：

```ts
type UniversalBlock =
  | { type: 'text'; text?: string }
  | { type: 'thinking'; text?: string }
  | { type: 'tool_call'; name?: string; input?: string; output?: string }
  | { type: string; text?: string; name?: string; input?: string; output?: string };
```

未知 block type 不要丢弃，至少用 JSON fallback 展示。

### 8.4 error 事件

| 事件 | UI 行为 |
| --- | --- |
| `error` | 展示 run/message 错误；如果没有明确 messageId，作为当前 run 的系统错误提示。 |

错误结构：

```json
{
  "error": {
    "code": "run_timeout",
    "message": "run timed out after 30m0s"
  }
}
```

### 8.5 session 事件

| 事件 | UI 行为 |
| --- | --- |
| `session.started` | runtime adapter 层会话开始；通常不需要展示成聊天消息。 |
| `session.ended` | runtime adapter 层会话结束；不等于业务 session 被删除。 |

## 9. Safe Cancel / Stop 按钮合同

### 9.1 `POST /sessions/{sessionId}/interrupt`

请求：

```json
{
  "expectedRunId": "run_current",
  "reason": "user_stop"
}
```

规则：

- `expectedRunId` 必填。
- 前端必须从当前 `/state.activeRun.runId` 或 run 创建响应里取值。
- 不允许前端发送“不带 runId 的停止”。

成功响应：

```json
{
  "interrupted": true,
  "run": {
    "runId": "run_current",
    "status": "cancelling",
    "cancelRequestedAt": "2026-05-06T...Z"
  },
  "cancelDelivered": true,
  "cancelError": ""
}
```

重复取消成功响应仍为 200，`run.status` 维持 `cancelling`，不会重复写 `run.cancelling`。

run mismatch 响应：

```json
{
  "interrupted": false,
  "reason": "run_mismatch",
  "expectedRunId": "run_old",
  "activeRun": {
    "runId": "run_new",
    "status": "running"
  }
}
```

前端处理：

1. 如果 HTTP 200 且 `interrupted=true`：进入 Cancelling 状态，等待 stream 中的 `run.cancelled` / terminal run event。
2. 如果 HTTP 409 且 `reason=run_mismatch`：立即重新拉 `/state`，避免误停新的 run。
3. 如果 `reason=already_terminal`：重新拉 `/state`，清空 Stop UI。
4. 如果 `cancelDelivered=false`：提示“取消请求未送达 runtime”，但仍以 event/state 为准。

## 10. 推荐前端状态机

### 10.1 初始化已有 session

```text
GET /sessions/{sessionId}/state
  -> render messages
  -> set activeRun
  -> cursor = latestEventId
GET /sessions/{sessionId}/events?after=cursor
  -> apply events, update cursor
open EventSource(/sessions/{sessionId}/stream?after=cursor)
```

### 10.2 创建新 session + first turn

```text
ensure workspace pod running
ensure llm connection exists
ensure at least one enabled model
POST /workspaces/{workspaceId}/sessions { modelId, firstTurn }
  -> save taskId/sessionId/runId
  -> render optimistic user message only if needed
  -> open streamUrl?after=run.lastEventId or GET /state first
```

更稳妥的实现：创建响应后统一 `GET /state`，再接 `/stream?after=latestEventId`。

### 10.3 追加 turn

```text
if activeRun exists: block send
POST /sessions/{sessionId}/turns { message, source: 'user' }
  -> if success: GET /state, then stream
  -> if 409 active_run_exists: set activeRun and reconnect stream
```

### 10.4 页面刷新恢复

```text
GET /sessions/{sessionId}/state
if activeRun:
  show running/cancelling UI
open /stream?after=latestEventId
```

这样用户从正在执行的 run 中途切走再回来，也能看到 `/messages` 快照里的已完成内容，并通过 `/stream` 或 `/events` 继续获得中间 delta / terminal 状态。

## 11. 前端需要补齐的本地数据结构

建议前端内部维护：

```ts
type WorkbenchState = {
  workspaceId: string;
  taskId?: string;
  sessionId?: string;
  modelId?: string;
  messages: MessageProjection[];
  activeRun?: SessionRun | null;
  latestEventId: number;
  streamStatus: 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed' | 'error';
};
```

事件应用时按 `(messageId, contentIndex)` 定位 block。`message.completed` 到达后，用完整 `content[]` 覆盖该 message 的 blocks，避免 delta 拼接误差。

## 12. 当前后端缺口 / 需要另行确认

这些不是 Workbench 前端能单独解决的问题，需要在实现时决定是临时绕过还是补后端接口：

1. **没有 workspace 列表接口**  
   Workbench 只能由外部指定 `workspaceId`，或先写死 dev workspace。

2. **没有 task / session 列表接口**  
   当前只能创建 session，或通过 URL 中已有 `sessionId` 恢复。普通用户要看到历史会话列表，需要新增类似：
   - `GET /workspaces/{workspaceId}/sessions`
   - 或 `GET /tasks/{taskId}/sessions`

3. **没有获取单个 session 元信息接口**  
   当前 `/state` 只返回 `sessionId/messages/activeRun/latestEventId`，不返回 session 的 `taskId/workspaceId/modelId`。如果前端刷新后需要 modelId，建议补：
   - `GET /sessions/{sessionId}`
   - 或把 `session` 嵌入 `/state`。

4. **没有删除 / 归档 session 接口**  
   本阶段可不做。

5. **没有 session 重命名 / title 接口**  
   之前已决定 `goal/title` 暂时不需要，Workbench 不应先做标题编辑。

6. **没有独立 task 创建接口**  
   当前符合“通过创建 session 自动创建默认 task”的阶段设计；如果以后普通用户要先建 task 再开多个 session，需要再设计。

7. **没有认证 / 用户维度 workspace 选择**  
   本阶段按 dev / 单用户处理。

8. **LLM apiKey 更新必须重新提交明文 key**  
   因为 `PUT /llm-connection` 要求 `apiKey` 必填且 GET 不返回明文。前端编辑 connection 时，如果用户不改 key，当前没有“保留旧 key”的接口语义，需要产品/后端确认。

9. **MCP env 当前明文返回**  
   已明确先不做 sensitive；普通用户页如果展示 MCP env，需要接受明文展示的风险。

## 13. MVP 验收清单

前端 Agent 完成后至少要能验证：

1. 进入 workbench 后能检查 `/health`。
2. 能启动或确认 workspace pod running。
3. 能配置 LLM connection。
4. 能刷新或手动添加 model，并启用目标 model。
5. 能创建 session + firstTurn。
6. 能展示 user message 和 assistant streaming text delta。
7. 收到 `message.completed` 后，消息内容以完整 blocks 为准。
8. 页面刷新后，`GET /state` 能恢复 messages、activeRun、latestEventId。
9. active run 执行中 Stop 按钮携带 `expectedRunId` 调 `/interrupt`。
10. run mismatch 时前端能重新拉 `/state`，不误停新 run。
11. cancel 后能看到 `run.cancelling`，最终 `run.cancelled` 后 activeRun 清空。
12. runtime error / timeout 时能展示错误信息并清空 activeRun。
13. 不依赖 `messages.blocks` JSONB；只消费后端 projection 返回的 `blocks` 数组。

## 14. 建议优先级

### P0：聊天闭环

- `/health`
- `/workspaces/{workspaceId}/pod`
- `/workspaces/{workspaceId}/llm-connection`
- `/workspaces/{workspaceId}/llm-models`
- `/workspaces/{workspaceId}/sessions`
- `/sessions/{sessionId}/state`
- `/sessions/{sessionId}/events`
- `/sessions/{sessionId}/stream`
- `/sessions/{sessionId}/turns`
- `/sessions/{sessionId}/interrupt`

### P1：配置面

- skill list/detail/upsert/delete
- MCP list/detail/upsert/delete
- workspace logs

### P2：需要后端补接口后再做

- workspace 列表
- session 历史列表
- session 元信息详情
- session 删除 / 归档
- session 重命名
