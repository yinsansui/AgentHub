import type { MessageProjection, SessionRun, UniversalBlock, UniversalEvent, WorkbenchState } from "./types";

const terminalRunEvents = new Set(["run.completed", "run.failed", "run.cancelled", "run.timed_out"]);

export function applyEvent(state: WorkbenchState, event: UniversalEvent, eventId?: number): WorkbenchState {
  const next: WorkbenchState = {
    ...state,
    latestEventId: eventId && eventId > state.latestEventId ? eventId : state.latestEventId
  };

  if (event.type === "run.started") {
    next.activeRun = runFromEvent(event, "running", next.activeRun, eventId);
    next.systemError = undefined;
    return next;
  }
  if (event.type === "run.cancelling") {
    next.activeRun = runFromEvent(event, "cancelling", next.activeRun, eventId);
    return next;
  }
  if (terminalRunEvents.has(event.type)) {
    next.activeRun = null;
    if (event.error?.message) {
      next.systemError = event.error.message;
    }
    return next;
  }
  if (event.type === "error") {
    if (event.messageId) {
      next.messages = upsertMessage(next.messages, messageFromEvent(event, "error"));
    } else if (event.error?.message) {
      next.systemError = event.error.message;
    }
    return next;
  }
  if (event.type === "message.started") {
    next.messages = upsertMessage(next.messages, messageFromEvent(event, "streaming"));
    return next;
  }
  if (event.type === "message.completed") {
    next.messages = upsertMessage(next.messages, {
      ...messageFromEvent(event, "completed"),
      blocks: event.content ?? []
    });
    return next;
  }

  if (isBlockEvent(event.type) && event.messageId) {
    next.messages = updateMessageBlock(next.messages, event);
  }
  return next;
}

function runFromEvent(event: UniversalEvent, status: SessionRun["status"], current?: SessionRun | null, eventId?: number): SessionRun {
  return {
    ...current,
    runId: event.runId || current?.runId || "",
    taskId: event.taskId || current?.taskId,
    sessionId: event.sessionId || current?.sessionId,
    workspaceId: event.workspaceId || current?.workspaceId,
    status,
    lastEventId: eventId ?? current?.lastEventId
  };
}

function messageFromEvent(event: UniversalEvent, status: MessageProjection["status"]): MessageProjection {
  return {
    sessionId: event.sessionId,
    messageId: event.messageId || fallbackMessageId(event),
    workspaceId: event.workspaceId,
    runId: event.runId,
    role: event.role || "assistant",
    status,
    blocks: event.content ?? [],
    error: event.error,
    createdAt: event.timestamp,
    updatedAt: event.timestamp
  };
}

function fallbackMessageId(event: UniversalEvent): string {
  return `${event.role || "assistant"}_${event.runId || "event"}_${event.type}`;
}

function upsertMessage(messages: MessageProjection[], incoming: MessageProjection): MessageProjection[] {
  const index = messages.findIndex((message) => message.messageId === incoming.messageId);
  if (index < 0) {
    return [...messages, incoming];
  }
  const next = messages.slice();
  next[index] = {
    ...next[index],
    ...incoming,
    createdAt: next[index].createdAt ?? incoming.createdAt,
    updatedAt: incoming.updatedAt ?? next[index].updatedAt
  };
  return next;
}

function updateMessageBlock(messages: MessageProjection[], event: UniversalEvent): MessageProjection[] {
  const messageId = event.messageId;
  if (!messageId) return messages;
  const index = event.contentIndex ?? 0;
  const messageIndex = messages.findIndex((message) => message.messageId === messageId);
  const message =
    messageIndex >= 0
      ? messages[messageIndex]
      : {
          sessionId: event.sessionId,
          messageId,
          workspaceId: event.workspaceId,
          runId: event.runId,
          role: event.role || "assistant",
          status: "streaming",
          blocks: [],
          createdAt: event.timestamp,
          updatedAt: event.timestamp
        };

  const blocks = message.blocks.slice();
  const existing = blocks[index] ?? blockPlaceholder(event.type);
  blocks[index] = mergeBlock(existing, event);
  const updated = { ...message, status: event.type.endsWith(".completed") ? message.status : "streaming", blocks, updatedAt: event.timestamp ?? message.updatedAt };

  if (messageIndex < 0) {
    return [...messages, updated];
  }
  const next = messages.slice();
  next[messageIndex] = updated;
  return next;
}

function isBlockEvent(type: string): boolean {
  return (
    type.startsWith("text.") ||
    type.startsWith("thinking.") ||
    type.startsWith("tool_call.")
  );
}

function blockPlaceholder(eventType: string): UniversalBlock {
  if (eventType.startsWith("thinking.")) return { type: "thinking", text: "" };
  if (eventType.startsWith("tool_call.")) return { type: "tool_call", input: "" };
  return { type: "text", text: "" };
}

function mergeBlock(existing: UniversalBlock, event: UniversalEvent): UniversalBlock {
  if (event.block) {
    return { ...existing, ...event.block };
  }
  if (event.type.startsWith("tool_call.")) {
    return {
      ...existing,
      type: "tool_call",
      input: event.partial ?? append(existing.input, event.delta)
    };
  }
  return {
    ...existing,
    type: event.type.startsWith("thinking.") ? "thinking" : "text",
    text: event.partial ?? append(existing.text, event.delta)
  };
}

function append(value = "", delta = ""): string {
  return value + delta;
}
