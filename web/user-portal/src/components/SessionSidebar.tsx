import { useEffect, useRef, useState } from "react";
import { ChevronDown, Plus, Settings, SquarePen } from "lucide-react";
import type { FormEvent } from "react";
import type { SessionProjection, WorkspaceProjection } from "../types";

type Props = {
  workspaceId: string;
  workspaceDraft: string;
  setWorkspaceDraft: (v: string) => void;
  onWorkspaceSubmit: (e: FormEvent) => void;
  workspaces: WorkspaceProjection[];
  workspacesLoading: boolean;
  workspacesLoaded: boolean;
  onLoadWorkspaces: () => void;
  onSelectWorkspace: (id: string) => void;
  sessionList: SessionProjection[];
  sessionListHasMore: boolean;
  sessionListLoading: boolean;
  sessionListError: string | null;
  activeSessionId: string | undefined;
  onLoadSession: (id: string) => void;
  onLoadMore: () => void;
  onNewSession: () => void;
  onOpenSettings: () => void;
};

export function SessionSidebar({
  workspaceId, workspaceDraft, setWorkspaceDraft, onWorkspaceSubmit,
  workspaces, workspacesLoading, workspacesLoaded, onLoadWorkspaces, onSelectWorkspace,
  sessionList, sessionListHasMore, sessionListLoading, sessionListError,
  activeSessionId, onLoadSession, onLoadMore, onNewSession, onOpenSettings
}: Props) {
  const [workspaceMenuOpen, setWorkspaceMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setWorkspaceMenuOpen(false);
      }
    }
    if (workspaceMenuOpen) {
      document.addEventListener("mousedown", handleClickOutside);
      return () => document.removeEventListener("mousedown", handleClickOutside);
    }
  }, [workspaceMenuOpen]);

  useEffect(() => {
    if (workspaceMenuOpen && !workspacesLoaded && !workspacesLoading) {
      onLoadWorkspaces();
    }
  }, [workspaceMenuOpen, workspacesLoaded, workspacesLoading, onLoadWorkspaces]);

  return (
    <>
      <div className="flex items-center gap-[5px] h-[52px] px-4 border-b border-black/[0.06]" />

      <div className="px-2 pt-1 pb-2">
        <button type="button" className="w-full justify-start min-h-9 px-2.5 py-[7px] rounded-[10px] bg-apple-panel shadow-apple text-[13px] font-normal hover:bg-white/90" onClick={onNewSession}>
          <SquarePen size={15} />
          新 Session
        </button>
      </div>

      <div
        className="flex-1 overflow-y-auto px-2 flex flex-col gap-0.5 min-h-0"
        onScroll={(e) => {
          const el = e.currentTarget;
          if (sessionListHasMore && el.scrollHeight - el.scrollTop - el.clientHeight < 60) {
            onLoadMore();
          }
        }}
      >
        {sessionList.map((s) => (
          <button
            key={s.sessionId}
            type="button"
            className={`w-full min-h-[52px] flex flex-col items-start gap-0.5 px-2.5 py-2 rounded-[10px] bg-transparent shadow-none text-left text-[13px] hover:bg-black/5 ${activeSessionId === s.sessionId ? "bg-black/[0.07]" : ""}`}
            onClick={() => onLoadSession(s.sessionId)}
          >
            <span className="w-full overflow-hidden text-ellipsis whitespace-nowrap font-medium">{s.title || s.sessionId}</span>
            <small className="text-apple-fg-50 text-[11px] overflow-hidden text-ellipsis whitespace-nowrap w-full">{s.modelId || ""}</small>
          </button>
        ))}
        {sessionListLoading && sessionList.length === 0 && (
          <div className="py-8 text-center text-apple-fg-50 text-[13px]">加载中…</div>
        )}
        {sessionListError && sessionList.length === 0 && (
          <div className="py-6 px-3 text-center">
            <p className="text-apple-destructive-text text-[13px] mb-2">{sessionListError}</p>
            <button type="button" className="text-apple-accent text-[13px] hover:underline" onClick={() => onLoadMore()}>重试</button>
          </div>
        )}
        {!sessionListLoading && !sessionListError && sessionList.length === 0 && (
          <p className="empty-copy">暂无 session。</p>
        )}
        {sessionListLoading && sessionList.length > 0 && (
          <div className="py-3 text-center text-apple-fg-50 text-[12px]">加载更多…</div>
        )}
        {!sessionListHasMore && sessionList.length > 0 && (
          <div className="py-3 text-center text-apple-fg-50 text-[12px]">没有更多了</div>
        )}
      </div>

      <div className="flex flex-col gap-1 p-2">
        <button type="button" className="w-full justify-start gap-2 min-h-8 px-2 py-[5px] rounded-md bg-transparent shadow-none text-[13px] text-apple-fg-50 hover:bg-apple-fg-5 hover:text-apple-fg" onClick={onOpenSettings} aria-label="设置">
          <Settings size={16} />
          <span>设置</span>
        </button>
        <div className="relative" ref={menuRef}>
          <button
            type="button"
            className="w-full flex items-center gap-1.5 min-h-8 px-2 py-[5px] rounded-md bg-transparent shadow-none text-[13px] text-apple-fg-50 hover:bg-apple-fg-5 hover:text-apple-fg"
            onClick={() => setWorkspaceMenuOpen((v) => !v)}
          >
            <span className="grid place-items-center w-4 h-4 rounded-full bg-apple-fg text-apple-bg text-[10px] font-semibold flex-shrink-0">{workspaceId.charAt(0).toUpperCase()}</span>
            <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap flex-1 text-left">{workspaces.find((ws) => ws.workspaceId === workspaceId)?.name || workspaceId}</span>
            <ChevronDown size={14} className={`flex-shrink-0 transition-transform duration-150 ${workspaceMenuOpen ? "rotate-180" : ""}`} />
          </button>
          {workspaceMenuOpen && (
            <div className="absolute bottom-full left-0 right-0 mb-1 p-1.5 rounded-xl bg-apple-panel shadow-apple-card border border-black/[0.06] max-h-[240px] overflow-y-auto z-50">
              {workspacesLoading && workspaces.length === 0 && (
                <div className="py-3 text-center text-apple-fg-50 text-[13px]">加载中…</div>
              )}
                {workspaces.map((ws) => (
                  <button
                    key={ws.workspaceId}
                    type="button"
                    className={`w-full flex items-center gap-2 px-2.5 py-[7px] rounded-lg text-left text-[13px] ${ws.workspaceId === workspaceId ? "bg-apple-accent/10 text-apple-accent" : "text-apple-fg hover:bg-apple-bg"}`}
                    onClick={() => {
                      onSelectWorkspace(ws.workspaceId);
                      setWorkspaceMenuOpen(false);
                    }}
                  >
                    <span className="grid place-items-center w-4 h-4 rounded-full bg-apple-fg text-apple-bg text-[10px] font-semibold flex-shrink-0">{ws.workspaceId.charAt(0).toUpperCase()}</span>
                    <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{ws.name || ws.workspaceId}</span>
                  </button>
                ))}
              <div className="border-t border-black/[0.06] my-1" />
              <form
                className="flex items-center gap-1.5 px-2 py-1"
                onSubmit={(e) => {
                  onWorkspaceSubmit(e);
                  setWorkspaceMenuOpen(false);
                }}
              >
                <input
                  className="min-w-0 flex-1 p-0 bg-transparent shadow-none text-[13px] placeholder:text-apple-fg-50"
                  value={workspaceDraft}
                  onChange={(e) => setWorkspaceDraft(e.target.value)}
                  placeholder="输入 workspace ID"
                  aria-label="Workspace"
                />
                <button type="submit" className="min-h-7 w-7 p-0 bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5" aria-label="切换 Workspace">
                  <Plus size={14} />
                </button>
              </form>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
