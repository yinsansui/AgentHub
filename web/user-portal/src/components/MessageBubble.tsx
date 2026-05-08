import { Bot, UserRound } from "lucide-react";
import type { MessageProjection, UniversalBlock } from "../types";

export function MessageBubble({ message }: { message: MessageProjection }) {
  const isUser = message.role === "user";
  const senderName = isUser ? "你" : message.status === "streaming" ? "Agent 生成中" : "Agent";
  const messageTime = formatMessageTime(message.createdAt || message.updatedAt);
  return (
    <article className={`w-[min(840px,100%)] mx-auto ${isUser ? "flex justify-end py-3.5 pb-2" : "py-0.5"}`}>
      <div className={isUser ? "max-w-[min(80%,660px)] px-4 py-[11px] rounded-[18px] bg-black/[0.06] overflow-wrap-anywhere max-[700px]:max-w-full" : "rounded-2xl bg-apple-panel shadow-[0_1px_4px_rgba(0,0,0,0.06)] px-4 py-3.5"}>
        <header className={`flex items-center gap-2 mb-2 text-xs text-apple-fg-50 ${isUser ? "justify-end" : "justify-between"}`}>
          <span className="inline-flex items-center gap-1.5 font-semibold text-apple-fg">
            {isUser ? <UserRound size={13} /> : <Bot size={13} />}
            {senderName}
          </span>
          <span>{messageTime}</span>
        </header>
        <div className="grid gap-2">
          {message.blocks.map((block, index) => <BlockView key={`${message.messageId}-${index}`} block={block} />)}
          {message.error && <div className="block error-block">{message.error.message}</div>}
        </div>
      </div>
    </article>
  );
}

function formatMessageTime(value?: string): string {
  if (!value) return "刚刚";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "刚刚";
  return date.toLocaleString("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function BlockView({ block }: { block: UniversalBlock }) {
  if (block.type === "thinking") {
    return (
      <details className="overflow-wrap-anywhere rounded-xl px-3 py-2.5 bg-black/[0.03] shadow-[inset_0_0_0_1px_rgba(0,0,0,0.07)]">
        <summary className="cursor-pointer text-apple-fg-50 font-semibold">思考过程</summary>
        <pre className="mt-2 overflow-auto whitespace-pre-wrap font-mono text-xs">{block.text}</pre>
      </details>
    );
  }
  if (block.type === "tool_call") {
    return (
      <div className="overflow-wrap-anywhere rounded-xl px-3 py-2.5 bg-black/[0.03] shadow-[inset_0_0_0_1px_rgba(0,0,0,0.07)]">
        <strong className="text-[color-mix(in_oklab,var(--color-apple-accent)_50%,var(--color-apple-fg))]">{block.name || "tool_call"}</strong>
        <pre className="mt-2 overflow-auto whitespace-pre-wrap font-mono text-xs">{block.input || block.output || ""}</pre>
      </div>
    );
  }
  if (block.type === "text") {
    return <div className="overflow-wrap-anywhere whitespace-pre-wrap">{block.text}</div>;
  }
  return <pre className="overflow-wrap-anywhere rounded-xl px-3 py-2.5 bg-black/[0.03] shadow-[inset_0_0_0_1px_rgba(0,0,0,0.07)] mt-2 overflow-auto whitespace-pre-wrap font-mono text-xs">{JSON.stringify(block, null, 2)}</pre>;
}
