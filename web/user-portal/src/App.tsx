import { useCallback, useEffect, useState } from "react";
import { getMe, logout, updateWorkspace, deleteWorkspace } from "./api";
import { errorMessage, sessionStorageKey } from "./lib/utils";
import { useWorkspace } from "./hooks/useWorkspace";
import { useSessionList } from "./hooks/useSessionList";
import { useWorkspaceList } from "./hooks/useWorkspaceList";
import { useChat } from "./hooks/useChat";
import { SessionSidebar } from "./components/SessionSidebar";
import { ChatPanel } from "./components/ChatPanel";
import { LoginPage } from "./components/LoginPage";
import { SettingsPanel } from "./components/SettingsPanel";
import type { Notice } from "./hooks/useWorkspace";
import type { CurrentUser, WorkspaceProjection } from "./types";

type NavigationPanel = "sessions" | "settings";
type SettingsTab = "llm" | "skills" | "mcp" | "workspace";

function resolveInitialWorkspaceId(workspaces: WorkspaceProjection[]): string {
  const params = new URLSearchParams(window.location.search);
  const fromUrl = params.get("workspaceId")?.trim();
  if (fromUrl && workspaces.some((workspace) => workspace.id === fromUrl)) return fromUrl;
  if (workspaces.length > 0) return workspaces[0].id;
  return "";
}

function syncWorkspaceUrl(workspaceId: string) {
  const url = new URL(window.location.href);
  url.searchParams.set("workspaceId", workspaceId);
  window.history.replaceState(null, "", url);
}

export default function App() {
  const [currentUser, setCurrentUser] = useState<CurrentUser | null>(null);
  const [authChecked, setAuthChecked] = useState(false);
  const workspaceList = useWorkspaceList();
  const [activeWorkspaceId, setActiveWorkspaceId] = useState("");
  const [activePanel, setActivePanel] = useState<NavigationPanel>("sessions");
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("llm");

  const workspace = useWorkspace(activeWorkspaceId);
  const sessions = useSessionList(activeWorkspaceId);

  useEffect(() => {
    let cancelled = false;
    void getMe()
      .then((payload) => {
        if (!cancelled) setCurrentUser(payload.user);
      })
      .catch(() => {
        if (!cancelled) setCurrentUser(null);
      })
      .finally(() => {
        if (!cancelled) setAuthChecked(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!currentUser) return;
    if (workspaceList.loaded) return;
    void workspaceList.loadWorkspaces();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentUser]);

  useEffect(() => {
    if (!currentUser) return;
    if (!workspaceList.loaded) return;
    const resolved = resolveInitialWorkspaceId(workspaceList.workspaces);
    if (!resolved) return;
    if (resolved !== activeWorkspaceId) setActiveWorkspaceId(resolved);
    syncWorkspaceUrl(resolved);
  }, [currentUser, activeWorkspaceId, workspaceList.loaded, workspaceList.workspaces]);



  const reportError = useCallback(
    (error: unknown, fallback: string) => {
      workspace.setNotice({ tone: "error", text: errorMessage(error, fallback) });
    },
    [workspace.setNotice]
  );

  const handleSessionCreated = useCallback((session: { sessionId: string; taskId: string; workspaceId: string; modelId?: string; title?: string }) => {
    sessions.prependSession(session);
  }, [sessions]);

  const chat = useChat(currentUser?.id ?? "", activeWorkspaceId, reportError, handleSessionCreated);

  useEffect(() => {
    if (workspace.notice?.tone !== "success") return;
    const timer = window.setTimeout(() => workspace.setNotice(null), 3000);
    return () => window.clearTimeout(timer);
  }, [workspace.notice, workspace.setNotice]);

  useEffect(() => {
    if (!currentUser) return;
    if (!activeWorkspaceId) return;
    void workspace.refreshWorkspace();
    sessions.resetSessions();
    void sessions.loadMoreSessions(true);
    chat.resetWorkbench();
    const stored = sessionStorage.getItem(sessionStorageKey(currentUser.id, activeWorkspaceId));
    if (stored) {
      void chat.loadSession(stored);
    }
    return () => chat.disconnectStream();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentUser, activeWorkspaceId]);

  async function handleLogout() {
    await logout();
    sessionStorage.clear();
    chat.disconnectStream();
    chat.resetWorkbench();
    setActiveWorkspaceId("");
    setCurrentUser(null);
    setAuthChecked(true);
  }

  function handleSelectWorkspace(nextId: string) {
    setActiveWorkspaceId(nextId);
    syncWorkspaceUrl(nextId);
  }

  function handleNewSession() {
    setActivePanel("sessions");
    chat.resetWorkbench();
  }

  function handleLoadSession(sessionId: string) {
    setActivePanel("sessions");
    void chat.loadSession(sessionId);
  }

  async function handleUpdateCurrentWorkspace(id: string, name: string) {
    await updateWorkspace(id, name);
    await workspaceList.loadWorkspaces();
  }

  async function handleDeleteCurrentWorkspace(id: string) {
    const result = await deleteWorkspace(id);
    const nextId = result.replacementWorkspace?.id || workspaceList.workspaces.find((w) => w.id !== id)?.id || "";
    await workspaceList.loadWorkspaces();
    if (nextId) handleSelectWorkspace(nextId);
    return result;
  }

  if (!authChecked) {
    return <main className="login-shell"><p className="text-apple-fg-50">正在检查登录状态…</p></main>;
  }

  if (!currentUser) {
    return <LoginPage onAuthenticated={setCurrentUser} />;
  }

  if (activePanel === "settings") {
    return (
      <>
        <NoticeToast notice={workspace.notice} />
        <SettingsPanel
          settingsTab={settingsTab}
          setSettingsTab={setSettingsTab}
          onBack={() => setActivePanel("sessions")}
          llmConnection={workspace.llmConnection}
          llmForm={workspace.llmForm}
          setLLMForm={workspace.setLLMForm}
          onSaveLLM={workspace.handleSaveLLM}
          models={workspace.models}
          manualModelId={workspace.manualModelId}
          setManualModelId={workspace.setManualModelId}
          onRefreshModels={workspace.handleRefreshModels}
          onUpsertModel={workspace.handleUpsertModel}
          onManualModel={workspace.handleManualModel}
          skills={workspace.skills}
          skillForm={workspace.skillForm}
          setSkillForm={workspace.setSkillForm}
          skillEditorOpen={workspace.skillEditorOpen}
          onNewSkill={workspace.handleNewSkill}
          onLoadSkill={workspace.handleLoadSkill}
          onSaveSkill={workspace.handleSaveSkill}
          onDeleteSkill={workspace.handleDeleteSkill}
          onCloseSkillEditor={workspace.handleCloseSkillEditor}
          mcpServers={workspace.mcpServers}
          mcpForm={workspace.mcpForm}
          setMCPForm={workspace.setMCPForm}
          mcpEditorOpen={workspace.mcpEditorOpen}
          onNewMCP={workspace.handleNewMCP}
          onLoadMCP={workspace.handleLoadMCP}
          onSaveMCP={workspace.handleSaveMCP}
          onDeleteMCP={workspace.handleDeleteMCP}
          onCloseMCPEditor={workspace.handleCloseMCPEditor}
          activeWorkspace={workspaceList.workspaces.find((w) => w.id === activeWorkspaceId)}
          onUpdateWorkspace={handleUpdateCurrentWorkspace}
          onDeleteWorkspace={handleDeleteCurrentWorkspace}
        />
      </>
    );
  }

  const enabledModels = workspace.models.filter((m) => m.enabled);

  return (
    <>
      <NoticeToast notice={workspace.notice} />
      <main className="grid h-screen min-h-[720px] grid-cols-[220px_minmax(520px,1fr)] gap-2 p-2 max-[700px]:grid-cols-1 max-[700px]:p-0">
        <aside className="min-w-0 min-h-0 flex flex-col gap-0 p-0 bg-transparent max-[700px]:rounded-none">
          <SessionSidebar
            activeWorkspaceId={activeWorkspaceId}
            workspaces={workspaceList.workspaces}
            workspacesLoading={workspaceList.loading}
            workspacesLoaded={workspaceList.loaded}
            onLoadWorkspaces={workspaceList.loadWorkspaces}
            onSelectWorkspace={handleSelectWorkspace}
            sessionList={sessions.sessionList}
            sessionListHasMore={sessions.sessionListHasMore}
            sessionListLoading={sessions.loading}
            sessionListError={sessions.error}
            activeSessionId={chat.workbench.sessionId}
            onLoadSession={handleLoadSession}
            onLoadMore={() => void sessions.loadMoreSessions()}
            onNewSession={handleNewSession}
            onOpenSettings={() => setActivePanel("settings")}
            user={currentUser}
            onLogout={() => void handleLogout()}
          />
        </aside>
        <section className="min-w-0 min-h-0 bg-apple-panel shadow-apple-card overflow-hidden rounded-2xl grid grid-rows-[auto_minmax(0,1fr)_auto] relative max-[700px]:rounded-none max-[700px]:min-h-[78vh]">
          <ChatPanel
            workbench={chat.workbench}
            enabledModels={enabledModels}
            messageDraft={chat.messageDraft}
            setMessageDraft={chat.setMessageDraft}
            selectedModelId={chat.selectedModelId}
            setSelectedModelId={chat.setSelectedModelId}
            onSubmit={chat.handleCreateOrTurn}
            onStop={chat.handleStop}
          />
        </section>
      </main>
    </>
  );
}

function NoticeToast({ notice }: { notice: Notice | null }) {
  if (!notice) return null;

  return (
    <div className={`notice-toast ${notice.tone}`} role={notice.tone === "error" ? "alert" : "status"}>
      {notice.text}
    </div>
  );
}
