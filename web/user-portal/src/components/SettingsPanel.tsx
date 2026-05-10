import { Bot, ChevronDown, ChevronRight, FileText, Folder, FolderOpen, FolderPlus, FilePlus, Plus, Puzzle, RefreshCw, Save, Search, Server, Trash2, LayoutGrid, Package } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { FormEvent, KeyboardEvent, ReactNode } from "react";
import type { LLMConnection, LLMModel, MCPServerDefinitionWithEnv, SkillDefinitionWithFiles, WorkspacePlugin, WorkspaceProjection } from "../types";
import type { SkillEditorState } from "../hooks/useWorkspace";
import { PageShell, SectionCard } from "./settings/layout";
import { PluginsTab } from "./settings/plugins/PluginsTab";

type SettingsTab = "llm" | "skills" | "mcp" | "workspace" | "plugins";

const API_PROTOCOL_OPTIONS = [
  { value: "anthropic-messages", label: "Anthropic Messages" },
  { value: "openai-completions", label: "OpenAI Chat Completions" },
  { value: "azure-chat", label: "Azure Chat" },
  { value: "google-generative", label: "Google Generative" },
  { value: "bedrock-converse", label: "Bedrock Converse" },
  { value: "ollama-chat", label: "Ollama Chat" },
];

type Props = {
  settingsTab: SettingsTab;
  setSettingsTab: (tab: SettingsTab) => void;
  onBack: () => void;
  llmConnection: LLMConnection | null;
  apiKeySet: boolean;
  llmForm: { provider: string; apiProtocol: string; baseUrl: string; apiKey: string };
  setLLMForm: (v: { provider: string; apiProtocol: string; baseUrl: string; apiKey: string }) => void;
  onSaveLLM: (e: FormEvent) => void;
  models: LLMModel[];
  manualModelId: string;
  setManualModelId: (v: string) => void;
  onRefreshModels: () => void;
  onUpsertModel: (modelId: string, enabled: boolean, source?: string) => void;
  onManualModel: (e: FormEvent) => void;
  onSetDefaultModel: (modelId: string) => void;
  skills: SkillDefinitionWithFiles[];
  skillEditor: SkillEditorState;
  setSkillEditor: (v: SkillEditorState) => void;
  skillEditorOpen: boolean;
  onNewSkill: () => void;
  onLoadSkill: (slug: string) => void;
  onSaveSkill: (e: FormEvent) => void;
  onDeleteSkill: (slug: string) => void;
  onCloseSkillEditor: () => void;
  mcpServers: MCPServerDefinitionWithEnv[];
  mcpForm: { name: string; command: string; args: string; transport: string; env: string };
  setMCPForm: (v: { name: string; command: string; args: string; transport: string; env: string }) => void;
  mcpEditorOpen: boolean;
  onNewMCP: () => void;
  onLoadMCP: (name: string) => void;
  onSaveMCP: (e: FormEvent) => void;
  onDeleteMCP: (name: string) => void;
  onCloseMCPEditor: () => void;
  activeWorkspace: WorkspaceProjection | undefined;
  onUpdateWorkspace: (id: string, name: string) => Promise<void>;
  onDeleteWorkspace: (id: string) => Promise<{ deleted: boolean; replacementWorkspace?: WorkspaceProjection }>;
  plugins: WorkspacePlugin[];
  onSavePlugin: (pluginId: string, config: Record<string, unknown>) => void;
};

type SettingsTabMeta = {
  tab: SettingsTab;
  label: string;
  icon: ReactNode;
};

const settingsTabs: SettingsTabMeta[] = [
  { tab: "llm", label: "LLM", icon: <Bot size={16} /> },
  { tab: "skills", label: "Skill", icon: <Puzzle size={16} /> },
  { tab: "mcp", label: "MCP", icon: <Server size={16} /> },
  { tab: "plugins", label: "Plugins", icon: <Package size={16} /> },
  { tab: "workspace", label: "Workspace", icon: <LayoutGrid size={16} /> }
];

function Field(props: { label: string; span?: boolean; children: ReactNode }) {
  return (
    <label className={`settings-field ${props.span ? "settings-field-span" : ""}`}>
      <span>{props.label}</span>
      {props.children}
    </label>
  );
}

function CustomSelect(props: {
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  const { value, options, onChange } = props;
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const selectedLabel = options.find((o) => o.value === value)?.label ?? value;

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (ref.current && !ref.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  return (
    <div className="custom-select" ref={ref}>
      <button
        type="button"
        className="custom-select-trigger"
        onClick={() => setOpen((prev) => !prev)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span>{selectedLabel}</span>
        <ChevronDown size={14} />
      </button>
      {open && (
        <ul className="custom-select-dropdown" role="listbox">
          {options.map((option) => (
            <li
              key={option.value}
              role="option"
              aria-selected={option.value === value}
              className={`custom-select-option ${option.value === value ? "selected" : ""}`}
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onChange(option.value);
                setOpen(false);
              }}
            >
              {option.label}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function slugify(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

function isDirtyEditor(editor: SkillEditorState): boolean {
  if (editor.files.length !== editor.savedFiles.length) return true;
  return editor.files.some((f, i) => {
    const saved = editor.savedFiles[i];
    return !saved || f.path !== saved.path || f.content !== saved.content;
  });
}

function cleanSkillPath(value: string): string {
  return value.trim().replace(/\\/g, "/").replace(/^\/+|\/+$/g, "").replace(/\/+/g, "/");
}

function parentDirectory(path: string): string {
  if (!path.includes("/")) return "";
  return path.slice(0, path.lastIndexOf("/"));
}

function pathHasUnsafeSegment(path: string): boolean {
  return path.split("/").some((part) => !part || part === "." || part === "..");
}

function CreateSkillDialog(props: {
  open: boolean;
  creating: boolean;
  onClose: () => void;
  onCreate: (payload: { slug: string; name: string; description: string }) => void;
}) {
  const nameRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [description, setDescription] = useState("");
  const suggestedSlug = useMemo(() => slugify(name), [name]);
  const effectiveSlug = slug.trim() || suggestedSlug;

  useEffect(() => {
    if (!props.open) { setName(""); setSlug(""); setDescription(""); return; }
    const t = setTimeout(() => nameRef.current?.focus(), 0);
    return () => clearTimeout(t);
  }, [props.open]);

  if (!props.open) return null;

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim() || !effectiveSlug) return;
    props.onCreate({ slug: effectiveSlug, name: name.trim(), description: description.trim() || "新建 Skill" });
  }

  return (
    <div className="settings-modal-overlay" onClick={props.onClose}>
      <div className="settings-modal-panel" onClick={(e) => e.stopPropagation()}>
        <h3>新建 Skill</h3>
        <p className="settings-modal-description">创建一个新的工作区 Skill。名称和描述会写入 SKILL.md。</p>
        <form onSubmit={handleSubmit} className="skill-create-form">
          <label className="settings-field">
            <span>显示名称</span>
            <input ref={nameRef} value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：Code Review Assistant" />
          </label>
          <label className="settings-field">
            <span>Slug {suggestedSlug && !slug.trim() ? <em className="skill-slug-hint">自动建议：{suggestedSlug}</em> : null}</span>
            <input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder={suggestedSlug || "例如：code-review-assistant"} />
          </label>
          <label className="settings-field">
            <span>描述</span>
            <textarea value={description} onChange={(e) => setDescription(e.target.value)} placeholder="简要说明这个 Skill 擅长处理什么任务" rows={3} />
          </label>
          <div className="settings-modal-actions">
            <button type="button" className="settings-modal-cancel" onClick={props.onClose} disabled={props.creating}>取消</button>
            <button type="submit" className="settings-modal-confirm" disabled={props.creating || !name.trim() || !effectiveSlug}>
              {props.creating ? "创建中…" : "创建 Skill"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function AddItemDialog(props: {
  open: boolean;
  mode: "file" | "folder";
  prefixPath: string;
  onClose: () => void;
  onAdd: (path: string) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [name, setName] = useState("");

  useLayoutEffect(() => {
    if (!props.open) { setName(""); return; }
    setName(props.prefixPath ? `${props.prefixPath}/` : "");
    const t = setTimeout(() => inputRef.current?.focus(), 0);
    return () => clearTimeout(t);
  }, [props.open]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!props.open) return null;

  const placeholder = props.mode === "file"
    ? (props.prefixPath ? `${props.prefixPath}/filename.md` : "例如：docs/guide.md")
    : (props.prefixPath ? `${props.prefixPath}/subfolder` : "例如：docs");

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    props.onAdd(trimmed);
    setName("");
  }

  return (
    <div className="settings-modal-overlay" onClick={props.onClose}>
      <div className="settings-modal-panel" onClick={(e) => e.stopPropagation()}>
        <h3>{props.mode === "file" ? "新增文件" : "新增文件夹"}</h3>
        <form onSubmit={handleSubmit}>
          <input ref={inputRef} value={name} onChange={(e) => setName(e.target.value)} placeholder={placeholder} />
          <div className="settings-modal-actions">
            <button type="button" className="settings-modal-cancel" onClick={props.onClose}>取消</button>
            <button type="submit" className="settings-modal-confirm" disabled={!name.trim()}>
              {props.mode === "file" ? "添加文件" : "添加文件夹"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

type TreeNode =
  | { kind: "file"; path: string; name: string }
  | { kind: "dir"; path: string; name: string; children: TreeNode[] };

function buildTree(filePaths: string[], localFolders: string[]): TreeNode[] {
  const root: TreeNode[] = [];
  for (const folder of localFolders) {
    const parts = folder.split("/");
    let current = root;
    for (let i = 0; i < parts.length; i++) {
      const dirPath = parts.slice(0, i + 1).join("/");
      let dir = current.find((n): n is Extract<TreeNode, { kind: "dir" }> => n.kind === "dir" && n.name === parts[i]);
      if (!dir) {
        dir = { kind: "dir", path: dirPath, name: parts[i], children: [] };
        current.push(dir);
      }
      current = dir.children;
    }
  }

  for (const filePath of filePaths) {
    const parts = filePath.split("/");
    if (parts.length === 1) {
      root.push({ kind: "file", path: filePath, name: parts[0] });
    } else {
      let current = root;
      for (let i = 0; i < parts.length - 1; i++) {
        const dirPath = parts.slice(0, i + 1).join("/");
        let dir = current.find((n): n is Extract<TreeNode, { kind: "dir" }> => n.kind === "dir" && n.name === parts[i]);
        if (!dir) {
          dir = { kind: "dir", path: dirPath, name: parts[i], children: [] };
          current.push(dir);
        }
        current = dir.children;
      }
      current.push({ kind: "file", path: filePath, name: parts[parts.length - 1] });
    }
  }

  return root;
}

function sortTree(nodes: TreeNode[]): TreeNode[] {
  return [...nodes].sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === "dir" ? -1 : 1;
    return a.name.localeCompare(b.name);
  }).map((n) => n.kind === "dir" ? { ...n, children: sortTree(n.children) } : n);
}

type RenameState = { path: string; value: string } | null;

function FileTreeNode(props: {
  node: TreeNode;
  depth: number;
  selectedPath: string;
  selectedDir: string;
  expandedDirs: Set<string>;
  renaming: RenameState;
  savedFiles: { path: string; content: string }[];
  files: { path: string; content: string }[];
  onSelect: (path: string) => void;
  onSelectDir: (path: string) => void;
  onToggleDir: (path: string) => void;
  onStartRename: (path: string, currentName: string) => void;
  onCommitRename: () => void;
  onCancelRename: () => void;
  onRenameChange: (value: string) => void;
  onDelete: (node: TreeNode) => void;
}) {
  const { node, depth, selectedPath, selectedDir, expandedDirs, renaming, onSelect, onSelectDir, onToggleDir, onStartRename, onCommitRename, onCancelRename, onRenameChange, onDelete } = props;
  const indent = depth * 14;
  const isRenaming = renaming?.path === node.path;

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter") { e.preventDefault(); onCommitRename(); }
    if (e.key === "Escape") { e.preventDefault(); onCancelRename(); }
  }

  if (node.kind === "dir") {
    const expanded = expandedDirs.has(node.path);
    const isDirSelected = selectedDir === node.path;
    return (
      <>
        <div
          className={`skill-tree-row skill-tree-dir ${isDirSelected ? "active" : ""}`}
          style={{ paddingLeft: 10 + indent }}
          onClick={() => { onSelectDir(node.path); onToggleDir(node.path); }}
          onDoubleClick={(e) => { e.stopPropagation(); if (!isRenaming) onStartRename(node.path, node.name); }}
        >
          <button type="button" className="skill-tree-toggle" onClick={(e) => { e.stopPropagation(); onToggleDir(node.path); }}>
            {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </button>
          {expanded ? <FolderOpen size={13} className="skill-tree-icon-dir" /> : <Folder size={13} className="skill-tree-icon-dir" />}
          {isRenaming ? (
            <input
              autoFocus
              className="skill-tree-rename-input"
              value={renaming.value}
              onChange={(e) => onRenameChange(e.target.value)}
              onBlur={onCommitRename}
              onKeyDown={handleKeyDown}
              onClick={(e) => e.stopPropagation()}
            />
          ) : (
            <span className="skill-tree-name">{node.name}</span>
          )}
          <button
            type="button"
            className="skill-tree-delete-btn"
            aria-label={`删除文件夹 ${node.name}`}
            onClick={(e) => { e.stopPropagation(); onDelete(node); }}
          >
            <Trash2 size={11} />
          </button>
        </div>
        {expanded && node.children.map((child) => (
          <FileTreeNode key={child.path} {...props} node={child} depth={depth + 1} />
        ))}
      </>
    );
  }

  const isSelected = node.path === selectedPath;
  const isMain = node.path === "SKILL.md";
  const fileDirty = (() => {
    const saved = props.savedFiles.find((sf) => sf.path === node.path);
    const current = props.files.find((f) => f.path === node.path);
    return !saved || saved.content !== current?.content;
  })();

  return (
    <div
      className={`skill-tree-row skill-tree-file ${isSelected ? "active" : ""}`}
      style={{ paddingLeft: 10 + indent }}
      onClick={() => { onSelect(node.path); onSelectDir(""); }}
      onDoubleClick={() => { if (!isMain) onStartRename(node.path, node.name); }}
    >
      <FileText size={13} className="skill-tree-icon-file" />
      {isRenaming ? (
        <input
          autoFocus
          className="skill-tree-rename-input"
          value={renaming.value}
          onChange={(e) => onRenameChange(e.target.value)}
          onBlur={onCommitRename}
          onKeyDown={handleKeyDown}
          onClick={(e) => e.stopPropagation()}
        />
      ) : (
        <span className="skill-tree-name">{node.name}</span>
      )}
      {isMain && <span className="skill-file-badge">主</span>}
      {fileDirty && <span className="skill-file-dirty" aria-label="未保存" />}
      {!isMain && (
        <button
          type="button"
          className="skill-tree-delete-btn"
          aria-label={`删除文件 ${node.name}`}
          onClick={(e) => { e.stopPropagation(); onDelete(node); }}
        >
          <Trash2 size={11} />
        </button>
      )}
    </div>
  );
}

type SkillsTabProps = {
  skills: SkillDefinitionWithFiles[];
  skillEditor: SkillEditorState;
  setSkillEditor: (v: SkillEditorState) => void;
  skillEditorOpen: boolean;
  onNewSkill: () => void;
  onLoadSkill: (slug: string) => void;
  onSaveSkill: (e: FormEvent) => void;
  onDeleteSkill: (slug: string) => void;
  onCloseSkillEditor: () => void;
};

function SkillsTab(props: SkillsTabProps) {
  const { skills, skillEditor, setSkillEditor, skillEditorOpen, onNewSkill, onLoadSkill, onSaveSkill, onDeleteSkill, onCloseSkillEditor } = props;
  const [query, setQuery] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [addItemMode, setAddItemMode] = useState<"file" | "folder" | null>(null);
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(new Set());
  const [selectedDir, setSelectedDir] = useState<string>("");
  const [renaming, setRenaming] = useState<{ path: string; value: string } | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<TreeNode | null>(null);

  const normalizedQuery = query.trim().toLowerCase();
  const filteredSkills = useMemo(() => {
    if (!normalizedQuery) return skills;
    return skills.filter((s) =>
      [s.definition.slug, s.definition.name, s.definition.description]
        .some((v) => String(v ?? "").toLowerCase().includes(normalizedQuery))
    );
  }, [skills, normalizedQuery]);

  const dirty = isDirtyEditor(skillEditor);
  const selectedFile = skillEditor.files.find((f) => f.path === skillEditor.selectedPath);

  const tree = useMemo(
    () => sortTree(buildTree(skillEditor.files.map((f) => f.path), skillEditor.localFolders)),
    [skillEditor.files, skillEditor.localFolders]
  );

  function directoryExists(path: string): boolean {
    return skillEditor.localFolders.includes(path) || skillEditor.files.some((f) => f.path.startsWith(path + "/"));
  }

  function handleSelectFile(path: string) {
    if (path === skillEditor.selectedPath) return;
    setSkillEditor({ ...skillEditor, selectedPath: path });
    setSelectedDir("");
  }

  function handleToggleDir(path: string) {
    setExpandedDirs((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path); else next.add(path);
      return next;
    });
  }

  function handleAddItem(rawPath: string) {
    const path = cleanSkillPath(rawPath);
    if (!path || pathHasUnsafeSegment(path)) return;

    if (addItemMode === "folder") {
      if (skillEditor.files.some((f) => f.path === path)) { setAddItemMode(null); return; }
      if (skillEditor.localFolders.includes(path)) { setAddItemMode(null); return; }
      const alreadyExists = skillEditor.files.some((f) => f.path.startsWith(path + "/") || f.path === path);
      if (!alreadyExists) {
        setSkillEditor({ ...skillEditor, localFolders: [...skillEditor.localFolders, path] });
      }
      const parts = path.split("/");
      setExpandedDirs((prev) => {
        const next = new Set(prev);
        for (let i = 1; i <= parts.length; i++) next.add(parts.slice(0, i).join("/"));
        return next;
      });
      setSelectedDir(path);
    } else {
      if (path.endsWith("/")) return;
      if (skillEditor.files.some((f) => f.path === path)) { setAddItemMode(null); return; }
      const newFiles = [...skillEditor.files, { path, content: "" }];
      const parts = path.split("/");
      const newLocalFolders = skillEditor.localFolders.filter((folder) => {
        return !parts.slice(0, -1).some((_, i) => parts.slice(0, i + 1).join("/") === folder && !newFiles.some((f) => f.path.startsWith(folder + "/")));
      });
      setSkillEditor({ ...skillEditor, files: newFiles, selectedPath: path, localFolders: newLocalFolders });
      if (parts.length > 1) {
        setExpandedDirs((prev) => {
          const next = new Set(prev);
          for (let i = 1; i < parts.length; i++) next.add(parts.slice(0, i).join("/"));
          return next;
        });
      }
    }
    setAddItemMode(null);
  }

  function handleStartRename(path: string, currentName: string) {
    setRenaming({ path, value: currentName });
  }

  function handleCommitRename() {
    if (!renaming) return;
    if (renaming.path === "SKILL.md") { setRenaming(null); return; }
    const newName = cleanSkillPath(renaming.value);
    if (!newName || newName.includes("/") || pathHasUnsafeSegment(newName)) { setRenaming(null); return; }

    const isDir = skillEditor.files.every((f) => f.path !== renaming.path);
    if (isDir) {
      const oldPrefix = renaming.path + "/";
      const parentPath = parentDirectory(renaming.path);
      const newDirPath = parentPath ? `${parentPath}/${newName}` : newName;
      if (newDirPath === renaming.path) { setRenaming(null); return; }
      const newPrefix = newDirPath + "/";
      const newFiles = skillEditor.files.map((f) =>
        f.path.startsWith(oldPrefix) ? { ...f, path: newPrefix + f.path.slice(oldPrefix.length) } : f
      );
      if (newFiles.length !== new Set(newFiles.map((f) => f.path)).size) { setRenaming(null); return; }
      if (skillEditor.files.some((f) => f.path === newDirPath)) { setRenaming(null); return; }
      const newLocalFolders = skillEditor.localFolders
        .filter((folder) => folder !== renaming.path && !folder.startsWith(oldPrefix))
        .concat(
          skillEditor.localFolders
            .filter((folder) => folder === renaming.path || folder.startsWith(oldPrefix))
            .map((folder) => folder === renaming.path ? newDirPath : newPrefix + folder.slice(oldPrefix.length))
        );
      const newSelectedPath = skillEditor.selectedPath.startsWith(oldPrefix)
        ? newPrefix + skillEditor.selectedPath.slice(oldPrefix.length)
        : skillEditor.selectedPath;
      setExpandedDirs((prev) => {
        const next = new Set<string>();
        for (const p of prev) {
          if (p === renaming.path) next.add(newDirPath);
          else if (p.startsWith(oldPrefix)) next.add(newPrefix + p.slice(oldPrefix.length));
          else next.add(p);
        }
        return next;
      });
      setSkillEditor({ ...skillEditor, files: newFiles, localFolders: newLocalFolders, selectedPath: newSelectedPath });
    } else {
      const oldPath = renaming.path;
      const parentPath = parentDirectory(oldPath);
      const newPath = parentPath ? `${parentPath}/${newName}` : newName;
      if (newPath === oldPath) { setRenaming(null); return; }
      if (skillEditor.files.some((f) => f.path === newPath)) { setRenaming(null); return; }
      const newFiles = skillEditor.files.map((f) => f.path === oldPath ? { ...f, path: newPath } : f);
      const newSelectedPath = skillEditor.selectedPath === oldPath ? newPath : skillEditor.selectedPath;
      setSkillEditor({ ...skillEditor, files: newFiles, selectedPath: newSelectedPath });
    }
    setRenaming(null);
  }

  function handleDeleteNode(node: TreeNode) {
    if (node.kind === "dir") {
      const hasFiles = skillEditor.files.some((f) => f.path.startsWith(node.path + "/"));
      if (hasFiles) { setDeleteConfirm(node); return; }
      setSkillEditor({ ...skillEditor, localFolders: skillEditor.localFolders.filter((f) => f !== node.path && !f.startsWith(node.path + "/")) });
      return;
    }
    if (node.path === "SKILL.md") return;
    setDeleteConfirm(node);
  }

  function handleConfirmDelete() {
    if (!deleteConfirm) return;
    if (deleteConfirm.kind === "file") {
      const newFiles = skillEditor.files.filter((f) => f.path !== deleteConfirm.path);
      const nextPath = newFiles.some((f) => f.path === "SKILL.md") ? "SKILL.md" : newFiles[0]?.path ?? "";
      setSkillEditor({ ...skillEditor, files: newFiles, selectedPath: nextPath });
    } else {
      const prefix = deleteConfirm.path + "/";
      const newFiles = skillEditor.files.filter((f) => !f.path.startsWith(prefix));
      const newLocalFolders = skillEditor.localFolders.filter((f) => f !== deleteConfirm.path && !f.startsWith(prefix));
      const nextPath = newFiles.some((f) => f.path === "SKILL.md") ? "SKILL.md" : newFiles[0]?.path ?? "";
      setSkillEditor({ ...skillEditor, files: newFiles, localFolders: newLocalFolders, selectedPath: nextPath });
    }
    setDeleteConfirm(null);
  }

  function handleContentChange(content: string) {
    setSkillEditor({
      ...skillEditor,
      files: skillEditor.files.map((f) => f.path === skillEditor.selectedPath ? { ...f, content } : f),
    });
  }

  function handleCloseEditor() {
    if (dirty && !window.confirm("有未保存的修改，确认放弃并返回列表吗？")) return;
    if (dirty) {
      setSkillEditor({
        ...skillEditor,
        files: skillEditor.savedFiles.length > 0 ? skillEditor.savedFiles.map((f) => ({ ...f })) : skillEditor.files,
        selectedPath: skillEditor.savedFiles[0]?.path ?? skillEditor.selectedPath,
      });
    }
    onCloseSkillEditor();
  }

  async function handleCreate(payload: { slug: string; name: string; description: string }) {
    setCreating(true);
    try {
      onNewSkill();
      setSkillEditor({
        slug: payload.slug,
        name: payload.name,
        description: payload.description,
        files: [{ path: "SKILL.md", content: `# ${payload.name}\n\n${payload.description}\n` }],
        selectedPath: "SKILL.md",
        savedFiles: [],
        localFolders: [],
      });
      setCreateOpen(false);
    } finally {
      setCreating(false);
    }
  }

  if (!skillEditorOpen) {
    return (
      <PageShell>
        <SectionCard
          title="Skill 列表"
          description="注入到新 session 的 Skill 包。"
          actions={
            <button type="button" className="settings-primary-button" onClick={() => setCreateOpen(true)}>
              <Plus size={14} />新建 Skill
            </button>
          }
        >
          <div className="skill-search-bar">
            <Search size={14} className="skill-search-icon" />
            <input
              className="skill-search-input"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索 Skill…"
            />
          </div>
          {normalizedQuery && (
            <p className="skill-search-hint">命中 {filteredSkills.length} / {skills.length}</p>
          )}
          <div className="settings-list">
            {filteredSkills.map((skill) => (
              <div className="settings-list-row" key={skill.definition.slug}>
                <button type="button" className="settings-list-button" onClick={() => void onLoadSkill(skill.definition.slug)}>
                  <FileText size={15} />
                  <span>{skill.definition.name || skill.definition.slug}</span>
                  <small>{skill.definition.slug}</small>
                </button>
                <button type="button" className="settings-delete-button" aria-label={`删除 ${skill.definition.slug}`} onClick={() => void onDeleteSkill(skill.definition.slug)}>
                  <Trash2 size={14} />
                </button>
              </div>
            ))}
            {skills.length === 0 && <p className="empty-copy">暂无 Skill。</p>}
            {skills.length > 0 && filteredSkills.length === 0 && <p className="empty-copy">没有匹配的 Skill。</p>}
          </div>
        </SectionCard>
        <CreateSkillDialog open={createOpen} creating={creating} onClose={() => setCreateOpen(false)} onCreate={(p) => void handleCreate(p)} />
      </PageShell>
    );
  }

  return (
    <PageShell>
      <SectionCard
        title={skillEditor.slug ? `Skill: ${skillEditor.slug}` : "新建 Skill"}
        actions={
          <button type="button" className="settings-back-button" onClick={handleCloseEditor}>← 返回列表</button>
        }
      >
        <form className="settings-form skill-meta-form" onSubmit={onSaveSkill}>
          <Field label="Slug">
            <input value={skillEditor.slug} onChange={(e) => setSkillEditor({ ...skillEditor, slug: e.target.value })} placeholder="my-skill" />
          </Field>
          <Field label="名称">
            <input value={skillEditor.name} onChange={(e) => setSkillEditor({ ...skillEditor, name: e.target.value })} placeholder="My Skill" />
          </Field>
          <Field label="描述" span>
            <input value={skillEditor.description} onChange={(e) => setSkillEditor({ ...skillEditor, description: e.target.value })} placeholder="简要描述" />
          </Field>

          <div className="skill-editor-area settings-field-span">
            <div className="skill-file-list">
              <div className="skill-file-list-header">
                <span>文件</span>
                <div className="skill-file-list-actions">
                  <button type="button" className="skill-file-add-btn" onClick={() => setAddItemMode("file")} aria-label="新增文件" title="新增文件">
                    <FilePlus size={13} />
                  </button>
                  <button type="button" className="skill-file-add-btn" onClick={() => setAddItemMode("folder")} aria-label="新增文件夹" title="新增文件夹">
                    <FolderPlus size={13} />
                  </button>
                </div>
              </div>
              <div className="skill-tree">
                {tree.map((node) => (
                  <FileTreeNode
                    key={node.path}
                    node={node}
                    depth={0}
                    selectedPath={skillEditor.selectedPath}
                    selectedDir={selectedDir}
                    expandedDirs={expandedDirs}
                    renaming={renaming}
                    savedFiles={skillEditor.savedFiles}
                    files={skillEditor.files}
                    onSelect={handleSelectFile}
                    onSelectDir={setSelectedDir}
                    onToggleDir={handleToggleDir}
                    onStartRename={handleStartRename}
                    onCommitRename={handleCommitRename}
                    onCancelRename={() => setRenaming(null)}
                    onRenameChange={(value) => setRenaming((r) => r ? { ...r, value } : r)}
                    onDelete={handleDeleteNode}
                  />
                ))}
              </div>
            </div>

            <div className="skill-editor-pane">
              <div className="skill-editor-pane-header">
                <span className="skill-editor-path">{skillEditor.selectedPath}</span>
                <div className="skill-editor-pane-actions">
                  {dirty && <span className="skill-dirty-badge">未保存</span>}
                </div>
              </div>
              <textarea
                className="settings-code-textarea skill-editor-textarea"
                value={selectedFile?.content ?? ""}
                onChange={(e) => handleContentChange(e.target.value)}
              />
            </div>
          </div>

          <div className="settings-form-footer">
            <span>保存后的 Skill 仅对新 session 生效。</span>
            <button type="submit" className="settings-primary-button" disabled={!skillEditor.slug.trim()}>
              <Save size={14} />保存 Skill
            </button>
          </div>
        </form>
      </SectionCard>

      <AddItemDialog
        open={addItemMode !== null}
        mode={addItemMode ?? "file"}
        prefixPath={(() => {
          if (selectedDir && directoryExists(selectedDir)) return selectedDir;
          const parent = parentDirectory(skillEditor.selectedPath);
          return parent;
        })()}
        onClose={() => setAddItemMode(null)}
        onAdd={handleAddItem}
      />

      {deleteConfirm && (
        <div className="settings-modal-overlay" onClick={() => setDeleteConfirm(null)}>
          <div className="settings-modal-panel" onClick={(e) => e.stopPropagation()}>
            <h3>{deleteConfirm.kind === "dir" ? "删除文件夹" : "删除文件"}</h3>
            <p className="settings-modal-description">
              确认删除 <code>{deleteConfirm.path}</code>
              {deleteConfirm.kind === "dir" ? " 及其所有文件" : ""}？
            </p>
            <div className="settings-modal-actions">
              <button type="button" className="settings-modal-cancel" onClick={() => setDeleteConfirm(null)}>取消</button>
              <button type="button" className="settings-modal-confirm" onClick={handleConfirmDelete}>删除</button>
            </div>
          </div>
        </div>
      )}
    </PageShell>
  );
}

function WorkspaceSettingsCard({  activeWorkspace,
  onUpdateWorkspace,
  onDeleteWorkspace,
  onBack,
}: {
  activeWorkspace: WorkspaceProjection | undefined;
  onUpdateWorkspace: (id: string, name: string) => Promise<void>;
  onDeleteWorkspace: (id: string) => Promise<{ deleted: boolean; replacementWorkspace?: WorkspaceProjection }>;
  onBack: () => void;
}) {
  const [editName, setEditName] = useState("");
  const [editError, setEditError] = useState<string | null>(null);
  const [editLoading, setEditLoading] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [confirmName, setConfirmName] = useState("");
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  useEffect(() => {
    setEditName(activeWorkspace?.name ?? "");
    setEditError(null);
  }, [activeWorkspace?.id, activeWorkspace?.name]);

  async function handleRenameSubmit(e: FormEvent) {
    e.preventDefault();
    if (!activeWorkspace) return;
    const name = editName.trim();
    if (!name) {
      setEditError("请输入 workspace 名称");
      return;
    }
    setEditLoading(true);
    setEditError(null);
    try {
      await onUpdateWorkspace(activeWorkspace.id, name);
    } catch (err) {
      setEditError(err instanceof Error ? err.message : "重命名失败");
    } finally {
      setEditLoading(false);
    }
  }

  async function handleDeleteConfirm() {
    if (!activeWorkspace) return;
    if (confirmName.trim() !== activeWorkspace.name) {
      setDeleteError("输入的名称与 workspace 名称不一致");
      return;
    }
    setDeleteLoading(true);
    setDeleteError(null);
    try {
      await onDeleteWorkspace(activeWorkspace.id);
      setDeleteOpen(false);
      setConfirmName("");
      onBack();
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "删除失败");
    } finally {
      setDeleteLoading(false);
    }
  }

  return (
    <PageShell>
      <SectionCard title="Workspace">
        {!activeWorkspace ? (
          <p className="empty-copy">当前没有可管理的 Workspace。</p>
        ) : (
          <form onSubmit={handleRenameSubmit} className="settings-form">
            <Field label="名称" span>
              <input
                value={editName}
                onChange={(e) => setEditName(e.target.value)}
                placeholder="Workspace 名称"
              />
            </Field>
            {editError && <p className="login-error settings-field-span">{editError}</p>}
            <div className="settings-form-footer">
              <button
                type="button"
                className="settings-text-delete-button"
                aria-label="删除 Workspace"
                onClick={() => { setDeleteOpen(true); setConfirmName(""); setDeleteError(null); }}
              >
                <Trash2 size={14} />删除 Workspace
              </button>
              <div className="settings-card-actions">
                <button type="submit" disabled={editLoading} className="settings-primary-button">
                  <Save size={14} />{editLoading ? "保存中…" : "保存"}
                </button>
              </div>
            </div>
          </form>
        )}
      </SectionCard>

      {deleteOpen && activeWorkspace && (
        <div className="settings-modal-overlay" onClick={() => { setDeleteOpen(false); setConfirmName(""); setDeleteError(null); }}>
          <div className="settings-modal-panel" onClick={(e) => e.stopPropagation()}>
            <h3>删除 Workspace</h3>
            <p className="settings-modal-description">
              此操作将永久删除该 workspace 及其所有数据。请输入 workspace 名称以确认删除：
              <span className="font-medium ml-1">
                {activeWorkspace.name}
              </span>
            </p>
            <input
              autoFocus
              value={confirmName}
              onChange={(e) => setConfirmName(e.target.value)}
              placeholder="输入 workspace 名称"
            />
            {deleteError && <p className="settings-modal-error">{deleteError}</p>}
            <div className="settings-modal-actions">
              <button
                type="button"
                className="settings-modal-cancel"
                onClick={() => { setDeleteOpen(false); setConfirmName(""); setDeleteError(null); }}
              >
                取消
              </button>
              <button
                type="button"
                disabled={deleteLoading}
                aria-label="确认删除 Workspace"
                className="settings-modal-confirm"
                onClick={handleDeleteConfirm}
              >
                {deleteLoading ? "删除中…" : "删除"}
              </button>
            </div>
          </div>
        </div>
      )}
    </PageShell>
  );
}

export function SettingsPanel(props: Props) {
  const {
    settingsTab, setSettingsTab, onBack,
    llmConnection, apiKeySet, llmForm, setLLMForm, onSaveLLM,
    models, manualModelId, setManualModelId, onRefreshModels, onUpsertModel, onManualModel, onSetDefaultModel,
    skills, skillEditor, setSkillEditor, skillEditorOpen, onNewSkill, onLoadSkill, onSaveSkill, onDeleteSkill, onCloseSkillEditor,
    mcpServers, mcpForm, setMCPForm, mcpEditorOpen, onNewMCP, onLoadMCP, onSaveMCP, onDeleteMCP, onCloseMCPEditor,
    activeWorkspace, onUpdateWorkspace, onDeleteWorkspace,
    plugins, onSavePlugin
  } = props;

  return (
    <main className="settings-shell">
      <div className="settings-frame">
        <nav className="settings-nav" aria-label="设置分区">
          <button type="button" className="settings-back-button" onClick={onBack}>← 返回工作台</button>
          <div className="settings-nav-items">
            {settingsTabs.map((item) => (
              <button
                key={item.tab}
                type="button"
                aria-current={settingsTab === item.tab ? "page" : undefined}
                className={`settings-nav-item ${settingsTab === item.tab ? "active" : ""}`}
                onClick={() => setSettingsTab(item.tab)}
              >
                <span className="settings-nav-item-label">
                  {item.icon}
                  {item.label}
                </span>
              </button>
            ))}
          </div>
        </nav>
        <div className="settings-main">
          {settingsTab === "llm" && (
            <PageShell>
              <SectionCard title="连接" description="配置供应商凭证与端点。">
                <form className="settings-form" onSubmit={onSaveLLM}>
                  <Field label="API 协议">
                    <CustomSelect
                      value={llmForm.apiProtocol}
                      options={API_PROTOCOL_OPTIONS}
                      onChange={(value) => setLLMForm({ ...llmForm, apiProtocol: value })}
                    />
                  </Field>
                  <Field label="Base URL" span><input value={llmForm.baseUrl} onChange={(e) => setLLMForm({ ...llmForm, baseUrl: e.target.value })} placeholder="http://example.local:8084" /></Field>
                  <Field label="API key" span>
                    <input value={llmForm.apiKey} onChange={(e) => setLLMForm({ ...llmForm, apiKey: e.target.value })} type="password" placeholder={apiKeySet ? "留空保留现有密钥" : "必填"} />
                  </Field>
                  <div className="settings-form-footer">
                    <span>{apiKeySet ? "已保存密钥。留空保留现有密钥，填写则替换。" : "尚未保存连接。"}</span>
                    <button type="submit" className="settings-primary-button"><Save size={14} />保存连接</button>
                  </div>
                </form>
              </SectionCard>
              <SectionCard
                title="模型"
                description="启用模型并设置默认。新会话使用默认模型。"
                actions={<button type="button" onClick={() => void onRefreshModels()} disabled={!llmConnection}><RefreshCw size={14} />刷新</button>}
              >
                <form className="settings-inline-form" onSubmit={onManualModel}>
                  <input className="min-w-0" value={manualModelId} onChange={(e) => setManualModelId(e.target.value)} placeholder="modelId" />
                  <button type="submit" className="settings-primary-button"><Plus size={14} />添加</button>
                </form>
                <div className="settings-list settings-model-list">
                  {models.map((model) => (
                    <label key={model.id || model.modelId} className={`settings-model-row ${model.enabled ? "selected" : ""}`}>
                      <input type="checkbox" checked={model.enabled} onChange={(e) => void onUpsertModel(model.modelId, e.target.checked, model.source)} />
                      <span>{model.modelId}</span>
                      <small>{model.source}</small>
                      {model.enabled && (
                        <button
                          type="button"
                          className="settings-model-default-radio"
                          onClick={(e) => {
                            e.preventDefault();
                            void onSetDefaultModel(llmConnection?.defaultModelId === model.modelId ? "" : model.modelId);
                          }}
                          aria-label={llmConnection?.defaultModelId === model.modelId ? "取消默认" : "设为默认"}
                        >
                          <span className={`radio-dot ${llmConnection?.defaultModelId === model.modelId ? "active" : ""}`} />
                        </button>
                      )}
                    </label>
                  ))}
                  {models.length === 0 && <p className="empty-copy">未配置模型。先保存连接并刷新。</p>}
                </div>
              </SectionCard>
            </PageShell>
          )}
          {settingsTab === "skills" && (
            <SkillsTab
              skills={skills}
              skillEditor={skillEditor}
              setSkillEditor={setSkillEditor}
              skillEditorOpen={skillEditorOpen}
              onNewSkill={onNewSkill}
              onLoadSkill={onLoadSkill}
              onSaveSkill={onSaveSkill}
              onDeleteSkill={onDeleteSkill}
              onCloseSkillEditor={onCloseSkillEditor}
            />
          )}
          {settingsTab === "mcp" && (
            <PageShell>
              {!mcpEditorOpen ? (
                <SectionCard
                  title="服务器列表"
                  description="Workspace 工具服务器与环境变量。"
                  actions={<button type="button" className="settings-primary-button" onClick={onNewMCP}><Plus size={14} />新建服务器</button>}
                >
                  <div className="settings-list">
                    {mcpServers.map((server) => (
                      <div className="settings-list-row" key={server.definition.name}>
                        <button type="button" className="settings-list-button" onClick={() => void onLoadMCP(server.definition.name)}>
                          <Server size={15} />
                          <span>{server.definition.name}</span>
                          <small>{server.definition.transport || "stdio"}</small>
                        </button>
                        <button type="button" className="settings-delete-button" aria-label={`删除 ${server.definition.name}`} onClick={() => void onDeleteMCP(server.definition.name)}>
                          <Trash2 size={14} />
                        </button>
                      </div>
                    ))}
                    {mcpServers.length === 0 && <p className="empty-copy">暂无 MCP 服务器。</p>}
                  </div>
                </SectionCard>
              ) : (
                <SectionCard title="服务器编辑器" description="命令、参数与环境变量输入。" actions={<button type="button" className="settings-back-button" onClick={onCloseMCPEditor}>← 返回列表</button>}>
                  <form className="settings-form" onSubmit={onSaveMCP}>
                    <Field label="名称" span><input value={mcpForm.name} onChange={(e) => setMCPForm({ ...mcpForm, name: e.target.value })} /></Field>
                    <Field label="命令" span><input value={mcpForm.command} onChange={(e) => setMCPForm({ ...mcpForm, command: e.target.value })} /></Field>
                    <Field label="参数" span><textarea value={mcpForm.args} onChange={(e) => setMCPForm({ ...mcpForm, args: e.target.value })} placeholder="每行一个参数" /></Field>
                    <Field label="Transport" span><input value={mcpForm.transport} onChange={(e) => setMCPForm({ ...mcpForm, transport: e.target.value })} /></Field>
                    <Field label="Env" span><textarea value={mcpForm.env} onChange={(e) => setMCPForm({ ...mcpForm, env: e.target.value })} placeholder="KEY=value" /></Field>
                    <div className="settings-form-footer">
                      <span>环境变量按 Workspace 服务器存储。</span>
                      <button type="submit" className="settings-primary-button"><Save size={14} />保存 MCP</button>
                    </div>
                  </form>
                </SectionCard>
              )}
            </PageShell>
          )}
          {settingsTab === "plugins" && (
            <PluginsTab
              plugins={plugins}
              onSavePlugin={onSavePlugin}
            />
          )}
          {settingsTab === "workspace" && (
            <WorkspaceSettingsCard
              activeWorkspace={activeWorkspace}
              onUpdateWorkspace={onUpdateWorkspace}
              onDeleteWorkspace={onDeleteWorkspace}
              onBack={onBack}
            />
          )}
        </div>
      </div>
    </main>
  );
}
