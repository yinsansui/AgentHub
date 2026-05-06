import { Bot, FileText, Plus, Puzzle, RefreshCw, Save, Server, Trash2 } from "lucide-react";
import type { FormEvent, ReactNode } from "react";
import type { LLMConnection, LLMModel, MCPServerDefinitionWithEnv, SkillDefinitionWithFiles } from "../types";

type SettingsTab = "llm" | "skills" | "mcp";

type Props = {
  settingsTab: SettingsTab;
  setSettingsTab: (tab: SettingsTab) => void;
  onBack: () => void;
  llmConnection: LLMConnection | null;
  llmForm: { provider: string; apiProtocol: string; baseUrl: string; apiKey: string };
  setLLMForm: (v: { provider: string; apiProtocol: string; baseUrl: string; apiKey: string }) => void;
  onSaveLLM: (e: FormEvent) => void;
  models: LLMModel[];
  manualModelId: string;
  setManualModelId: (v: string) => void;
  onRefreshModels: () => void;
  onUpsertModel: (modelId: string, enabled: boolean, source?: string) => void;
  onManualModel: (e: FormEvent) => void;
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
};

type SettingsTabMeta = {
  tab: SettingsTab;
  label: string;
  icon: ReactNode;
};

const settingsTabs: SettingsTabMeta[] = [
  { tab: "llm", label: "LLM", icon: <Bot size={16} /> },
  { tab: "skills", label: "Skill", icon: <Puzzle size={16} /> },
  { tab: "mcp", label: "MCP", icon: <Server size={16} /> }
];

function Field(props: { label: string; span?: boolean; children: ReactNode }) {
  return (
    <label className={`settings-field ${props.span ? "settings-field-span" : ""}`}>
      <span>{props.label}</span>
      {props.children}
    </label>
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

export function SettingsPanel(props: Props) {
  const {
    settingsTab, setSettingsTab, onBack,
    llmConnection, llmForm, setLLMForm, onSaveLLM,
    models, manualModelId, setManualModelId, onRefreshModels, onUpsertModel, onManualModel,
    skills, skillForm, setSkillForm, skillEditorOpen, onNewSkill, onLoadSkill, onSaveSkill, onDeleteSkill, onCloseSkillEditor,
    mcpServers, mcpForm, setMCPForm, mcpEditorOpen, onNewMCP, onLoadMCP, onSaveMCP, onDeleteMCP, onCloseMCPEditor
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
                  <Field label="供应商"><input value={llmForm.provider} onChange={(e) => setLLMForm({ ...llmForm, provider: e.target.value })} /></Field>
                  <Field label="API 协议"><input value={llmForm.apiProtocol} onChange={(e) => setLLMForm({ ...llmForm, apiProtocol: e.target.value })} /></Field>
                  <Field label="Base URL" span><input value={llmForm.baseUrl} onChange={(e) => setLLMForm({ ...llmForm, baseUrl: e.target.value })} placeholder="http://example.local:8084" /></Field>
                  <Field label="API key" span><input value={llmForm.apiKey} onChange={(e) => setLLMForm({ ...llmForm, apiKey: e.target.value })} type="password" placeholder={llmConnection ? "更新时必填" : "必填"} /></Field>
                  <div className="settings-form-footer">
                    <span>{llmConnection ? "已检测到现有连接。API key 仅在编辑时显示。" : "尚未保存连接。"}</span>
                    <button type="submit" className="settings-primary-button"><Save size={14} />保存连接</button>
                  </div>
                </form>
              </SectionCard>
              <SectionCard
                title="模型"
                description="仅启用当前 Workspace 需要暴露的 modelId。"
                actions={<button type="button" onClick={() => void onRefreshModels()}><RefreshCw size={14} />刷新</button>}
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
                    </label>
                  ))}
                  {models.length === 0 && <p className="empty-copy">未配置模型。</p>}
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
        </div>
      </div>
    </main>
  );
}
