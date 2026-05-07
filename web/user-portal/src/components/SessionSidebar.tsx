import { useEffect, useRef, useState } from "react";
import { ChevronDown, LogOut, Settings, SquarePen, Plus, Pencil, Trash2 } from "lucide-react";
import { createWorkspace, updateWorkspace, deleteWorkspace } from "../api";
import type { CurrentUser, SessionProjection, WorkspaceProjection } from "../types";

type Props = {
  activeWorkspaceId: string;
  workspaces: WorkspaceProjection[];
  workspacesLoading: boolean;
  workspacesLoaded: boolean;
  onLoadWorkspaces: () => Promise<void>;
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
  user: CurrentUser;
  onLogout: () => void;
};

export function SessionSidebar({
  activeWorkspaceId,
  workspaces, workspacesLoading, workspacesLoaded, onLoadWorkspaces, onSelectWorkspace,
  sessionList, sessionListHasMore, sessionListLoading, sessionListError,
  activeSessionId, onLoadSession, onLoadMore, onNewSession, onOpenSettings, user, onLogout
}: Props) {
  const [workspaceMenuOpen, setWorkspaceMenuOpen] = useState(false);
  const [menuMode, setMenuMode] = useState<"list" | "create" | "rename" | "delete">("list");
  const [formName, setFormName] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [formLoading, setFormLoading] = useState(false);
  const [editingWorkspace, setEditingWorkspace] = useState<WorkspaceProjection | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const activeWorkspace = workspaces.find((workspace) => workspace.id === activeWorkspaceId);
  const activeWorkspaceName = activeWorkspace?.name || "选择 Workspace";

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

  useEffect(() => {
    if (!workspaceMenuOpen) {
      const timer = setTimeout(() => {
        setMenuMode("list");
        setFormName("");
        setFormError(null);
        setEditingWorkspace(null);
      }, 150);
      return () => clearTimeout(timer);
    }
  }, [workspaceMenuOpen]);

  async function handleCreateSubmit(event: React.FormEvent) {
    event.preventDefault();
    const name = formName.trim();
    if (!name) {
      setFormError("请输入 workspace 名称");
      return;
    }
    setFormLoading(true);
    setFormError(null);
    try {
      const result = await createWorkspace(name);
      await onLoadWorkspaces();
      onSelectWorkspace(result.workspace.id);
      setWorkspaceMenuOpen(false);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "创建失败");
    } finally {
      setFormLoading(false);
    }
  }

  async function handleRenameSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (!editingWorkspace) return;
    const name = formName.trim();
    if (!name) {
      setFormError("请输入 workspace 名称");
      return;
    }
    setFormLoading(true);
    setFormError(null);
    try {
      await updateWorkspace(editingWorkspace.id, name);
      await onLoadWorkspaces();
      setWorkspaceMenuOpen(false);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "重命名失败");
    } finally {
      setFormLoading(false);
    }
  }

  async function handleDeleteConfirm() {
    if (!editingWorkspace) return;
    setFormLoading(true);
    setFormError(null);
    try {
      const result = await deleteWorkspace(editingWorkspace.id);
      await onLoadWorkspaces();
      if (activeWorkspaceId === editingWorkspace.id) {
        const nextId = result.replacementWorkspace?.id || workspaces.find((w) => w.id !== editingWorkspace.id)?.id || "";
        if (nextId) onSelectWorkspace(nextId);
      }
      setWorkspaceMenuOpen(false);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "删除失败");
    } finally {
      setFormLoading(false);
    }
  }

  return (
    <>
      <div className="flex items-center justify-between gap-2 h-[52px] px-4 border-b border-black/[0.06]">
        <div className="min-w-0">
          <p className="m-0 text-[13px] font-medium text-apple-fg truncate">{user.username}</p>
          <p className="m-0 text-[11px] text-apple-fg-50 truncate">{user.role}</p>
        </div>
        <button type="button" className="min-h-8 w-8 p-0 bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5" onClick={onLogout} aria-label="退出登录">
          <LogOut size={15} />
        </button>
      </div>

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
            data-testid="workspace-menu-trigger"
            className="w-full flex items-center gap-1.5 min-h-8 px-2 py-[5px] rounded-md bg-transparent shadow-none text-[13px] text-apple-fg-50 hover:bg-apple-fg-5 hover:text-apple-fg"
            onClick={() => setWorkspaceMenuOpen((v) => !v)}
          >
            <span className="grid place-items-center w-4 h-4 rounded-full bg-apple-fg text-apple-bg text-[10px] font-semibold flex-shrink-0">{activeWorkspaceName.charAt(0).toUpperCase()}</span>
            <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap flex-1 text-left">{activeWorkspaceName}</span>
            <ChevronDown size={14} className={`flex-shrink-0 transition-transform duration-150 ${workspaceMenuOpen ? "rotate-180" : ""}`} />
          </button>
          {workspaceMenuOpen && (
            <div className="absolute bottom-full left-0 right-0 mb-1 p-1.5 rounded-xl bg-apple-panel shadow-apple-card border border-black/[0.06] max-h-[240px] overflow-y-auto z-50">
              {workspacesLoading && workspaces.length === 0 && (
                <div className="py-3 text-center text-apple-fg-50 text-[13px]">加载中…</div>
              )}

              {menuMode === "list" && (
                <>
                  {workspaces.map((workspace) => (
                    <div
                      key={workspace.id}
                      className={`w-full flex items-center gap-2 px-2.5 py-[7px] rounded-lg text-left text-[13px] ${workspace.id === activeWorkspaceId ? "bg-apple-accent/10 text-apple-accent" : "text-apple-fg hover:bg-apple-bg"}`}
                    >
                      <button
                        type="button"
                        className="flex items-center gap-2 flex-1 min-w-0 text-left"
                        onClick={() => {
                          onSelectWorkspace(workspace.id);
                          setWorkspaceMenuOpen(false);
                        }}
                      >
                        <span className="grid place-items-center w-4 h-4 rounded-full bg-apple-fg text-apple-bg text-[10px] font-semibold flex-shrink-0">{workspace.name.charAt(0).toUpperCase()}</span>
                        <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{workspace.name}</span>
                      </button>
                      <div className="flex items-center gap-0.5 flex-shrink-0">
                        <button
                          type="button"
                          className="w-6 h-6 p-0 bg-transparent shadow-none text-apple-fg-40 hover:text-apple-accent rounded-md"
                          aria-label="重命名"
                          onClick={(e) => {
                            e.stopPropagation();
                            setEditingWorkspace(workspace);
                            setFormName(workspace.name);
                            setFormError(null);
                            setMenuMode("rename");
                          }}
                        >
                          <Pencil size={12} />
                        </button>
                        <button
                          type="button"
                          className="w-6 h-6 p-0 bg-transparent shadow-none text-apple-fg-40 hover:text-apple-destructive rounded-md"
                          aria-label="删除"
                          onClick={(e) => {
                            e.stopPropagation();
                            setEditingWorkspace(workspace);
                            setFormError(null);
                            setMenuMode("delete");
                          }}
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    </div>
                  ))}
                  <button
                    type="button"
                    className="w-full flex items-center gap-2 px-2.5 py-[7px] rounded-lg text-left text-[13px] text-apple-accent hover:bg-apple-accent/5 mt-0.5"
                    onClick={() => {
                      setFormName("");
                      setFormError(null);
                      setMenuMode("create");
                    }}
                  >
                    <Plus size={14} />
                    <span>新建 Workspace</span>
                  </button>
                </>
              )}

              {menuMode === "create" && (
                <form onSubmit={handleCreateSubmit} className="p-1">
                  <input
                    autoFocus
                    placeholder="Workspace 名称"
                    value={formName}
                    onChange={(e) => setFormName(e.target.value)}
                    className="w-full text-[13px] px-2.5 py-[7px] rounded-lg bg-apple-bg shadow-none border border-black/[0.08]"
                  />
                  {formError && <p className="text-apple-destructive-text text-[12px] mt-1.5 px-0.5">{formError}</p>}
                  <div className="flex items-center justify-end gap-2 mt-2">
                    <button
                      type="button"
                      className="min-h-7 px-2.5 py-0 text-[12px] bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5 rounded-lg"
                      onClick={() => {
                        setMenuMode("list");
                        setFormName("");
                        setFormError(null);
                      }}
                    >
                      取消
                    </button>
                    <button
                      type="submit"
                      disabled={formLoading}
                      className="min-h-7 px-2.5 py-0 text-[12px] bg-apple-accent text-white shadow-none hover:bg-apple-accent-hover rounded-lg"
                    >
                      {formLoading ? "创建中…" : "创建"}
                    </button>
                  </div>
                </form>
              )}

              {menuMode === "rename" && editingWorkspace && (
                <form onSubmit={handleRenameSubmit} className="p-1">
                  <input
                    autoFocus
                    placeholder="Workspace 名称"
                    value={formName}
                    onChange={(e) => setFormName(e.target.value)}
                    className="w-full text-[13px] px-2.5 py-[7px] rounded-lg bg-apple-bg shadow-none border border-black/[0.08]"
                  />
                  {formError && <p className="text-apple-destructive-text text-[12px] mt-1.5 px-0.5">{formError}</p>}
                  <div className="flex items-center justify-end gap-2 mt-2">
                    <button
                      type="button"
                      className="min-h-7 px-2.5 py-0 text-[12px] bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5 rounded-lg"
                      onClick={() => {
                        setMenuMode("list");
                        setFormName("");
                        setFormError(null);
                        setEditingWorkspace(null);
                      }}
                    >
                      取消
                    </button>
                    <button
                      type="submit"
                      disabled={formLoading}
                      className="min-h-7 px-2.5 py-0 text-[12px] bg-apple-accent text-white shadow-none hover:bg-apple-accent-hover rounded-lg"
                    >
                      {formLoading ? "保存中…" : "保存"}
                    </button>
                  </div>
                </form>
              )}

              {menuMode === "delete" && editingWorkspace && (
                <div className="p-1">
                  <p className="text-[13px] text-apple-fg px-0.5">
                    确定删除 <span className="font-medium">{editingWorkspace.name}</span>？
                  </p>
                  {formError && <p className="text-apple-destructive-text text-[12px] mt-1.5 px-0.5">{formError}</p>}
                  <div className="flex items-center justify-end gap-2 mt-2">
                    <button
                      type="button"
                      className="min-h-7 px-2.5 py-0 text-[12px] bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5 rounded-lg"
                      onClick={() => {
                        setMenuMode("list");
                        setFormError(null);
                        setEditingWorkspace(null);
                      }}
                    >
                      取消
                    </button>
                    <button
                      type="button"
                      disabled={formLoading}
                      className="min-h-7 px-2.5 py-0 text-[12px] bg-apple-destructive text-white shadow-none hover:opacity-90 rounded-lg"
                      onClick={handleDeleteConfirm}
                    >
                      {formLoading ? "删除中…" : "删除"}
                    </button>
                  </div>
                </div>
              )}

              {!workspacesLoading && workspaces.length === 0 && menuMode === "list" && (
                <div className="py-3 text-center text-apple-fg-50 text-[13px]">暂无 workspace</div>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  );
}
