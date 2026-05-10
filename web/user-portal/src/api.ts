import type {
  LLMConnection,
  LLMModel,
  MCPServerDefinitionWithEnv,
  CurrentUser,
  PodInfo,
  SessionProjection,
  SessionRun,
  SessionState,
  SkillDefinitionWithFiles,
  StoredEvent,
  TaskProjection,
  WorkspaceProjection,
  WorkspacePlugin
} from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly body: string;

  constructor(status: number, body: string) {
    super(body || `HTTP ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

type CreateSessionResponse = {
  task: TaskProjection;
  session: SessionProjection;
  run?: SessionRun;
  streamUrl?: string;
};

type CreateTurnResponse = {
  session: SessionProjection;
  run: SessionRun;
  streamUrl?: string;
};

type InterruptResponse = {
  interrupted: boolean;
  reason?: string;
  expectedRunId?: string;
  activeRun?: SessionRun;
  run?: SessionRun;
  cancelDelivered?: boolean;
  cancelError?: string;
};

export async function getHealth(): Promise<{ ok: boolean }> {
  return apiRequest("/health");
}

export async function getMe(): Promise<{ user: CurrentUser }> {
  return apiRequest("/auth/me");
}

export async function login(username: string, password: string): Promise<{ user: CurrentUser }> {
  return apiRequest("/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password })
  });
}

export async function logout(): Promise<void> {
  await apiRequest("/auth/logout", { method: "POST" });
}

export async function getPod(workspaceId: string): Promise<PodInfo> {
  return apiRequest(`/workspaces/${encodeURIComponent(workspaceId)}/pod`);
}

export async function getWorkspaceLogs(workspaceId: string, tail = 100): Promise<string> {
  return apiRequestText(`/workspaces/${encodeURIComponent(workspaceId)}/logs?tail=${tail}`);
}

export async function getLLMConnection(workspaceId: string): Promise<{ connection: LLMConnection | null; apiKeySet: boolean }> {
  try {
    return await apiRequest<{ connection: LLMConnection; apiKeySet: boolean }>(`/workspaces/${encodeURIComponent(workspaceId)}/llm-connection`);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return { connection: null, apiKeySet: false };
    }
    throw error;
  }
}

export async function saveLLMConnection(
  workspaceId: string,
  body: { apiProtocol: string; baseUrl: string; apiKey: string; defaultModelId?: string }
): Promise<{ connection: LLMConnection; apiKeySet: boolean }> {
  return apiRequest(`/workspaces/${encodeURIComponent(workspaceId)}/llm-connection`, {
    method: "PUT",
    body: JSON.stringify(body)
  });
}

export async function listModels(workspaceId: string): Promise<LLMModel[]> {
  const payload = await apiRequest<{ models: LLMModel[] }>(`/workspaces/${encodeURIComponent(workspaceId)}/llm-models`);
  return payload.models ?? [];
}

export async function refreshModels(workspaceId: string): Promise<LLMModel[]> {
  const payload = await apiRequest<{ models: LLMModel[] }>(`/workspaces/${encodeURIComponent(workspaceId)}/llm-models:refresh`, {
    method: "POST"
  });
  return payload.models ?? [];
}

export async function upsertModel(workspaceId: string, modelId: string, enabled: boolean, source = "manual"): Promise<LLMModel> {
  const payload = await apiRequest<{ model: LLMModel }>(`/workspaces/${encodeURIComponent(workspaceId)}/llm-models`, {
    method: "PUT",
    body: JSON.stringify({ modelId, enabled, source, raw: {} })
  });
  return payload.model;
}

export async function listSkills(workspaceId: string): Promise<SkillDefinitionWithFiles[]> {
  const payload = await apiRequest<{ skills: SkillDefinitionWithFiles[] }>(`/workspaces/${encodeURIComponent(workspaceId)}/skills`);
  return payload.skills ?? [];
}

export async function getSkill(workspaceId: string, slug: string): Promise<SkillDefinitionWithFiles> {
  const payload = await apiRequest<{ skill: SkillDefinitionWithFiles }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/skills/${encodeURIComponent(slug)}`
  );
  return payload.skill;
}

export async function saveSkill(
  workspaceId: string,
  slug: string,
  body: { name: string; description: string; files: Array<{ path: string; content: string }> }
): Promise<SkillDefinitionWithFiles> {
  const payload = await apiRequest<{ skill: SkillDefinitionWithFiles }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/skills/${encodeURIComponent(slug)}`,
    { method: "PUT", body: JSON.stringify(body) }
  );
  return payload.skill;
}

export async function deleteSkill(workspaceId: string, slug: string): Promise<void> {
  await apiRequest(`/workspaces/${encodeURIComponent(workspaceId)}/skills/${encodeURIComponent(slug)}`, { method: "DELETE" });
}

export async function listMCPServers(workspaceId: string): Promise<MCPServerDefinitionWithEnv[]> {
  const payload = await apiRequest<{ mcpServers: MCPServerDefinitionWithEnv[] }>(`/workspaces/${encodeURIComponent(workspaceId)}/mcp-servers`);
  return payload.mcpServers ?? [];
}

export async function getMCPServer(workspaceId: string, name: string): Promise<MCPServerDefinitionWithEnv> {
  const payload = await apiRequest<{ mcpServer: MCPServerDefinitionWithEnv }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/mcp-servers/${encodeURIComponent(name)}`
  );
  return payload.mcpServer;
}

export async function saveMCPServer(
  workspaceId: string,
  name: string,
  body: { command: string; args: string[]; transport: string; env: Record<string, string> }
): Promise<MCPServerDefinitionWithEnv> {
  const payload = await apiRequest<{ mcpServer: MCPServerDefinitionWithEnv }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/mcp-servers/${encodeURIComponent(name)}`,
    { method: "PUT", body: JSON.stringify(body) }
  );
  return payload.mcpServer;
}

export async function deleteMCPServer(workspaceId: string, name: string): Promise<void> {
  await apiRequest(`/workspaces/${encodeURIComponent(workspaceId)}/mcp-servers/${encodeURIComponent(name)}`, { method: "DELETE" });
}

export async function listWorkspacePlugins(workspaceId: string): Promise<WorkspacePlugin[]> {
  const payload = await apiRequest<{ plugins: WorkspacePlugin[] }>(`/workspaces/${encodeURIComponent(workspaceId)}/plugins`);
  return payload.plugins ?? [];
}

export async function installWorkspacePlugin(workspaceId: string, pluginId: string, config: object): Promise<WorkspacePlugin> {
  const payload = await apiRequest<{ plugin: WorkspacePlugin }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/plugins/${encodeURIComponent(pluginId)}/install`,
    { method: "PUT", body: JSON.stringify({ config }) }
  );
  return payload.plugin;
}

export async function listWorkspaces(limit: number, offset: number): Promise<{ workspaces: WorkspaceProjection[]; limit: number; offset: number }> {
  return apiRequest(`/workspaces?limit=${limit}&offset=${offset}`);
}

export async function getWorkspace(workspaceId: string): Promise<{ workspace: WorkspaceProjection }> {
  return apiRequest(`/workspaces/${encodeURIComponent(workspaceId)}`);
}

export async function createWorkspace(name: string): Promise<{ workspace: WorkspaceProjection }> {
  return apiRequest(`/workspaces`, {
    method: "POST",
    body: JSON.stringify({ name })
  });
}

export async function updateWorkspace(id: string, name: string): Promise<{ workspace: WorkspaceProjection }> {
  return apiRequest(`/workspaces/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify({ name })
  });
}

export async function deleteWorkspace(id: string): Promise<{ deleted: boolean; replacementWorkspace?: WorkspaceProjection }> {
  return apiRequest(`/workspaces/${encodeURIComponent(id)}`, {
    method: "DELETE"
  });
}

export async function listSessions(workspaceId: string, limit: number, offset: number): Promise<{ sessions: SessionProjection[]; limit: number; offset: number }> {
  return apiRequest(`/workspaces/${encodeURIComponent(workspaceId)}/sessions?limit=${limit}&offset=${offset}`);
}

export async function deleteSession(sessionId: string): Promise<{ deleted: boolean }> {
  return apiRequest(`/sessions/${encodeURIComponent(sessionId)}`, {
    method: "DELETE"
  });
}

export async function createSession(workspaceId: string, body: { modelId?: string; firstTurn?: { message: string; source: "user" } }): Promise<CreateSessionResponse> {
  return apiRequest(`/workspaces/${encodeURIComponent(workspaceId)}/sessions`, {
    method: "POST",
    body: JSON.stringify(body)
  });
}

export async function createTurn(sessionId: string, message: string): Promise<CreateTurnResponse> {
  return apiRequest(`/sessions/${encodeURIComponent(sessionId)}/turns`, {
    method: "POST",
    body: JSON.stringify({ message, source: "user" })
  });
}

export async function getSessionState(sessionId: string): Promise<SessionState> {
  return apiRequest(`/sessions/${encodeURIComponent(sessionId)}/state`);
}

export async function getSessionEvents(sessionId: string, after: number): Promise<{ events: StoredEvent[]; nextCursor: number }> {
  return apiRequest(`/sessions/${encodeURIComponent(sessionId)}/events?after=${after}&limit=1000`);
}

export async function interruptRun(sessionId: string, expectedRunId: string): Promise<InterruptResponse> {
  return apiRequest(`/sessions/${encodeURIComponent(sessionId)}/interrupt`, {
    method: "POST",
    body: JSON.stringify({ expectedRunId, reason: "user_stop" })
  });
}

async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }
  headers.set("accept", "application/json");
  const response = await fetch(path, { ...init, headers, credentials: "same-origin" });
  if (!response.ok) {
    throw new ApiError(response.status, await response.text());
  }
  return (await response.json()) as T;
}

async function apiRequestText(path: string, init: RequestInit = {}): Promise<string> {
  const response = await fetch(path, { ...init, credentials: "same-origin" });
  if (!response.ok) {
    throw new ApiError(response.status, await response.text());
  }
  return response.text();
}
