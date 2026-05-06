import { useCallback, useEffect, useMemo, useState } from "react";
import { errorMessage, sessionStorageKey } from "./lib/utils";
import { useWorkspace } from "./hooks/useWorkspace";
import { useSessionList } from "./hooks/useSessionList";
import { useChat } from "./hooks/useChat";
import { SessionSidebar } from "./components/SessionSidebar";
import { ChatPanel } from "./components/ChatPanel";
import { SettingsPanel } from "./components/SettingsPanel";
import type { Notice } from "./hooks/useWorkspace";
import type { FormEvent } from "react";

const defaultWorkspaceId = "ws_dev";

type NavigationPanel = "sessions" | "settings";
type SettingsTab = "llm" | "skills" | "mcp";

export default function App() {
  const initialWorkspaceId = useMemo(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get("workspaceId")?.trim() || defaultWorkspaceId;
  }, []);

  const [workspaceId, setWorkspaceId] = useState(initialWorkspaceId);
  const [workspaceDraft, setWorkspaceDraft] = useState(initialWorkspaceId);
  const [activePanel, setActivePanel] = useState<NavigationPanel>("sessions");
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("llm");

  const workspace = useWorkspace(workspaceId);
  const sessions = useSessionList(workspaceId);

  const reportError = useCallback(
    (error: unknown, fallback: string) => {
      workspace.setNotice({ tone: "error", text: errorMessage(error, fallback) });
    },
    [workspace.setNotice]
  );

  const chat = useChat(workspaceId, reportError);

  useEffect(() => {
    if (workspace.notice?.tone !== "success") return;
    const timer = window.setTimeout(() => workspace.setNotice(null), 3000);
    return () => window.clearTimeout(timer);
  }, [workspace.notice, workspace.setNotice]);

  useEffect(() => {
    void workspace.refreshWorkspace();
    sessions.resetSessions();
    void sessions.loadMoreSessions(true);
    chat.resetWorkbench();
    const stored = sessionStorage.getItem(sessionStorageKey(workspaceId));
    if (stored) {
      void chat.loadSession(stored);
    }
    return () => chat.disconnectStream();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspaceId]);

  function handleWorkspaceSubmit(event: FormEvent) {
    event.preventDefault();
    const next = workspaceDraft.trim() || defaultWorkspaceId;
    setWorkspaceId(next);
    const url = new URL(window.location.href);
    url.searchParams.set("workspaceId", next);
    window.history.replaceState(null, "", url);
  }

  function handleNewSession() {
    setActivePanel("sessions");
    chat.resetWorkbench();
  }

  function handleLoadSession(sessionId: string) {
    setActivePanel("sessions");
    void chat.loadSession(sessionId);
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
            workspaceId={workspaceId}
            workspaceDraft={workspaceDraft}
            setWorkspaceDraft={setWorkspaceDraft}
            onWorkspaceSubmit={handleWorkspaceSubmit}
            sessionList={sessions.sessionList}
            sessionListHasMore={sessions.sessionListHasMore}
            activeSessionId={chat.workbench.sessionId}
            onLoadSession={handleLoadSession}
            onLoadMore={() => void sessions.loadMoreSessions()}
            onNewSession={handleNewSession}
            onOpenSettings={() => setActivePanel("settings")}
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
