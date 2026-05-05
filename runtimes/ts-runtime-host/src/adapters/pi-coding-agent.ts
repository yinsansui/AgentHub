import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import {
  AuthStorage,
  createAgentSession,
  DefaultResourceLoader,
  ModelRegistry,
  SessionManager,
  SettingsManager,
  type AgentSession,
  type AgentSessionEvent,
} from "@mariozechner/pi-coding-agent";
import { MCPToolBridge } from "../mcp.js";
import { baseEvent, type RunCommand, type UniversalBlock, type UniversalEvent } from "../protocol.js";

export type RuntimeEventSink = (event: UniversalEvent) => void;

export interface RuntimeAdapter {
  run(command: RunCommand, emit: RuntimeEventSink): Promise<void>;
  abort(runId: string): Promise<void>;
  shutdown(): Promise<void>;
}

type TextContent = { type: "text"; text: string };
type ThinkingContent = { type: "thinking"; thinking: string };
type ToolCallContent = { type: "toolCall"; id: string; name: string; arguments: Record<string, unknown> };
type AssistantMessage = { role: "assistant"; content: Array<TextContent | ThinkingContent | ToolCallContent>; errorMessage?: string };
type MessageUpdateEvent = AgentSessionEvent & {
  type: "message_update";
  assistantMessageEvent?: {
    type?: string;
    contentIndex?: number;
    delta?: string;
    content?: string;
    toolCall?: ToolCallContent;
    partial?: AssistantMessage;
  };
};
type ProviderConfig = Parameters<ModelRegistry["registerProvider"]>[1];
type RegisteredModel = NonNullable<ProviderConfig["models"]>[number];
type ProviderApi = NonNullable<ProviderConfig["api"]>;

interface RuntimeModelConfig {
  provider: string;
  modelId?: string;
  baseUrl?: string;
  apiKeyEnv?: string;
  apiKey?: string;
  api: ProviderApi;
}

interface ActiveRunState {
  command: RunCommand;
  emit: RuntimeEventSink;
  assistantIndex: number;
  currentAssistantMessageId?: string;
}

export class PiCodingAgentAdapter implements RuntimeAdapter {
  private session?: AgentSession;
  private sessionCwd?: string;
  private active?: ActiveRunState;
  private mcpBridge?: MCPToolBridge;

  async run(command: RunCommand, emit: RuntimeEventSink): Promise<void> {
    if (this.active) {
      throw new Error(`runtime is already processing run ${this.active.command.runId}`);
    }
    const session = await this.ensureSession(command.cwd);
    this.active = { command, emit, assistantIndex: 0 };
    try {
      await session.prompt(command.message, { source: "rpc" });
    } catch (error) {
      emit(this.errorEvent(command, error));
      emit({ ...baseEvent("session.ended", command), metadata: { reason: "error", adapter: "pi-coding-agent" } });
    } finally {
      this.active = undefined;
    }
  }

  async abort(runId: string): Promise<void> {
    if (!this.active || this.active.command.runId !== runId || !this.session) {
      return;
    }
    await this.session.abort();
  }

  async shutdown(): Promise<void> {
    this.session?.dispose();
    await this.mcpBridge?.close();
    this.mcpBridge = undefined;
  }

  private async ensureSession(cwd: string): Promise<AgentSession> {
    if (this.session && this.sessionCwd === cwd) {
      return this.session;
    }
    if (this.session) {
      this.session.dispose();
      this.session = undefined;
    }
    await this.mcpBridge?.close();
    this.mcpBridge = undefined;

    const agentDir = join(cwd, ".agenthub", "pi-agent");
    await mkdir(agentDir, { recursive: true });
    const settingsManager = SettingsManager.create(cwd, agentDir);
    const authStorage = AuthStorage.create(join(agentDir, "auth.json"));
    const modelRegistry = ModelRegistry.create(authStorage, join(agentDir, "models.json"));
    const runtimeModel = runtimeModelConfig();
    if (runtimeModel.apiKey) {
      authStorage.setRuntimeApiKey(runtimeModel.provider, runtimeModel.apiKey);
    }
    if (runtimeModel.modelId || runtimeModel.baseUrl) {
      modelRegistry.registerProvider(runtimeModel.provider, providerConfig(runtimeModel));
    }
    const selectedModel = runtimeModel.modelId ? modelRegistry.find(runtimeModel.provider, runtimeModel.modelId) : undefined;
    if (runtimeModel.modelId && !selectedModel) {
      throw new Error(`configured model not found: ${runtimeModel.provider}/${runtimeModel.modelId}`);
    }
    const resourceLoader = new DefaultResourceLoader({
      cwd,
      agentDir,
      settingsManager,
      additionalSkillPaths: [join(cwd, ".agents", "skills")],
    });
    await resourceLoader.reload();
    const mcpBridge = new MCPToolBridge();
    const mcpTools = await mcpBridge.loadTools(cwd);

    const { session } = await createAgentSession({
      cwd,
      agentDir,
      settingsManager,
      authStorage,
      modelRegistry,
      model: selectedModel,
      resourceLoader,
      sessionManager: SessionManager.inMemory(),
      customTools: mcpTools,
    });
    session.subscribe((event) => this.handleEvent(event));
    this.mcpBridge = mcpBridge;
    this.session = session;
    this.sessionCwd = cwd;
    return session;
  }

  private handleEvent(event: AgentSessionEvent): void {
    const active = this.active;
    if (!active) {
      return;
    }
    const { command, emit } = active;
    switch (event.type) {
      case "agent_start":
        emit({ ...baseEvent("session.started", command), metadata: { adapter: "pi-coding-agent" } });
        return;
      case "message_start":
        if (event.message.role !== "assistant") return;
        active.currentAssistantMessageId = `msg_${command.runId}_${active.assistantIndex++}`;
        emit({
          ...baseEvent("message.started", command),
          messageId: active.currentAssistantMessageId,
          role: "assistant",
        });
        return;
      case "message_update": {
        const update = event as MessageUpdateEvent;
        if (update.message.role !== "assistant") return;
        this.emitContentEvent(command, emit, active.currentAssistantMessageId ?? `msg_${command.runId}_${active.assistantIndex}`, update.assistantMessageEvent);
        return;
      }
      case "message_end":
        if (event.message.role !== "assistant") return;
        this.emitCompletedMessage(command, emit, active.currentAssistantMessageId ?? `msg_${command.runId}_${active.assistantIndex}`, event.message as AssistantMessage);
        return;
      case "agent_end":
        emit({ ...baseEvent("session.ended", command), metadata: { reason: "stop", adapter: "pi-coding-agent" } });
        return;
      default:
        return;
    }
  }

  private emitContentEvent(command: RunCommand, emit: RuntimeEventSink, messageId: string, event: MessageUpdateEvent["assistantMessageEvent"]): void {
    if (!event || typeof event.contentIndex !== "number") return;
    const base = { ...baseEvent(contentEventType(event.type), command), messageId, contentIndex: event.contentIndex, role: "assistant" };
    switch (event.type) {
      case "text_start":
      case "thinking_start":
      case "toolcall_start":
        emit(base);
        return;
      case "text_delta":
      case "thinking_delta":
      case "toolcall_delta":
        if (!event.delta) return;
        emit({ ...base, delta: event.delta, partial: blockPartial(event.partial, event.contentIndex) });
        return;
      case "text_end":
      case "thinking_end":
        emit({ ...base, block: { type: event.type === "text_end" ? "text" : "thinking", text: event.content ?? blockPartial(event.partial, event.contentIndex) } });
        return;
      case "toolcall_end":
        emit({ ...base, block: toolCallBlock(event.toolCall ?? blockAt(event.partial, event.contentIndex)) });
        return;
      default:
        return;
    }
  }

  private emitCompletedMessage(command: RunCommand, emit: RuntimeEventSink, messageId: string, message: AssistantMessage): void {
    const blocks = assistantBlocks(message);
    emit({
      ...baseEvent("message.completed", command),
      messageId,
      role: "assistant",
      content: blocks,
      metadata: message.errorMessage ? { stopReason: "error", errorMessage: message.errorMessage } : undefined,
    });
  }

  private errorEvent(command: RunCommand, error: unknown): UniversalEvent {
    return {
      ...baseEvent("error", command),
      error: { message: error instanceof Error ? error.message : String(error) },
      metadata: { adapter: "pi-coding-agent" },
    };
  }
}

function assistantBlocks(message: AssistantMessage): UniversalBlock[] {
  const blocks: UniversalBlock[] = [];
  for (const content of message.content ?? []) {
    blocks.push(blockFromContent(content));
  }
  if (blocks.length === 0 && message.errorMessage) {
    blocks.push({ type: "text", text: message.errorMessage });
  }
  return blocks;
}

function contentEventType(type: string | undefined): UniversalEvent["type"] {
  switch (type) {
    case "text_start":
      return "text.started";
    case "text_delta":
      return "text.delta";
    case "text_end":
      return "text.completed";
    case "thinking_start":
      return "thinking.started";
    case "thinking_delta":
      return "thinking.delta";
    case "thinking_end":
      return "thinking.completed";
    case "toolcall_start":
      return "tool_call.started";
    case "toolcall_delta":
      return "tool_call.delta";
    case "toolcall_end":
      return "tool_call.completed";
    default:
      return "text.delta";
  }
}

function blockPartial(message: AssistantMessage | undefined, contentIndex: number): string {
  const block = blockAt(message, contentIndex);
  if (!block) return "";
  if (block.type === "text") return block.text;
  if (block.type === "thinking") return block.thinking;
  if (block.type === "toolCall") return JSON.stringify(block.arguments ?? {});
  return "";
}

function blockAt(message: AssistantMessage | undefined, contentIndex: number): TextContent | ThinkingContent | ToolCallContent | undefined {
  return message?.content?.[contentIndex];
}

function toolCallBlock(content: unknown): UniversalBlock {
  if (content && typeof content === "object" && (content as { type?: string }).type === "toolCall") {
    const toolCall = content as ToolCallContent;
    return blockFromContent(toolCall);
  }
  return { type: "tool_call" };
}

function blockFromContent(content: TextContent | ThinkingContent | ToolCallContent): UniversalBlock {
  if (content.type === "text") {
    return { type: "text", text: content.text };
  }
  if (content.type === "thinking") {
    return { type: "thinking", text: content.thinking };
  }
  return { type: "tool_call", name: content.name, input: JSON.stringify(content.arguments ?? {}) };
}

function runtimeModelConfig(): RuntimeModelConfig {
  return {
    provider: env("AGENTHUB_PI_PROVIDER") ?? "anthropic",
    modelId: env("AGENTHUB_PI_MODEL") ?? env("ANTHROPIC_MODEL"),
    baseUrl: env("AGENTHUB_PI_BASE_URL") ?? env("ANTHROPIC_BASE_URL"),
    apiKeyEnv: firstConfiguredEnvName("AGENTHUB_PI_API_KEY", "ANTHROPIC_API_KEY"),
    apiKey: env("AGENTHUB_PI_API_KEY") ?? env("ANTHROPIC_API_KEY"),
    api: (env("AGENTHUB_PI_API") ?? "anthropic-messages") as ProviderApi,
  };
}

function providerConfig(config: RuntimeModelConfig): ProviderConfig {
  const provider: ProviderConfig = { api: config.api };
  if (config.baseUrl) {
    provider.baseUrl = config.baseUrl;
  }
  if (config.modelId) {
    if (config.apiKeyEnv) {
      provider.apiKey = config.apiKeyEnv;
    }
    provider.models = [customModel(config.modelId, config.api)];
  }
  return provider;
}

function customModel(modelId: string, api: ProviderApi): RegisteredModel {
  return {
    id: modelId,
    name: modelId,
    api,
    reasoning: false,
    input: ["text"],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: 128000,
    maxTokens: 16384,
  };
}

function env(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}

function firstConfiguredEnvName(...names: string[]): string | undefined {
  return names.find((name) => env(name));
}
