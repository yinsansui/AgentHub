import type { MessageProjection, UniversalBlock } from "../types";

export function MessageBubble({ message }: { message: MessageProjection }) {
  const isUser = message.role === "user";
  return (
    <article className={`turn ${isUser ? "user-turn" : "assistant-turn"}`}>
      <div className={isUser ? "user-bubble" : "assistant-card"}>
        {!isUser && (
          <header className="turn-header">
            <span>{message.status === "streaming" ? "Streaming" : "Agent response"}</span>
            <small>{message.runId}</small>
          </header>
        )}
        <div className="blocks">
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
      <details className="block thinking-block">
        <summary>thinking</summary>
        <pre>{block.text}</pre>
      </details>
    );
  }
  if (block.type === "tool_call") {
    return (
      <div className="block tool-block">
        <strong>{block.name || "tool_call"}</strong>
        <pre>{block.input || block.output || ""}</pre>
      </div>
    );
  }
  if (block.type === "text") {
    return <div className="block text-block">{block.text}</div>;
  }
  return <pre className="block unknown-block">{JSON.stringify(block, null, 2)}</pre>;
}
