import { FileText, Play, Plus, RefreshCw, Save, ScrollText, Server, Trash2 } from "lucide-react";
import type { FormEvent } from "react";
import type { LLMConnection, LLMModel, MCPServerDefinitionWithEnv, PodInfo, SkillDefinitionWithFiles } from "../types";
import type { Notice } from "../hooks/useWorkspace";

type SettingsTab = "runtime" | "llm" | "skills" | "mcp";

type Props = {
  settingsTab: SettingsTab;
  setSettingsTab: (tab: SettingsTab) => void;
  onBack: () => void;
  notice: Notice | null;
  pod: PodInfo | null;
  logs: string;
  showLogs: boolean;
  workspaceId: string;
  onRefreshWorkspace: () => void;
  onStartPod: () => void;
  onLoadLogs: () => void;
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
  mcpServers: MCPServerDefinitionWithEnv[];
  mcpForm: { name: string; command: string; args: string; transport: string; env: string };
  setMCPForm: (v: { name: string; command: string; args: string; transport: string; env: string }) => void;
  mcpEditorOpen: boolean;
  onNewMCP: () => void;
  onLoadMCP: (name: string) => void;
  onSaveMCP: (e: FormEvent) => void;
  onDeleteMCP: (name: string) => void;
};

export function SettingsPanel(props: Props) {
  const {
    settingsTab, setSettingsTab, onBack, notice,
    pod, logs, showLogs, workspaceId,
    onRefreshWorkspace, onStartPod, onLoadLogs,
    llmConnection, llmForm, setLLMForm, onSaveLLM,
    models, manualModelId, setManualModelId, onRefreshModels, onUpsertModel, onManualModel,
    skills, skillForm, setSkillForm, skillEditorOpen, onNewSkill, onLoadSkill, onSaveSkill, onDeleteSkill,
    mcpServers, mcpForm, setMCPForm, mcpEditorOpen, onNewMCP, onLoadMCP, onSaveMCP, onDeleteMCP
  } = props;

  return (
    <main className="grid h-screen min-h-[720px] grid-cols-[220px_minmax(520px,1fr)] gap-2 p-2 max-[700px]:grid-cols-1 max-[700px]:p-0">
      <div className="col-span-full grid grid-cols-[220px_1fr] bg-apple-panel shadow-apple-card rounded-2xl overflow-hidden h-full max-[700px]:grid-cols-1">
        <nav className="border-r border-apple-fg-5 px-2 py-3 flex flex-col gap-0.5">
          <button type="button" className="justify-start bg-transparent shadow-none text-[13px] text-apple-fg-50 px-2 py-[5px] hover:bg-apple-fg-5 hover:text-apple-fg" onClick={onBack}>← Back</button>
          <div className="h-px bg-apple-fg-5 my-1.5" />
          {(["runtime", "llm", "skills", "mcp"] as SettingsTab[]).map((tab) => (
            <button
              key={tab}
              type="button"
              className={`flex items-center gap-2 min-h-[30px] w-full justify-start text-left bg-transparent shadow-none rounded-lg px-2.5 py-[5px] text-[13px] text-apple-fg font-normal hover:bg-black/5 ${settingsTab === tab ? "bg-black/[0.07]" : ""}`}
              onClick={() => setSettingsTab(tab)}
            >
              {tab === "runtime" && "Runtime"}
              {tab === "llm" && "LLM & Models"}
              {tab === "skills" && "Skills"}
              {tab === "mcp" && "MCP Servers"}
            </button>
          ))}
        </nav>
        <div className="overflow-auto flex flex-col">
          {notice && <div className={`notice ${notice.tone}`}>{notice.text}</div>}
          {settingsTab === "runtime" && (
            <div className="min-h-0 grid content-start gap-3.5 px-5 pt-[18px] pb-[22px]">
              <div className="flex flex-wrap gap-2">
                <button type="button" onClick={() => void onRefreshWorkspace()}><RefreshCw size={14} />Refresh</button>
                <button type="button" onClick={() => void onStartPod()}><Play size={14} />Start workspace</button>
                <button type="button" onClick={() => void onLoadLogs()}><ScrollText size={14} />Logs</button>
              </div>
              <dl className="grid gap-[7px] m-0 rounded-xl p-3.5 bg-black/[0.03] shadow-[inset_0_0_0_1px_rgba(0,0,0,0.07)] [&_div]:grid [&_div]:gap-[3px] [&_dd]:m-0 [&_dd]:overflow-wrap-anywhere [&_dd]:text-[13px]">
                <div><dt>Workspace</dt><dd>{workspaceId}</dd></div>
                <div><dt>Pod</dt><dd>{pod?.name ?? "not found"}</dd></div>
                <div><dt>Image</dt><dd>{pod?.image ?? "-"}</dd></div>
                <div><dt>Endpoint</dt><dd>{pod?.endpoint ?? "-"}</dd></div>
              </dl>
              {showLogs && <pre className="max-h-[260px] overflow-auto m-0 p-2.5 bg-[color-mix(in_oklab,var(--color-apple-fg)_88%,black)] text-[color-mix(in_oklab,var(--color-apple-bg)_88%,white)] rounded-lg whitespace-pre-wrap text-xs max-h-[min(520px,55vh)]">{logs || "No logs loaded."}</pre>}
            </div>
          )}
          {settingsTab === "llm" && (
            <div className="min-h-0 grid content-start gap-3.5 px-5 pt-[18px] pb-[22px]">
              <form className="grid gap-[9px]" onSubmit={onSaveLLM}>
                <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Provider<input value={llmForm.provider} onChange={(e) => setLLMForm({ ...llmForm, provider: e.target.value })} /></label>
                <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">API protocol<input value={llmForm.apiProtocol} onChange={(e) => setLLMForm({ ...llmForm, apiProtocol: e.target.value })} /></label>
                <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Base URL<input value={llmForm.baseUrl} onChange={(e) => setLLMForm({ ...llmForm, baseUrl: e.target.value })} placeholder="http://example.local:8084" /></label>
                <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">API key<input value={llmForm.apiKey} onChange={(e) => setLLMForm({ ...llmForm, apiKey: e.target.value })} type="password" placeholder={llmConnection ? "Required to update" : "Required"} /></label>
                <button type="submit"><Save size={14} />Save connection</button>
              </form>
              <div className="flex items-center justify-between gap-3 mt-2">
                <h3 className="text-sm">Models</h3>
                <button type="button" onClick={() => void onRefreshModels()}><RefreshCw size={14} />Refresh</button>
              </div>
              <form className="flex gap-[7px]" onSubmit={onManualModel}>
                <input className="min-w-0" value={manualModelId} onChange={(e) => setManualModelId(e.target.value)} placeholder="modelId" />
                <button type="submit"><Plus size={14} />Add</button>
              </form>
              <div className="grid gap-[9px]">
                {models.map((model) => (
                  <label key={model.id || model.modelId} className={`grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 min-h-9 rounded-xl bg-black/[0.03] shadow-[inset_0_0_0_1px_rgba(0,0,0,0.07)] px-2.5 py-2 ${model.enabled ? "selected" : ""}`}>
                    <input type="checkbox" className="w-auto" checked={model.enabled} onChange={(e) => void onUpsertModel(model.modelId, e.target.checked, model.source)} />
                    <span className="min-w-0 overflow-wrap-anywhere">{model.modelId}</span>
                    <small className="text-apple-fg-50">{model.source}</small>
                  </label>
                ))}
                {models.length === 0 && <p className="empty-copy">No models configured.</p>}
              </div>
            </div>
          )}
          {settingsTab === "skills" && (
            <div className="min-h-0 grid content-start gap-3.5 px-5 pt-[18px] pb-[22px]">
              <div className="flex flex-wrap gap-2">
                <button type="button" onClick={onNewSkill}><Plus size={14} />New skill</button>
              </div>
              <div className="grid gap-[9px] overflow-auto min-h-0 pr-0.5">
                {skills.map((skill) => (
                  <div className={`min-w-0 rounded-lg grid grid-cols-[1fr_auto] items-center hover:bg-apple-fg-5 active:bg-apple-fg-7 ${skillEditorOpen && skillForm.slug === skill.definition.slug ? "bg-apple-fg-7" : ""}`} key={skill.definition.slug}>
                    <button type="button" className="w-full min-h-9 grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 px-2 py-[7px] bg-transparent shadow-none text-left justify-stretch" onClick={() => void onLoadSkill(skill.definition.slug)}>
                      <FileText className="text-apple-fg-50" size={15} />
                      <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{skill.definition.slug}</span>
                      <small className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-apple-fg-50 text-[12px]">{skill.definition.name || "skill"}</small>
                    </button>
                    <button type="button" className="min-h-7 w-7 p-0 bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5 danger-text" aria-label={`Delete ${skill.definition.slug}`} onClick={() => void onDeleteSkill(skill.definition.slug)}>
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
                {skills.length === 0 && <p className="empty-copy">No workspace skills.</p>}
              </div>
              {skillEditorOpen && (
                <form className="grid gap-[9px]" onSubmit={onSaveSkill}>
                  <div className="grid grid-cols-2 gap-3">
                    <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Slug<input value={skillForm.slug} onChange={(e) => setSkillForm({ ...skillForm, slug: e.target.value })} /></label>
                    <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Name<input value={skillForm.name} onChange={(e) => setSkillForm({ ...skillForm, name: e.target.value })} /></label>
                    <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase span-2">Description<input value={skillForm.description} onChange={(e) => setSkillForm({ ...skillForm, description: e.target.value })} /></label>
                    <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase span-2">File path<input value={skillForm.path} onChange={(e) => setSkillForm({ ...skillForm, path: e.target.value })} /></label>
                  </div>
                  <label className="min-h-0 grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Content<textarea className="min-h-[min(520px,55vh)] resize-y font-mono text-[13px] leading-relaxed whitespace-pre" value={skillForm.content} onChange={(e) => setSkillForm({ ...skillForm, content: e.target.value })} /></label>
                  <div className="flex flex-wrap gap-2 justify-between items-center">
                    <span className="text-apple-fg-50 text-[12px]">Saved skills affect new sessions.</span>
                    <button type="submit"><Save size={14} />Save skill</button>
                  </div>
                </form>
              )}
            </div>
          )}
          {settingsTab === "mcp" && (
            <div className="min-h-0 grid content-start gap-3.5 px-5 pt-[18px] pb-[22px]">
              <div className="flex flex-wrap gap-2">
                <button type="button" onClick={onNewMCP}><Plus size={14} />New server</button>
              </div>
              <div className="grid gap-[9px] overflow-auto min-h-0 pr-0.5">
                {mcpServers.map((server) => (
                  <div className={`min-w-0 rounded-lg grid grid-cols-[1fr_auto] items-center hover:bg-apple-fg-5 active:bg-apple-fg-7 ${mcpEditorOpen && mcpForm.name === server.definition.name ? "bg-apple-fg-7" : ""}`} key={server.definition.name}>
                    <button type="button" className="w-full min-h-9 grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 px-2 py-[7px] bg-transparent shadow-none text-left justify-stretch" onClick={() => void onLoadMCP(server.definition.name)}>
                      <Server className="text-apple-fg-50" size={15} />
                      <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{server.definition.name}</span>
                      <small className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-apple-fg-50 text-[12px]">{server.definition.transport || "stdio"}</small>
                    </button>
                    <button type="button" className="min-h-7 w-7 p-0 bg-transparent shadow-none text-apple-fg-50 hover:bg-apple-fg-5 danger-text" aria-label={`Delete ${server.definition.name}`} onClick={() => void onDeleteMCP(server.definition.name)}>
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
                {mcpServers.length === 0 && <p className="empty-copy">No MCP servers.</p>}
              </div>
              {mcpEditorOpen && (
                <form className="grid gap-[9px]" onSubmit={onSaveMCP}>
                  <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Name<input value={mcpForm.name} onChange={(e) => setMCPForm({ ...mcpForm, name: e.target.value })} /></label>
                  <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Command<input value={mcpForm.command} onChange={(e) => setMCPForm({ ...mcpForm, command: e.target.value })} /></label>
                  <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Args<textarea value={mcpForm.args} onChange={(e) => setMCPForm({ ...mcpForm, args: e.target.value })} placeholder="One argument per line" /></label>
                  <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Transport<input value={mcpForm.transport} onChange={(e) => setMCPForm({ ...mcpForm, transport: e.target.value })} /></label>
                  <label className="grid gap-[5px] text-apple-fg-50 text-[11px] font-semibold uppercase">Env<textarea value={mcpForm.env} onChange={(e) => setMCPForm({ ...mcpForm, env: e.target.value })} placeholder="KEY=value" /></label>
                  <button type="submit"><Save size={14} />Save MCP</button>
                </form>
              )}
            </div>
          )}
        </div>
      </div>
    </main>
  );
}
