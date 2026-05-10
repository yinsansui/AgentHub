import { useCallback, useState } from "react";
import {
  getLLMConnection,
  getMCPServer,
  getPod,
  getWorkspaceLogs,
  deleteMCPServer,
  deleteSkill,
  getSkill,
  getHealth,
  listMCPServers,
  listModels,
  listSkills,
  listWorkspacePlugins,
  installWorkspacePlugin,
  refreshModels,
  saveLLMConnection,
  saveMCPServer,
  saveSkill,
  upsertModel
} from "../api";
import { errorMessage, replaceBy, lines, envMap } from "../lib/utils";
import type { LLMConnection, LLMModel, MCPServerDefinitionWithEnv, PodInfo, SkillDefinitionWithFiles, WorkspacePlugin } from "../types";
import type { FormEvent } from "react";

type LoadState = "idle" | "loading" | "ready" | "error";

export type Notice = {
  tone: "info" | "error" | "success";
  text: string;
};

const initialLLMForm = {
  provider: "anthropic",
  apiProtocol: "anthropic-messages",
  baseUrl: "",
  apiKey: ""
};

export type SkillFileEntry = { path: string; content: string };

export type SkillEditorState = {
  slug: string;
  name: string;
  description: string;
  files: SkillFileEntry[];
  selectedPath: string;
  savedFiles: SkillFileEntry[];
  localFolders: string[];
};

const INITIAL_SKILL_FILE: SkillFileEntry = { path: "SKILL.md", content: "# Skill\n\n" };

const initialSkillEditor: SkillEditorState = {
  slug: "",
  name: "",
  description: "",
  files: [INITIAL_SKILL_FILE],
  selectedPath: "SKILL.md",
  savedFiles: [INITIAL_SKILL_FILE],
  localFolders: [],
};

const initialMCPForm = {
  name: "",
  command: "",
  args: "",
  transport: "stdio",
  env: ""
};

export function useWorkspace(workspaceId: string) {
  const [loadState, setLoadState] = useState<LoadState>("idle");
  const [healthOk, setHealthOk] = useState<boolean | null>(null);
  const [pod, setPod] = useState<PodInfo | null>(null);
  const [logs, setLogs] = useState("");
  const [showLogs, setShowLogs] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);

  const [llmConnection, setLLMConnection] = useState<LLMConnection | null>(null);
  const [apiKeySet, setApiKeySet] = useState(false);
  const [llmForm, setLLMForm] = useState(initialLLMForm);
  const [models, setModels] = useState<LLMModel[]>([]);
  const [manualModelId, setManualModelId] = useState("");

  const [skills, setSkills] = useState<SkillDefinitionWithFiles[]>([]);
  const [skillEditor, setSkillEditor] = useState<SkillEditorState>(initialSkillEditor);
  const [skillEditorOpen, setSkillEditorOpen] = useState(false);

  const [mcpServers, setMCPServers] = useState<MCPServerDefinitionWithEnv[]>([]);
  const [mcpForm, setMCPForm] = useState(initialMCPForm);
  const [mcpEditorOpen, setMCPEditorOpen] = useState(false);

  const [plugins, setPlugins] = useState<WorkspacePlugin[]>([]);

  const reportError = useCallback((error: unknown, fallback: string) => {
    setNotice({ tone: "error", text: errorMessage(error, fallback) });
  }, []);

  const refreshWorkspace = useCallback(async () => {
    if (!workspaceId) return;
    setLoadState("loading");
    setNotice(null);
    try {
      const [health, llm, modelList, skillList, mcpList, pluginList] = await Promise.all([
        getHealth(),
        getLLMConnection(workspaceId),
        listModels(workspaceId),
        listSkills(workspaceId),
        listMCPServers(workspaceId),
        listWorkspacePlugins(workspaceId)
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
      setPlugins(pluginList);
      try { setPod(await getPod(workspaceId)); } catch { setPod(null); }
      setLoadState("ready");
    } catch (error) {
      setLoadState("error");
      reportError(error, "加载 workspace 失败");
    }
  }, [reportError, workspaceId]);

  async function handleLoadLogs() {
    try {
      setLogs(await getWorkspaceLogs(workspaceId));
      setShowLogs(true);
    } catch (error) {
      reportError(error, "读取日志失败");
    }
  }

  async function handleSaveLLM(event: FormEvent) {
    event.preventDefault();
    try {
      const payload = await saveLLMConnection(workspaceId, {
        apiProtocol: llmForm.apiProtocol,
        baseUrl: llmForm.baseUrl,
        apiKey: llmForm.apiKey,
        defaultModelId: llmConnection?.defaultModelId
      });
      setLLMConnection(payload.connection);
      setApiKeySet(payload.apiKeySet);
      setLLMForm((c) => ({ ...c, apiKey: "" }));
      try {
        const refreshed = await refreshModels(workspaceId);
        setModels(refreshed);
        setNotice({ tone: "success", text: "LLM 连接已保存，模型已刷新" });
      } catch (refreshError) {
        reportError(refreshError, "LLM 连接已保存，但自动刷新模型失败");
      }
    } catch (error) {
      reportError(error, "保存 LLM 连接失败");
    }
  }

  async function saveDefaultModel(modelId: string) {
    if (!llmConnection) {
      throw new Error("请先保存 LLM 连接");
    }
    const payload = await saveLLMConnection(workspaceId, {
      apiProtocol: llmConnection.apiProtocol,
      baseUrl: llmConnection.baseUrl,
      apiKey: "",
      defaultModelId: modelId
    });
    setLLMConnection(payload.connection);
    setApiKeySet(payload.apiKeySet);
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
      setModels((c) => replaceBy(c, saved, (m) => m.modelId));
      if (!saved.enabled && llmConnection?.defaultModelId === saved.modelId) {
        await saveDefaultModel("");
      }
    } catch (error) {
      reportError(error, "保存模型失败");
    }
  }

  async function handleManualModel(event: FormEvent) {
    event.preventDefault();
    const modelId = manualModelId.trim();
    if (!modelId) return;
    await handleUpsertModel(modelId, true, "manual");
    setManualModelId("");
    if (!llmConnection?.defaultModelId) {
      try {
        await saveDefaultModel(modelId);
      } catch (error) {
        reportError(error, "设置默认模型失败");
      }
    }
  }

  function handleNewSkill() {
    setSkillEditor(initialSkillEditor);
    setSkillEditorOpen(true);
  }

  async function handleLoadSkill(slug: string) {
    try {
      const skill = await getSkill(workspaceId, slug);
      const files: SkillFileEntry[] = skill.files.length > 0
        ? skill.files.map((f) => ({ path: f.path, content: f.content }))
        : [INITIAL_SKILL_FILE];
      const selectedPath = files.some((f) => f.path === "SKILL.md") ? "SKILL.md" : files[0].path;
      setSkillEditor({
        slug: skill.definition.slug,
        name: skill.definition.name ?? "",
        description: skill.definition.description ?? "",
        files,
        selectedPath,
        savedFiles: files.map((f) => ({ ...f })),
        localFolders: [],
      });
      setSkillEditorOpen(true);
    } catch (error) {
      reportError(error, "读取 Skill 失败");
    }
  }

  async function handleSaveSkill(event: FormEvent) {
    event.preventDefault();
    try {
      const saved = await saveSkill(workspaceId, skillEditor.slug.trim(), {
        name: skillEditor.name,
        description: skillEditor.description,
        files: skillEditor.files,
      });
      setSkills((c) => replaceBy(c, saved, (s) => s.definition.slug));
      setSkillEditor((prev) => ({ ...prev, savedFiles: prev.files.map((f) => ({ ...f })) }));
      setNotice({ tone: "success", text: "Skill 已保存，对新 session 生效" });
    } catch (error) {
      reportError(error, "保存 Skill 失败");
    }
  }

  async function handleDeleteSkill(slug: string) {
    try {
      await deleteSkill(workspaceId, slug);
      setSkills((c) => c.filter((s) => s.definition.slug !== slug));
      if (skillEditor.slug === slug) {
        setSkillEditor(initialSkillEditor);
        setSkillEditorOpen(false);
      }
    } catch (error) {
      reportError(error, "删除 Skill 失败");
    }
  }

  function handleCloseSkillEditor() {
    setSkillEditorOpen(false);
  }

  function handleNewMCP() {
    setMCPForm(initialMCPForm);
    setMCPEditorOpen(true);
  }

  async function handleLoadMCP(name: string) {
    try {
      const server = await getMCPServer(workspaceId, name);
      setMCPEditorOpen(true);
      setMCPForm({
        name: server.definition.name,
        command: server.definition.command,
        args: (server.definition.args ?? []).join("\n"),
        transport: server.definition.transport || "stdio",
        env: server.env.map((e) => `${e.name}=${e.value}`).join("\n")
      });
    } catch (error) {
      reportError(error, "读取 MCP 服务器失败");
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
      setMCPServers((c) => replaceBy(c, saved, (s) => s.definition.name));
      setMCPEditorOpen(true);
      setNotice({ tone: "success", text: "MCP 服务器已保存，对新 session 生效" });
    } catch (error) {
      reportError(error, "保存 MCP 服务器失败");
    }
  }

  async function handleDeleteMCP(name: string) {
    try {
      await deleteMCPServer(workspaceId, name);
      setMCPServers((c) => c.filter((s) => s.definition.name !== name));
      if (mcpForm.name === name) {
        setMCPForm(initialMCPForm);
        setMCPEditorOpen(false);
      }
    } catch (error) {
      reportError(error, "删除 MCP 服务器失败");
    }
  }

  function handleCloseMCPEditor() {
    setMCPEditorOpen(false);
  }

  async function handleSavePlugin(pluginId: string, config: Record<string, unknown>) {
    try {
      const saved = await installWorkspacePlugin(workspaceId, pluginId, config);
      setPlugins((c) => replaceBy(c, saved, (p) => p.id));
      setNotice({ tone: "success", text: "插件已保存，对新 session 生效" });
    } catch (error) {
      reportError(error, "保存插件配置失败");
    }
  }

  async function handleSetDefaultModel(modelId: string) {
    try {
      await saveDefaultModel(modelId);
    } catch (error) {
      reportError(error, "设置默认模型失败");
    }
  }

  return {
    loadState, healthOk, pod, logs, showLogs, notice, setNotice,
    llmConnection, apiKeySet, llmForm, setLLMForm, models, manualModelId, setManualModelId,
    skills, skillEditor, setSkillEditor, skillEditorOpen,
    mcpServers, mcpForm, setMCPForm, mcpEditorOpen,
    plugins,
    refreshWorkspace,
    handleLoadLogs,
    handleSaveLLM, handleRefreshModels, handleUpsertModel, handleManualModel, handleSetDefaultModel,
    handleNewSkill, handleLoadSkill, handleSaveSkill, handleDeleteSkill, handleCloseSkillEditor,
    handleNewMCP, handleLoadMCP, handleSaveMCP, handleDeleteMCP, handleCloseMCPEditor,
    handleSavePlugin
  };
}
