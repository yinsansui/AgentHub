# user-portal

AgentHub 用户工作台前端，基于 React 19 + TypeScript + Vite。

## 开发

```bash
npm install
npm run dev
```

默认代理到本地后端 `http://localhost:8080`（见 `vite.config.ts`）。

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

URL 中通过 `?workspaceId=<id>` 指定工作区，默认为 `ws_dev`。
