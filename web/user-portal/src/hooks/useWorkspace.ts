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
  refreshModels,
  saveLLMConnection,
  saveMCPServer,
  saveSkill,
  upsertModel
} from "../api";
import { errorMessage, replaceBy, lines, envMap } from "../lib/utils";
import type { LLMConnection, LLMModel, MCPServerDefinitionWithEnv, PodInfo, SkillDefinitionWithFiles } from "../types";
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

const initialSkillForm = {
  slug: "",
  name: "",
  description: "",
  path: "SKILL.md",
  content: "# Skill\n\n"
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
  const [skillForm, setSkillForm] = useState(initialSkillForm);
  const [skillEditorOpen, setSkillEditorOpen] = useState(false);

  const [mcpServers, setMCPServers] = useState<MCPServerDefinitionWithEnv[]>([]);
  const [mcpForm, setMCPForm] = useState(initialMCPForm);
  const [mcpEditorOpen, setMCPEditorOpen] = useState(false);

  const reportError = useCallback((error: unknown, fallback: string) => {
    setNotice({ tone: "error", text: errorMessage(error, fallback) });
  }, []);

  const refreshWorkspace = useCallback(async () => {
    setLoadState("loading");
    setNotice(null);
    try {
      const [health, llm, modelList, skillList, mcpList] = await Promise.all([
        getHealth(),
        getLLMConnection(workspaceId),
        listModels(workspaceId),
        listSkills(workspaceId),
        listMCPServers(workspaceId)
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
      const payload = await saveLLMConnection(workspaceId, llmForm);
      setLLMConnection(payload.connection);
      setApiKeySet(payload.apiKeySet);
      setLLMForm((c) => ({ ...c, apiKey: "" }));
      setNotice({ tone: "success", text: "LLM 连接已保存" });
    } catch (error) {
      reportError(error, "保存 LLM 连接失败");
    }
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
    } catch (error) {
      reportError(error, "保存模型失败");
    }
  }

  async function handleManualModel(event: FormEvent) {
    event.preventDefault();
    await handleUpsertModel(manualModelId, true, "manual");
    setManualModelId("");
  }

  function handleNewSkill() {
    setSkillForm(initialSkillForm);
    setSkillEditorOpen(true);
  }

  async function handleLoadSkill(slug: string) {
    try {
      const skill = await getSkill(workspaceId, slug);
      setSkillEditorOpen(true);
      setSkillForm({
        slug: skill.definition.slug,
        name: skill.definition.name ?? "",
        description: skill.definition.description ?? "",
        path: skill.files[0]?.path ?? "SKILL.md",
        content: skill.files[0]?.content ?? ""
      });
    } catch (error) {
      reportError(error, "读取 Skill 失败");
    }
  }

  async function handleSaveSkill(event: FormEvent) {
    event.preventDefault();
    try {
      const saved = await saveSkill(workspaceId, skillForm.slug.trim(), {
        name: skillForm.name,
        description: skillForm.description,
        files: [{ path: skillForm.path, content: skillForm.content }]
      });
      setSkills((c) => replaceBy(c, saved, (s) => s.definition.slug));
      setSkillEditorOpen(true);
      setNotice({ tone: "success", text: "Skill 已保存，对新 session 生效" });
    } catch (error) {
      reportError(error, "保存 Skill 失败");
    }
  }

  async function handleDeleteSkill(slug: string) {
    try {
      await deleteSkill(workspaceId, slug);
      setSkills((c) => c.filter((s) => s.definition.slug !== slug));
      if (skillForm.slug === slug) {
        setSkillForm(initialSkillForm);
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

  return {
    loadState, healthOk, pod, logs, showLogs, notice, setNotice,
    llmConnection, apiKeySet, llmForm, setLLMForm, models, manualModelId, setManualModelId,
    skills, skillForm, setSkillForm, skillEditorOpen,
    mcpServers, mcpForm, setMCPForm, mcpEditorOpen,
    refreshWorkspace,
    handleLoadLogs,
    handleSaveLLM, handleRefreshModels, handleUpsertModel, handleManualModel,
    handleNewSkill, handleLoadSkill, handleSaveSkill, handleDeleteSkill, handleCloseSkillEditor,
    handleNewMCP, handleLoadMCP, handleSaveMCP, handleDeleteMCP, handleCloseMCPEditor
  };
}
