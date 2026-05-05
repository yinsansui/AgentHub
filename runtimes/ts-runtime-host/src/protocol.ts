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
  type: "session.started" | "item.started" | "item.delta" | "item.completed" | "session.ended" | "error";
  timestamp: string;
  workspaceId?: string;
  taskId?: string;
  sessionId?: string;
  runId?: string;
  itemId?: string;
  role?: string;
  item?: UniversalItem;
  delta?: string;
  error?: EventErrorPayload;
  metadata?: Record<string, unknown>;
}

export interface UniversalItem {
  id: string;
  type: string;
  role?: string;
  content?: UniversalBlock[];
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
