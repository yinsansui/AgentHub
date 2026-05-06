import { ChevronDown, Settings, SquarePen } from "lucide-react";
import type { FormEvent } from "react";
import type { SessionProjection } from "../types";

type Props = {
  workspaceId: string;
  workspaceDraft: string;
  setWorkspaceDraft: (v: string) => void;
  onWorkspaceSubmit: (e: FormEvent) => void;
  sessionList: SessionProjection[];
  sessionListHasMore: boolean;
  activeSessionId: string | undefined;
  onLoadSession: (id: string) => void;
  onLoadMore: () => void;
  onNewSession: () => void;
  onOpenSettings: () => void;
};

export function SessionSidebar({
  workspaceId, workspaceDraft, setWorkspaceDraft, onWorkspaceSubmit,
  sessionList, sessionListHasMore, activeSessionId,
  onLoadSession, onLoadMore, onNewSession, onOpenSettings
}: Props) {
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
        {sessionList.length === 0 && <p className="empty-copy">暂无 session。</p>}
      </div>

      <div className="flex flex-col gap-1 p-2">
        <button type="button" className="w-full justify-start gap-2 min-h-8 px-2 py-[5px] rounded-md bg-transparent shadow-none text-[13px] text-apple-fg-50 hover:bg-apple-fg-5 hover:text-apple-fg" onClick={onOpenSettings} aria-label="设置">
          <Settings size={16} />
          <span>设置</span>
        </button>
        <form className="flex-1 min-w-0 flex items-center gap-1.5 h-[34px] px-2 rounded-lg bg-transparent hover:bg-apple-fg-5" onSubmit={onWorkspaceSubmit}>
          <span className="grid place-items-center w-4 h-4 rounded-full bg-apple-fg text-apple-bg text-[10px] font-semibold flex-shrink-0">{workspaceId.charAt(0).toUpperCase()}</span>
          <input className="min-w-0 p-0 bg-transparent shadow-none text-[13px]" value={workspaceDraft} onChange={(e) => setWorkspaceDraft(e.target.value)} aria-label="Workspace" />
          <button type="submit" className="min-h-7 w-7 p-0 bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5" aria-label="打开 Workspace"><ChevronDown size={14} /></button>
        </form>
      </div>
    </>
  );
}
