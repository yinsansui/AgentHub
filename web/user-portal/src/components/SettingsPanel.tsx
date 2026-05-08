import { Bot, ChevronDown, FileText, Plus, Puzzle, RefreshCw, Save, Server, Trash2, LayoutGrid } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import type { LLMConnection, LLMModel, MCPServerDefinitionWithEnv, SkillDefinitionWithFiles, WorkspaceProjection } from "../types";

type SettingsTab = "llm" | "skills" | "mcp" | "workspace";

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
  skillForm: { slug: string; name: string; description: string; path: string; content: string };
  setSkillForm: (v: { slug: string; name: string; description: string; path: string; content: string }) => void;
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

function PageShell(props: { actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="settings-page">
      {props.actions && <div className="settings-page-actions">{props.actions}</div>}
      <div className="settings-page-body">{props.children}</div>
    </section>
  );
}

function SectionCard(props: { title: string; description?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="settings-card">
      <div className="settings-card-header">
        <div>
          <h2>{props.title}</h2>
          {props.description && <p>{props.description}</p>}
        </div>
        {props.actions && <div className="settings-card-actions">{props.actions}</div>}
      </div>
      {props.children}
    </section>
  );
}

function WorkspaceSettingsCard({
  activeWorkspace,
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
    skills, skillForm, setSkillForm, skillEditorOpen, onNewSkill, onLoadSkill, onSaveSkill, onDeleteSkill, onCloseSkillEditor,
    mcpServers, mcpForm, setMCPForm, mcpEditorOpen, onNewMCP, onLoadMCP, onSaveMCP, onDeleteMCP, onCloseMCPEditor,
    activeWorkspace, onUpdateWorkspace, onDeleteWorkspace
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
            <PageShell>
              {!skillEditorOpen ? (
                <SectionCard
                  title="Skill 列表"
                  description="注入到新 session 的 Skill 包。"
                  actions={<button type="button" className="settings-primary-button" onClick={onNewSkill}><Plus size={14} />新建 Skill</button>}
                >
                  <div className="settings-list">
                    {skills.map((skill) => (
                      <div className="settings-list-row" key={skill.definition.slug}>
                        <button type="button" className="settings-list-button" onClick={() => void onLoadSkill(skill.definition.slug)}>
                          <FileText size={15} />
                          <span>{skill.definition.slug}</span>
                          <small>{skill.definition.name || "Skill"}</small>
                        </button>
                        <button type="button" className="settings-delete-button" aria-label={`删除 ${skill.definition.slug}`} onClick={() => void onDeleteSkill(skill.definition.slug)}>
                          <Trash2 size={14} />
                        </button>
                      </div>
                    ))}
                    {skills.length === 0 && <p className="empty-copy">暂无 Skill。</p>}
                  </div>
                </SectionCard>
              ) : (
                <SectionCard title="Skill 编辑器" description="编辑元数据与包入口文件。" actions={<button type="button" className="settings-back-button" onClick={onCloseSkillEditor}>← 返回列表</button>}>
                  <form className="settings-form" onSubmit={onSaveSkill}>
                    <Field label="Slug"><input value={skillForm.slug} onChange={(e) => setSkillForm({ ...skillForm, slug: e.target.value })} /></Field>
                    <Field label="名称"><input value={skillForm.name} onChange={(e) => setSkillForm({ ...skillForm, name: e.target.value })} /></Field>
                    <Field label="描述" span><input value={skillForm.description} onChange={(e) => setSkillForm({ ...skillForm, description: e.target.value })} /></Field>
                    <Field label="文件路径" span><input value={skillForm.path} onChange={(e) => setSkillForm({ ...skillForm, path: e.target.value })} /></Field>
                    <Field label="内容" span><textarea className="settings-code-textarea" value={skillForm.content} onChange={(e) => setSkillForm({ ...skillForm, content: e.target.value })} /></Field>
                    <div className="settings-form-footer">
                      <span>保存后的 Skill 仅对新 session 生效。</span>
                      <button type="submit" className="settings-primary-button"><Save size={14} />保存 Skill</button>
                    </div>
                  </form>
                </SectionCard>
              )}
            </PageShell>
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
