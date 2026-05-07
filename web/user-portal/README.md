# user-portal

AgentHub 用户工作台前端，基于 React 19 + TypeScript + Vite。

## 开发

```bash
npm install
npm run dev
```

默认代理到本地后端 `http://127.0.0.1:3000`（见 `vite.config.ts`）。如需改用其他后端地址，可设置 `AGENTHUB_USER_PORTAL_PROXY_TARGET`。

首次进入会展示登录页。当前原型内置账号为：

```text
admin / admin
```

登录成功后，后端通过 HTTP-only cookie 维持会话。

## 构建

```bash
npm run build
```

产物输出到 `dist/`，由后端静态服务托管。

## 项目结构

```
src/
  components/       # 可复用 UI 组件
    MessageBubble.tsx   # 消息气泡（文本/思考/工具调用块）
  lib/
    utils.ts        # 纯工具函数（sessionStorageKey, replaceBy, envMap…）
  api.ts            # 后端 HTTP 客户端
  events.ts         # SSE 事件处理（applyEvent）
  types.ts          # 共享类型定义
  App.tsx           # 根组件（路由、状态、布局）
  styles.css        # 全局样式
```

## 工作区参数

URL 中通过 `?workspaceId=<uuid>` 指定当前工作区。这个值来自后端返回的 `workspace.id`，是系统生成的 UUID，不是用户可编辑字段。用户看到并能修改的是 `workspace.name`。首次登录后，`GET /workspaces` 会按当前 owner 自动创建 `Default 工作空间`，返回形状为 `{ id, name, ownerUserId }`。最近使用的 session 会按 `userId + workspaceId` 存入 `sessionStorage`，避免不同用户或 workspace 串用。
