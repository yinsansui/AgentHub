import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowUp,
  ChevronDown,
  FileText,
  Play,
  Plus,
  RefreshCw,
  Save,
  ScrollText,
  Server,
  Settings,
  SquarePen,
  Trash2,
  Zap
} from "lucide-react";
import {
  ApiError,
  createSession,
  createTurn,
  deleteMCPServer,
  deleteSkill,
  getHealth,
  getLLMConnection,
  getMCPServer,
  getPod,
  getSessionEvents,
  getSessionState,
  getSkill,
  getWorkspaceLogs,
  interruptRun,
  listMCPServers,
  listModels,
  listSessions,
  listSkills,
  refreshModels,
  saveLLMConnection,
  saveMCPServer,
  saveSkill,
  startPod,
  upsertModel
} from "./api";
import { applyEvent } from "./events";
import { MessageBubble } from "./components/MessageBubble";
import { sessionStorageKey, errorMessage, replaceBy, lines, envMap } from "./lib/utils";
import type {
  LLMConnection,
  LLMModel,
  MCPServerDefinitionWithEnv,
  PodInfo,
  SessionProjection,
  SkillDefinitionWithFiles,
  UniversalEvent,
  WorkbenchState
} from "./types";

const defaultWorkspaceId = "ws_dev";

type LoadState = "idle" | "loading" | "ready" | "error";
type NavigationPanel = "sessions" | "settings";
type SettingsTab = "runtime" | "llm" | "skills" | "mcp";

type Notice = {
  tone: "info" | "error" | "success";
  text: string;
};

const initialLLMForm = {
  provider: "anthropic",
  apiProtocol: "anthropic-messages",
  baseUrl: "",
  apiKey: ""
};

const initialSkillForm = {
  slug: "",
  name: "",
  description: "",
  path: "SKILL.md",
  content: "# Skill\n\n"
};

const initialMCPForm = {
  name: "",
  command: "",
  args: "",
  transport: "stdio",
  env: ""
};

export default function App() {
  const initialWorkspaceId = useMemo(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get("workspaceId")?.trim() || defaultWorkspaceId;
  }, []);
  const [workspaceId, setWorkspaceId] = useState(initialWorkspaceId);
  const [workspaceDraft, setWorkspaceDraft] = useState(initialWorkspaceId);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [loadState, setLoadState] = useState<LoadState>("idle");
  const [healthOk, setHealthOk] = useState<boolean | null>(null);
  const [pod, setPod] = useState<PodInfo | null>(null);
  const [logs, setLogs] = useState("");
  const [showLogs, setShowLogs] = useState(false);
  const [activePanel, setActivePanel] = useState<NavigationPanel>("sessions");
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("runtime");
  const [skillEditorOpen, setSkillEditorOpen] = useState(false);
  const [mcpEditorOpen, setMCPEditorOpen] = useState(false);

  const [sessionList, setSessionList] = useState<SessionProjection[]>([]);
  const [sessionListOffset, setSessionListOffset] = useState(0);
  const [sessionListHasMore, setSessionListHasMore] = useState(true);
  const sessionListLoadingRef = useRef(false);

  const [llmConnection, setLLMConnection] = useState<LLMConnection | null>(null);
  const [apiKeySet, setApiKeySet] = useState(false);
  const [llmForm, setLLMForm] = useState(initialLLMForm);
  const [models, setModels] = useState<LLMModel[]>([]);
  const [manualModelId, setManualModelId] = useState("");

  const [skills, setSkills] = useState<SkillDefinitionWithFiles[]>([]);
  const [skillForm, setSkillForm] = useState(initialSkillForm);
  const [mcpServers, setMCPServers] = useState<MCPServerDefinitionWithEnv[]>([]);
  const [mcpForm, setMCPForm] = useState(initialMCPForm);

  const [workbench, setWorkbench] = useState<WorkbenchState>({
    workspaceId: initialWorkspaceId,
    messages: [],
    activeRun: null,
    latestEventId: 0,
    streamStatus: "idle"
  });
  const [sessionDraft, setSessionDraft] = useState(() => sessionStorage.getItem(sessionStorageKey(initialWorkspaceId)) ?? "");
  const [selectedModelId, setSelectedModelId] = useState("");
  const [messageDraft, setMessageDraft] = useState("");
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<number | null>(null);
  const latestEventIdRef = useRef(0);

  const enabledModels = models.filter((model) => model.enabled);
  const canSend = messageDraft.trim() !== "" && !workbench.activeRun;

  const reportError = useCallback((error: unknown, fallback: string) => {
    setNotice({ tone: "error", text: errorMessage(error, fallback) });
  }, []);

  const loadMoreSessions = useCallback(async (reset = false) => {
    if (sessionListLoadingRef.current) return;
    sessionListLoadingRef.current = true;
    const offset = reset ? 0 : sessionListOffset;
    try {
      const result = await listSessions(workspaceId, 20, offset);
      setSessionList((prev) => reset ? result.sessions : [...prev, ...result.sessions]);
      setSessionListOffset(offset + result.sessions.length);
      setSessionListHasMore(result.sessions.length === 20);
    } catch {
      // silently ignore
    } finally {
      sessionListLoadingRef.current = false;
    }
  }, [workspaceId, sessionListOffset]);

  const refreshWorkspace = useCallback(async () => {
    setLoadState("loading");
    setNotice(null);
    try {
      const [health, llm, modelList, skillList, mcpList] = await Promise.all([
        getHealth(),
        getLLMConnection(workspaceId),
        listModels(workspaceId),
        listSkills(workspaceId),
        listMCPServers(workspaceId)
      ]);
      setHealthOk(health.ok);
      setLLMConnection(llm.connection);
      setApiKeySet(llm.apiKeySet);
      setLLMForm({
        provider: llm.connection?.provider ?? "anthropic",
        apiProtocol: llm.connection?.apiProtocol ?? "anthropic-messages",
        baseUrl: llm.connection?.baseUrl ?? "",
        apiKey: ""
      });
      setModels(modelList);
      setSkills(skillList);
      setMCPServers(mcpList);
      try {
        setPod(await getPod(workspaceId));
      } catch {
        setPod(null);
      }
      setLoadState("ready");
    } catch (error) {
      setLoadState("error");
      reportError(error, "加载 workspace 失败");
    }
  }, [reportError, workspaceId]);

  const disconnectStream = useCallback(() => {
    eventSourceRef.current?.close();
    eventSourceRef.current = null;
    if (reconnectTimerRef.current) {
      window.clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const connectStream = useCallback(
    (sessionId: string, after: number) => {
      disconnectStream();
      setWorkbench((current) => ({ ...current, streamStatus: "connecting" }));
      const source = new EventSource(`/sessions/${encodeURIComponent(sessionId)}/stream?after=${after}`);
      eventSourceRef.current = source;
      source.onopen = () => {
        setWorkbench((current) => ({ ...current, streamStatus: "open" }));
      };
      source.onerror = () => {
        source.close();
        if (eventSourceRef.current === source) {
          eventSourceRef.current = null;
        }
        setWorkbench((current) => ({ ...current, streamStatus: "reconnecting" }));
        reconnectTimerRef.current = window.setTimeout(async () => {
          try {
            const currentCursor = latestEventIdRef.current;
            const replay = await getSessionEvents(sessionId, currentCursor);
            setWorkbench((current) => {
              const replayed = replay.events.reduce((next, event) => applyEvent(next, event.payload, event.id), current);
              latestEventIdRef.current = replay.nextCursor;
              return { ...replayed, latestEventId: replay.nextCursor };
            });
            connectStream(sessionId, replay.nextCursor);
          } catch (error) {
            setWorkbench((current) => ({ ...current, streamStatus: "error" }));
            reportError(error, "SSE 重连失败");
          }
        }, 1200);
      };
      const handleMessage = (raw: MessageEvent<string>) => {
        try {
          const event = JSON.parse(raw.data) as UniversalEvent;
          const eventId = Number(raw.lastEventId || 0);
          setWorkbench((current) => {
            const next = applyEvent(current, event, eventId || undefined);
            latestEventIdRef.current = next.latestEventId;
            return next;
          });
        } catch (error) {
          reportError(error, "解析事件失败");
        }
      };
      [
        "run.started",
        "run.cancelling",
        "run.completed",
        "run.failed",
        "run.cancelled",
        "run.timed_out",
        "message.started",
        "message.completed",
        "text.started",
        "text.delta",
        "text.completed",
        "thinking.started",
        "thinking.delta",
        "thinking.completed",
        "tool_call.started",
        "tool_call.delta",
        "tool_call.completed",
        "error",
        "session.started",
        "session.ended"
      ].forEach((type) => source.addEventListener(type, handleMessage as EventListener));
      source.onmessage = handleMessage;
    },
    [disconnectStream, reportError]
  );

  const loadSession = useCallback(
    async (sessionId: string) => {
      if (!sessionId.trim()) return;
      try {
        disconnectStream();
        setActivePanel("sessions");
        setWorkbench((current) => ({ ...current, streamStatus: "connecting" }));
        const state = await getSessionState(sessionId.trim());
        const next: WorkbenchState = {
          workspaceId,
          sessionId: state.sessionId,
          taskId: state.activeRun?.taskId,
          modelId: undefined,
          messages: state.messages ?? [],
          activeRun: state.activeRun ?? null,
          latestEventId: state.latestEventId ?? 0,
          streamStatus: "connecting"
        };
        latestEventIdRef.current = next.latestEventId;
        setWorkbench(next);
        setSessionDraft(state.sessionId);
        sessionStorage.setItem(sessionStorageKey(workspaceId), state.sessionId);
        const replay = await getSessionEvents(state.sessionId, next.latestEventId);
        const replayed = replay.events.reduce((current, event) => applyEvent(current, event.payload, event.id), next);
        latestEventIdRef.current = replay.nextCursor;
        setWorkbench({ ...replayed, latestEventId: replay.nextCursor, streamStatus: "connecting" });
        connectStream(state.sessionId, replay.nextCursor);
      } catch (error) {
        setWorkbench((current) => ({ ...current, streamStatus: "error" }));
        reportError(error, "加载 session 失败");
      }
    },
    [connectStream, disconnectStream, reportError, workspaceId]
  );

  useEffect(() => {
    void refreshWorkspace();
    setSessionList([]);
    setSessionListOffset(0);
    setSessionListHasMore(true);
    sessionListLoadingRef.current = false;
    void listSessions(workspaceId, 20, 0).then((result) => {
      setSessionList(result.sessions);
      setSessionListOffset(result.sessions.length);
      setSessionListHasMore(result.sessions.length === 20);
    }).catch(() => {});
    const storedSession = sessionStorage.getItem(sessionStorageKey(workspaceId));
    setSessionDraft(storedSession ?? "");
    setWorkbench({
      workspaceId,
      sessionId: storedSession ?? undefined,
      messages: [],
      activeRun: null,
      latestEventId: 0,
      streamStatus: "idle"
    });
    if (storedSession) {
      void loadSession(storedSession);
    }
    return () => disconnectStream();
  }, [disconnectStream, loadSession, refreshWorkspace, workspaceId]);

  async function handleWorkspaceSubmit(event: FormEvent) {
    event.preventDefault();
    const nextWorkspaceId = workspaceDraft.trim() || defaultWorkspaceId;
    setWorkspaceId(nextWorkspaceId);
    const url = new URL(window.location.href);
    url.searchParams.set("workspaceId", nextWorkspaceId);
    window.history.replaceState(null, "", url);
  }

  function handleNewSession() {
    setActivePanel("sessions");
    setWorkbench((current) => ({ ...current, sessionId: undefined, taskId: undefined, modelId: undefined, messages: [], activeRun: null, latestEventId: 0, streamStatus: "idle" }));
    setSessionDraft("");
    disconnectStream();
  }

  async function handleStartPod() {
    try {
      const payload = await startPod(workspaceId);
      setPod(payload.pod);
      setNotice({ tone: "success", text: "工作区已启动" });
    } catch (error) {
      reportError(error, "启动工作区失败");
    }
  }

  async function handleLoadLogs() {
    try {
      setLogs(await getWorkspaceLogs(workspaceId));
      setShowLogs(true);
      setActivePanel("settings");
    } catch (error) {
      reportError(error, "读取日志失败");
    }
  }

  async function handleSaveLLM(event: FormEvent) {
    event.preventDefault();
    try {
      const payload = await saveLLMConnection(workspaceId, llmForm);
      setLLMConnection(payload.connection);
      setApiKeySet(payload.apiKeySet);
      setLLMForm((current) => ({ ...current, apiKey: "" }));
      setNotice({ tone: "success", text: "LLM connection 已保存" });
    } catch (error) {
      reportError(error, "保存 LLM connection 失败");
    }
  }

  async function handleRefreshModels() {
    try {
      setModels(await refreshModels(workspaceId));
      setNotice({ tone: "success", text: "远端模型已刷新" });
    } catch (error) {
      reportError(error, "刷新模型失败");
    }
  }

  async function handleUpsertModel(modelId: string, enabled: boolean, source?: string) {
    if (!modelId.trim()) return;
    try {
      const saved = await upsertModel(workspaceId, modelId.trim(), enabled, source);
      setModels((current) => replaceBy(current, saved, (model) => model.modelId));
      if (saved.enabled && !selectedModelId) setSelectedModelId(saved.modelId);
    } catch (error) {
      reportError(error, "保存模型失败");
    }
  }

  async function handleManualModel(event: FormEvent) {
    event.preventDefault();
    await handleUpsertModel(manualModelId, true, "manual");
    setManualModelId("");
  }

  async function handleCreateOrTurn(event: FormEvent) {
    event.preventDefault();
    const message = messageDraft.trim();
    if (!message || workbench.activeRun) return;
    try {
      if (!workbench.sessionId) {
        const response = await createSession(workspaceId, {
          modelId: selectedModelId || undefined,
          firstTurn: { message, source: "user" }
        });
        setMessageDraft("");
        setWorkbench((current) => ({
          ...current,
          workspaceId,
          taskId: response.task.taskId,
          sessionId: response.session.sessionId,
          modelId: response.session.modelId,
          activeRun: response.run ?? null
        }));
        sessionStorage.setItem(sessionStorageKey(workspaceId), response.session.sessionId);
        await loadSession(response.session.sessionId);
      } else {
        const response = await createTurn(workbench.sessionId, message);
        setMessageDraft("");
        setWorkbench((current) => ({ ...current, activeRun: response.run, taskId: response.session.taskId, modelId: response.session.modelId }));
        await loadSession(response.session.sessionId);
      }
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setNotice({ tone: "error", text: "当前 workspace 未启动或已有 active run，请刷新状态后重试" });
      } else {
        reportError(error, "发送消息失败");
      }
    }
  }

  async function handleStop() {
    const activeRun = workbench.activeRun;
    if (!workbench.sessionId || !activeRun?.runId || activeRun.status === "cancelling") return;
    try {
      const result = await interruptRun(workbench.sessionId, activeRun.runId);
      if (result.interrupted && result.run) {
        setWorkbench((current) => ({ ...current, activeRun: result.run ?? current.activeRun }));
      } else {
        await loadSession(workbench.sessionId);
      }
      if (result.cancelError) {
        setNotice({ tone: "error", text: `取消请求未完全送达：${result.cancelError}` });
      }
    } catch (error) {
      if (error instanceof ApiError && error.status === 409 && workbench.sessionId) {
        await loadSession(workbench.sessionId);
      } else {
        reportError(error, "停止 run 失败");
      }
    }
  }

  function handleNewSkill() {
    setActivePanel("settings");
    setSettingsTab("skills");
    setSkillForm(initialSkillForm);
    setSkillEditorOpen(true);
  }

  async function handleLoadSkill(slug: string) {
    try {
      const skill = await getSkill(workspaceId, slug);
      setActivePanel("settings");
      setSettingsTab("skills");
      setSkillEditorOpen(true);
      setSkillForm({
        slug: skill.definition.slug,
        name: skill.definition.name ?? "",
        description: skill.definition.description ?? "",
        path: skill.files[0]?.path ?? "SKILL.md",
        content: skill.files[0]?.content ?? ""
      });
    } catch (error) {
      reportError(error, "读取 skill 失败");
    }
  }

  async function handleSaveSkill(event: FormEvent) {
    event.preventDefault();
    try {
      const saved = await saveSkill(workspaceId, skillForm.slug.trim(), {
        name: skillForm.name,
        description: skillForm.description,
        files: [{ path: skillForm.path, content: skillForm.content }]
      });
      setSkills((current) => replaceBy(current, saved, (skill) => skill.definition.slug));
      setSkillEditorOpen(true);
      setNotice({ tone: "success", text: "Skill 已保存，新 session 生效" });
    } catch (error) {
      reportError(error, "保存 skill 失败");
    }
  }

  async function handleDeleteSkill(slug: string) {
    try {
      await deleteSkill(workspaceId, slug);
      setSkills((current) => current.filter((skill) => skill.definition.slug !== slug));
      if (skillForm.slug === slug) {
        setSkillForm(initialSkillForm);
        setSkillEditorOpen(false);
      }
    } catch (error) {
      reportError(error, "删除 skill 失败");
    }
  }

  function handleNewMCP() {
    setActivePanel("settings");
    setSettingsTab("mcp");
    setMCPForm(initialMCPForm);
    setMCPEditorOpen(true);
  }

  async function handleLoadMCP(name: string) {
    try {
      const server = await getMCPServer(workspaceId, name);
      setActivePanel("settings");
      setSettingsTab("mcp");
      setMCPEditorOpen(true);
      setMCPForm({
        name: server.definition.name,
        command: server.definition.command,
        args: (server.definition.args ?? []).join("\n"),
        transport: server.definition.transport || "stdio",
        env: server.env.map((entry) => `${entry.name}=${entry.value}`).join("\n")
      });
    } catch (error) {
      reportError(error, "读取 MCP server 失败");
    }
  }

  async function handleSaveMCP(event: FormEvent) {
    event.preventDefault();
    try {
      const saved = await saveMCPServer(workspaceId, mcpForm.name.trim(), {
        command: mcpForm.command,
        args: lines(mcpForm.args),
        transport: mcpForm.transport || "stdio",
        env: envMap(mcpForm.env)
      });
      setMCPServers((current) => replaceBy(current, saved, (server) => server.definition.name));
      setMCPEditorOpen(true);
      setNotice({ tone: "success", text: "MCP server 已保存，新 session 生效" });
    } catch (error) {
      reportError(error, "保存 MCP server 失败");
    }
  }

  async function handleDeleteMCP(name: string) {
    try {
      await deleteMCPServer(workspaceId, name);
      setMCPServers((current) => current.filter((server) => server.definition.name !== name));
      if (mcpForm.name === name) {
        setMCPForm(initialMCPForm);
        setMCPEditorOpen(false);
      }
    } catch (error) {
      reportError(error, "删除 MCP server 失败");
    }
  }

  function renderMainPanel() {
    if (activePanel === "sessions") {
      return (
        <>
          {notice && <div className={`notice ${notice.tone}`}>{notice.text}</div>}
          {workbench.systemError && <div className="notice error">{workbench.systemError}</div>}

          <div className="conversation-mask">
            <div className="conversation">
              {workbench.messages.map((message) => <MessageBubble key={message.messageId} message={message} />)}
              {workbench.messages.length === 0 && (
                <div className="empty-state">
                  <h3>有什么我可以帮你的？</h3>
                </div>
              )}
            </div>
          </div>

          <form className="composer" onSubmit={handleCreateOrTurn}>
            <div className="composer-card">
              <textarea value={messageDraft} onChange={(event) => setMessageDraft(event.target.value)} placeholder="Message AgentHub..." onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); if (canSend) e.currentTarget.form?.requestSubmit(); } }} />
              <div className="composer-toolbar">
                <div className="composer-toolbar-left">
                  <button type="button" className="composer-tool-btn" aria-label="Add"><Plus size={16} /></button>
                  <select className="composer-model-select" value={selectedModelId} onChange={(event) => setSelectedModelId(event.target.value)} disabled={Boolean(workbench.sessionId)}>
                    <option value="">Default model</option>
                    {enabledModels.map((model) => <option key={model.modelId} value={model.modelId}>{model.modelId}</option>)}
                  </select>
                </div>
                <button type="submit" className="composer-send-btn" disabled={!canSend} aria-label="Send">
                  <ArrowUp size={16} />
                </button>
              </div>
            </div>
          </form>
        </>
      );
    }

  }

  function renderSettingsContent() {
    if (settingsTab === "runtime") {
      return (
        <div className="page-body">
          <div className="page-actions">
            <button type="button" onClick={() => void refreshWorkspace()}><RefreshCw size={14} />Refresh</button>
            <button type="button" onClick={() => void handleStartPod()}><Play size={14} />Start workspace</button>
            <button type="button" onClick={() => void handleLoadLogs()}><ScrollText size={14} />Logs</button>
          </div>
          <dl className="detail-list page-card">
            <div><dt>Workspace</dt><dd>{workspaceId}</dd></div>
            <div><dt>Pod</dt><dd>{pod?.name ?? "not found"}</dd></div>
            <div><dt>Image</dt><dd>{pod?.image ?? "-"}</dd></div>
            <div><dt>Endpoint</dt><dd>{pod?.endpoint ?? "-"}</dd></div>
          </dl>
          {showLogs && <pre className="log-box large">{logs || "No logs loaded."}</pre>}
        </div>
      );
    }
    if (settingsTab === "llm") {
      return (
        <div className="page-body">
          <form className="settings-form" onSubmit={handleSaveLLM}>
            <label>Provider<input value={llmForm.provider} onChange={(event) => setLLMForm({ ...llmForm, provider: event.target.value })} /></label>
            <label>API protocol<input value={llmForm.apiProtocol} onChange={(event) => setLLMForm({ ...llmForm, apiProtocol: event.target.value })} /></label>
            <label>Base URL<input value={llmForm.baseUrl} onChange={(event) => setLLMForm({ ...llmForm, baseUrl: event.target.value })} placeholder="http://example.local:8084" /></label>
            <label>API key<input value={llmForm.apiKey} onChange={(event) => setLLMForm({ ...llmForm, apiKey: event.target.value })} type="password" placeholder={llmConnection ? "Required to update" : "Required"} /></label>
            <button type="submit"><Save size={14} />Save connection</button>
          </form>
          <div className="subsection-head">
            <h3>Models</h3>
            <button type="button" onClick={() => void handleRefreshModels()}><RefreshCw size={14} />Refresh</button>
          </div>
          <form className="inline-control" onSubmit={handleManualModel}>
            <input value={manualModelId} onChange={(event) => setManualModelId(event.target.value)} placeholder="modelId" />
            <button type="submit"><Plus size={14} />Add</button>
          </form>
          <div className="item-list">
            {models.map((model) => (
              <label key={model.id || model.modelId} className={`model-row ${model.enabled ? "selected" : ""}`}>
                <input type="checkbox" checked={model.enabled} onChange={(event) => void handleUpsertModel(model.modelId, event.target.checked, model.source)} />
                <span>{model.modelId}</span>
                <small>{model.source}</small>
              </label>
            ))}
            {models.length === 0 && <p className="empty-copy">No models configured.</p>}
          </div>
        </div>
      );
    }
    if (settingsTab === "skills") {
      return (
        <div className="page-body">
          <div className="navigator-actions">
            <button type="button" onClick={handleNewSkill}><Plus size={14} />New skill</button>
          </div>
          <div className="navigator-list">
            {skills.map((skill) => (
              <div className={`navigator-row-group ${skillEditorOpen && skillForm.slug === skill.definition.slug ? "active" : ""}`} key={skill.definition.slug}>
                <button type="button" className="navigator-row main-action" onClick={() => void handleLoadSkill(skill.definition.slug)}>
                  <FileText className="row-icon" size={15} />
                  <span>{skill.definition.slug}</span>
                  <small>{skill.definition.name || "skill"}</small>
                </button>
                <button type="button" className="icon-action danger-text" aria-label={`Delete ${skill.definition.slug}`} onClick={() => void handleDeleteSkill(skill.definition.slug)}>
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
            {skills.length === 0 && <p className="empty-copy">No workspace skills.</p>}
          </div>
          {skillEditorOpen && (
            <form className="skill-editor" onSubmit={handleSaveSkill}>
              <div className="editor-grid">
                <label>Slug<input value={skillForm.slug} onChange={(event) => setSkillForm({ ...skillForm, slug: event.target.value })} /></label>
                <label>Name<input value={skillForm.name} onChange={(event) => setSkillForm({ ...skillForm, name: event.target.value })} /></label>
                <label className="span-2">Description<input value={skillForm.description} onChange={(event) => setSkillForm({ ...skillForm, description: event.target.value })} /></label>
                <label className="span-2">File path<input value={skillForm.path} onChange={(event) => setSkillForm({ ...skillForm, path: event.target.value })} /></label>
              </div>
              <label className="code-editor-label">Content<textarea className="code-editor" value={skillForm.content} onChange={(event) => setSkillForm({ ...skillForm, content: event.target.value })} /></label>
              <div className="editor-footer">
                <span>Saved skills affect new sessions.</span>
                <button type="submit"><Save size={14} />Save skill</button>
              </div>
            </form>
          )}
        </div>
      );
    }
    return (
      <div className="page-body">
        <div className="navigator-actions">
          <button type="button" onClick={handleNewMCP}><Plus size={14} />New server</button>
        </div>
        <div className="navigator-list">
          {mcpServers.map((server) => (
            <div className={`navigator-row-group ${mcpEditorOpen && mcpForm.name === server.definition.name ? "active" : ""}`} key={server.definition.name}>
              <button type="button" className="navigator-row main-action" onClick={() => void handleLoadMCP(server.definition.name)}>
                <Server className="row-icon" size={15} />
                <span>{server.definition.name}</span>
                <small>{server.definition.transport || "stdio"}</small>
              </button>
              <button type="button" className="icon-action danger-text" aria-label={`Delete ${server.definition.name}`} onClick={() => void handleDeleteMCP(server.definition.name)}>
                <Trash2 size={14} />
              </button>
            </div>
          ))}
          {mcpServers.length === 0 && <p className="empty-copy">No MCP servers.</p>}
        </div>
        {mcpEditorOpen && (
          <form className="settings-form" onSubmit={handleSaveMCP}>
            <label>Name<input value={mcpForm.name} onChange={(event) => setMCPForm({ ...mcpForm, name: event.target.value })} /></label>
            <label>Command<input value={mcpForm.command} onChange={(event) => setMCPForm({ ...mcpForm, command: event.target.value })} /></label>
            <label>Args<textarea value={mcpForm.args} onChange={(event) => setMCPForm({ ...mcpForm, args: event.target.value })} placeholder="One argument per line" /></label>
            <label>Transport<input value={mcpForm.transport} onChange={(event) => setMCPForm({ ...mcpForm, transport: event.target.value })} /></label>
            <label>Env<textarea value={mcpForm.env} onChange={(event) => setMCPForm({ ...mcpForm, env: event.target.value })} placeholder="KEY=value" /></label>
            <button type="submit"><Save size={14} />Save MCP</button>
          </form>
        )}
      </div>
    );
  }

  if (activePanel === "settings") {
    return (
      <main className="workbench-shell">
        <div className="settings-shell">
          <nav className="settings-nav">
            <button type="button" className="settings-back-button" onClick={() => setActivePanel("sessions")}>← Back</button>
            <div className="settings-nav-separator" />
            <button type="button" className={`sidebar-item ${settingsTab === "runtime" ? "active" : ""}`} onClick={() => setSettingsTab("runtime")}>Runtime</button>
            <button type="button" className={`sidebar-item ${settingsTab === "llm" ? "active" : ""}`} onClick={() => setSettingsTab("llm")}>LLM &amp; Models</button>
            <button type="button" className={`sidebar-item ${settingsTab === "skills" ? "active" : ""}`} onClick={() => setSettingsTab("skills")}>Skills</button>
            <button type="button" className={`sidebar-item ${settingsTab === "mcp" ? "active" : ""}`} onClick={() => setSettingsTab("mcp")}>MCP Servers</button>
          </nav>
          <div className="settings-content">
            {notice && <div className={`notice ${notice.tone}`}>{notice.text}</div>}
            {renderSettingsContent()}
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="workbench-shell">
      <aside className="workspace-panel">
        <div className="app-menu-bar" />

        <div className="new-session-area">
          <button type="button" className="new-session-button" onClick={handleNewSession}>
            <SquarePen size={15} />
            New Session
          </button>
        </div>

        <div
          className="session-list"
          onScroll={(e) => {
            const el = e.currentTarget;
            if (sessionListHasMore && el.scrollHeight - el.scrollTop - el.clientHeight < 60) {
              void loadMoreSessions();
            }
          }}
        >
          {sessionList.map((s) => (
            <button
              key={s.sessionId}
              type="button"
              className={`session-list-item ${workbench.sessionId === s.sessionId ? "active" : ""}`}
              onClick={() => void loadSession(s.sessionId)}
            >
              <span className="session-list-title">{s.title || s.sessionId}</span>
              <small>{s.modelId || ""}</small>
            </button>
          ))}
          {sessionList.length === 0 && <p className="empty-copy">No sessions yet.</p>}
        </div>

        <div className="sidebar-bottom">
          <button type="button" className="settings-button" onClick={() => setActivePanel("settings")} aria-label="Settings">
            <Settings size={16} />
            <span>Settings</span>
          </button>
          <form className="workspace-switcher" onSubmit={handleWorkspaceSubmit}>
            <span className="workspace-avatar">{workspaceId.charAt(0).toUpperCase()}</span>
            <input value={workspaceDraft} onChange={(event) => setWorkspaceDraft(event.target.value)} aria-label="Workspace" />
            <button type="submit" aria-label="Open workspace"><ChevronDown size={14} /></button>
          </form>
        </div>
      </aside>

      <section className={`main-panel ${activePanel === "sessions" ? "chat-panel" : "content-panel"}`}>
        {renderMainPanel()}
      </section>
    </main>
  );
}
