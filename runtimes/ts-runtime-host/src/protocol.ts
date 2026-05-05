export type RuntimeCommand = RunCommand | AbortCommand | ShutdownCommand;

export interface RunCommand {
  type: "run";
  workspaceId: string;
  taskId: string;
  sessionId: string;
  runId: string;
  cwd: string;
  message: string;
  source?: string;
}

export interface AbortCommand {
  type: "abort";
  runId: string;
}

export interface ShutdownCommand {
  type: "shutdown";
}

export interface UniversalEvent {
  type:
    | "session.started"
    | "session.ended"
    | "error"
    | "run.started"
    | "run.cancelling"
    | "run.completed"
    | "run.failed"
    | "run.cancelled"
    | "run.timed_out"
    | "message.started"
    | "message.completed"
    | "text.started"
    | "text.delta"
    | "text.completed"
    | "thinking.started"
    | "thinking.delta"
    | "thinking.completed"
    | "tool_call.started"
    | "tool_call.delta"
    | "tool_call.completed";
  timestamp: string;
  workspaceId?: string;
  taskId?: string;
  sessionId?: string;
  runId?: string;
  messageId?: string;
  contentIndex?: number;
  role?: string;
  content?: UniversalBlock[];
  block?: UniversalBlock;
  delta?: string;
  partial?: string;
  error?: EventErrorPayload;
  metadata?: Record<string, unknown>;
}

export interface UniversalBlock {
  type: string;
  text?: string;
  name?: string;
  input?: string;
  output?: string;
}

export interface EventErrorPayload {
  code?: string;
  message: string;
}

export function baseEvent(type: UniversalEvent["type"], run: RunCommand): UniversalEvent {
  return {
    type,
    timestamp: new Date().toISOString(),
    workspaceId: run.workspaceId,
    taskId: run.taskId,
    sessionId: run.sessionId,
    runId: run.runId,
  };
}
