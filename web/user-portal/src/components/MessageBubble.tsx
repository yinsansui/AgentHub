import type { MessageProjection, UniversalBlock } from "../types";

export function MessageBubble({ message }: { message: MessageProjection }) {
  const isUser = message.role === "user";
  return (
    <article className={`w-[min(840px,100%)] mx-auto ${isUser ? "flex justify-end py-3.5 pb-2" : "py-0.5"}`}>
      <div className={isUser ? "max-w-[min(80%,660px)] px-4 py-[11px] rounded-[18px] bg-black/[0.06] overflow-wrap-anywhere max-[700px]:max-w-full" : "rounded-2xl bg-apple-panel shadow-[0_1px_4px_rgba(0,0,0,0.06)] px-4 py-3.5"}>
        {!isUser && (
          <header className="flex justify-between gap-3 mb-2 text-apple-fg-50 text-xs">
            <span className="font-semibold text-apple-fg">{message.status === "streaming" ? "生成中" : "Agent 回复"}</span>
            <small>{message.runId}</small>
          </header>
        )}
        <div className="grid gap-2">
          {message.blocks.map((block, index) => <BlockView key={`${message.messageId}-${index}`} block={block} />)}
          {message.error && <div className="block error-block">{message.error.message}</div>}
        </div>
      </div>
    </article>
  );
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
