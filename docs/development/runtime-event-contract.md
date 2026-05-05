# Runtime Event Contract

AgentHub 前端按 `snapshot + replay + live` 消费会话状态：

```text
GET /sessions/{sessionId}/messages        # 首次加载的消息快照
GET /sessions/{sessionId}/events?after=id # 补洞 / replay，使用全局递增 event id
GET /sessions/{sessionId}/stream          # live SSE，浏览器重连用 Last-Event-ID
GET /sessions/{sessionId}/state           # messages + activeRun + latestEventId
```

## 事件分层

- `run.*`：驱动 Thinking、Stop 按钮、状态条和 active run 恢复。
- `message.*`：驱动消息气泡生命周期；`message.completed.content` 是 `/messages` 快照的最终来源。
- `text.*` / `thinking.*` / `tool_call.*`：驱动单个 `message.content[contentIndex]` 的实时块级展示。
- `error`：写入 event log；没有明确 `messageId` 时会投影为本 run 的 error message。
- `session.*`：底层 runtime adapter 生命周期，不等同于业务 session 创建/结束。

实时 delta 只用于 UI 动画和补洞 replay，不直接作为最终 blocks 真相源。最终展示以 `message.completed.content` 重建 `messages + message_blocks`。

## 标准序列

### 普通文本回答

```text
run.started
message.completed            # user message，control-plane 已有完整输入
session.started
message.started              # assistant bubble
text.started                 # contentIndex = 0
text.delta                   # delta + partial
text.completed               # block 完整文本
message.completed            # 完整 content[]
session.ended
run.completed
```

### thinking + text

```text
run.started
message.completed            # user
message.started              # assistant
thinking.started             # contentIndex = 0
thinking.delta
thinking.completed
text.started                 # contentIndex = 1
text.delta
text.completed
message.completed            # content[0]=thinking, content[1]=text
run.completed
```

### tool call

```text
run.started
message.completed            # user
message.started              # assistant
tool_call.started            # contentIndex = 0
tool_call.delta              # 增量 JSON 参数
tool_call.completed          # block.name + block.input
message.completed            # assistant message with tool_call block
run.completed                # 如果 runtime 后续继续回答，会产生下一条 assistant message
```

### cancel

```text
run.started
message.completed            # user
message.started              # assistant may already exist
text.delta                   # optional partial content
run.cancelling               # POST /interrupt expectedRunId 命中 active run
session.ended                # runtime abort 后结束
run.cancelled                # activeRun 清空
```

Cancel 语义：

- `POST /sessions/{sessionId}/interrupt` 必须携带 `expectedRunId`。
- 如果 `expectedRunId != state.activeRun.runId`，返回 conflict，不写 `run.cancelling`。
- repeated cancel 只保持 `cancelling`，不重复写 `run.cancelling`。

### timeout / runtime error

```text
run.started
message.completed            # user
error                         # error.code = run_timeout 或 runtime error
run.timed_out | run.failed
```

Timeout / error 终态必须写入 `session_runs.error_code/error_message`，并清空 active run。
