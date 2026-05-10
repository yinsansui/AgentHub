export type StreamStatus = "idle" | "connecting" | "open" | "reconnecting" | "closed" | "error";

export type ErrorPayload = {
  code?: string;
  message: string;
};

export type UniversalBlock = {
  type: string;
  text?: string;
  name?: string;
  input?: string;
  output?: string;
};

export type UniversalEvent = {
  type: string;
  timestamp?: string;
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
  error?: ErrorPayload;
  metadata?: Record<string, unknown>;
};

export type StoredEvent = {
  id: number;
  type: string;
  payload: UniversalEvent;
  createdAt: string;
};

export type MessageProjection = {
  sessionId?: string;
  messageId: string;
  workspaceId?: string;
  runId?: string;
  role: "user" | "assistant" | "system" | string;
  status: "streaming" | "completed" | "error" | string;
  blocks: UniversalBlock[];
  error?: ErrorPayload;
  createdAt?: string;
  updatedAt?: string;
};

export type TaskProjection = {
  taskId: string;
  workspaceId: string;
  createdAt?: string;
  updatedAt?: string;
};

export type SessionProjection = {
  sessionId: string;
  taskId: string;
  workspaceId: string;
  modelId?: string;
  title?: string;
  metadata?: Record<string, unknown>;
  createdAt?: string;
  updatedAt?: string;
};

export type SessionRun = {
  runId: string;
  taskId?: string;
  sessionId?: string;
  workspaceId?: string;
  status: "queued" | "running" | "cancelling" | "completed" | "failed" | "cancelled" | "timed_out" | string;
  startedAt?: string;
  endedAt?: string;
  lastEventId?: number;
  error?: ErrorPayload;
  cancelRequestedAt?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type SessionState = {
  sessionId: string;
  modelId?: string;
  messages: MessageProjection[];
  activeRun?: SessionRun | null;
  latestEventId: number;
};

export type WorkbenchState = {
  workspaceId: string;
  taskId?: string;
  sessionId?: string;
  modelId?: string;
  messages: MessageProjection[];
  activeRun?: SessionRun | null;
  latestEventId: number;
  streamStatus: StreamStatus;
  systemError?: string;
};

export type PodInfo = {
  workspaceId: string;
  name?: string;
  image?: string;
  network?: string;
  status?: string;
  endpoint?: string;
};

export type LLMConnection = {
  id: string;
  userId: string;
  workspaceId: string;
  provider: string;
  apiProtocol: string;
  baseUrl: string;
  defaultModelId?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type LLMModel = {
  id: string;
  connectionId: string;
  modelId: string;
  source: string;
  enabled: boolean;
  raw?: Record<string, unknown>;
  lastSeenAt?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type SkillFile = {
  id?: string;
  skillId?: string;
  path: string;
  content: string;
  contentHash?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type SkillDefinition = {
  id: string;
  slug: string;
  source: string;
  scopeType: string;
  scopeId?: string;
  name?: string;
  description?: string;
  version?: number;
  contentHash?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type SkillDefinitionWithFiles = {
  definition: SkillDefinition;
  files: SkillFile[];
};

export type MCPServerEnv = {
  id?: string;
  serverId?: string;
  name: string;
  value: string;
  createdAt?: string;
  updatedAt?: string;
};

export type MCPServerDefinition = {
  id: string;
  name: string;
  source: string;
  scopeType: string;
  scopeId?: string;
  command: string;
  args?: string[];
  transport: string;
  version?: number;
  contentHash?: string;
  createdAt?: string;
  updatedAt?: string;
};

export type MCPServerDefinitionWithEnv = {
  definition: MCPServerDefinition;
  env: MCPServerEnv[];
};

export type WorkspaceProjection = {
  id: string;
  name: string;
  ownerUserId: string;
};

export type CurrentUser = {
  id: string;
  username: string;
  role: string;
};

export type PluginConfigField = {
  name: string;
  type: string;
  required: boolean;
  description: string;
};

export type RepoEntry = {
  repoName: string;
  repoUrl: string;
};

export type WorkspacePlugin = {
  id: string;
  name: string;
  description: string;
  icon?: string;
  configFields: PluginConfigField[];
  installed: boolean;
  config?: Record<string, unknown>;
  updatedAt?: string;
};
