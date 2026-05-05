import { createInterface, type Interface } from "node:readline";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import type { ToolDefinition } from "@mariozechner/pi-coding-agent";

type JsonObject = Record<string, unknown>;

type MCPServerConfig = {
  command: string;
  args?: string[];
  transport?: string;
  env?: Record<string, string>;
};

type MCPConfig = {
  mcpServers?: Record<string, MCPServerConfig>;
};

type JSONRPCResponse = {
  jsonrpc?: string;
  id?: string | number | null;
  result?: unknown;
  error?: { code?: number; message?: string; data?: unknown };
};

type PendingRequest = {
  resolve: (value: unknown) => void;
  reject: (error: Error) => void;
  timer: NodeJS.Timeout;
};

type MCPTool = {
  name: string;
  description?: string;
  inputSchema?: JsonObject;
};

type MCPToolListResult = {
  tools?: MCPTool[];
};

type MCPToolCallResult = {
  content?: Array<{ type?: string; text?: string; data?: string; mimeType?: string }>;
  isError?: boolean;
  [key: string]: unknown;
};

type ManagedMCPTool = {
  client: StdioMCPClient;
  serverName: string;
  remoteToolName: string;
  exposedToolName: string;
  tool: MCPTool;
};

const REQUEST_TIMEOUT_MS = 30_000;
const SUPPORTED_PROTOCOL_VERSION = "2024-11-05";

export class MCPToolBridge {
  private clients: StdioMCPClient[] = [];

  async loadTools(cwd: string): Promise<ToolDefinition[]> {
    const configPath = join(cwd, ".agents", "mcp.json");
    if (!existsSync(configPath)) {
      return [];
    }
    const config = parseMCPConfig(readFileSync(configPath, "utf8"));
    const servers = Object.entries(config.mcpServers ?? {});
    const managedTools: ManagedMCPTool[] = [];
    for (const [serverName, serverConfig] of servers) {
      if (serverConfig.transport && serverConfig.transport !== "stdio") {
        console.warn(`Skipping MCP server ${serverName}: unsupported transport ${serverConfig.transport}`);
        continue;
      }
      const client = new StdioMCPClient(serverName, serverConfig);
      try {
        await client.start();
        this.clients.push(client);
        const tools = await client.listTools();
        for (const tool of tools) {
          managedTools.push({
            client,
            serverName,
            remoteToolName: tool.name,
            exposedToolName: exposedToolName(serverName, tool.name),
            tool,
          });
        }
      } catch (error) {
        await client.close();
        console.warn(`Skipping MCP server ${serverName}: ${error instanceof Error ? error.message : String(error)}`);
      }
    }
    return managedTools.map((managedTool) => toolDefinition(managedTool));
  }

  async close(): Promise<void> {
    const clients = this.clients.splice(0);
    await Promise.allSettled(clients.map((client) => client.close()));
  }
}

class StdioMCPClient {
  private child?: ChildProcessWithoutNullStreams;
  private lines?: Interface;
  private nextID = 1;
  private pending = new Map<string | number, PendingRequest>();

  constructor(
    private readonly name: string,
    private readonly config: MCPServerConfig,
  ) {}

  async start(): Promise<void> {
    if (!this.config.command?.trim()) {
      throw new Error("command is required");
    }
    this.child = spawn(this.config.command, this.config.args ?? [], {
      stdio: ["pipe", "pipe", "pipe"],
      env: { ...process.env, ...(this.config.env ?? {}) },
    });
    this.child.stderr.on("data", (chunk: Buffer) => {
      const text = chunk.toString().trimEnd();
      if (text) console.warn(`[mcp:${this.name}] ${text}`);
    });
    this.child.on("exit", (code, signal) => {
      const reason = signal ? `signal ${signal}` : `code ${code}`;
      this.rejectAll(new Error(`MCP server ${this.name} exited with ${reason}`));
    });
    this.child.on("error", (error) => this.rejectAll(error));
    this.lines = createInterface({ input: this.child.stdout, crlfDelay: Infinity });
    this.lines.on("line", (line) => this.handleLine(line));

    await this.request("initialize", {
      protocolVersion: SUPPORTED_PROTOCOL_VERSION,
      capabilities: {},
      clientInfo: { name: "agenthub-ts-runtime-host", version: "0.1.0" },
    });
    this.notify("notifications/initialized", {});
  }

  async listTools(): Promise<MCPTool[]> {
    const result = (await this.request("tools/list", {})) as MCPToolListResult;
    return Array.isArray(result.tools) ? result.tools.filter(isMCPTool) : [];
  }

  async callTool(name: string, args: JsonObject): Promise<MCPToolCallResult> {
    return (await this.request("tools/call", { name, arguments: args })) as MCPToolCallResult;
  }

  async close(): Promise<void> {
    this.rejectAll(new Error(`MCP server ${this.name} closed`));
    this.lines?.close();
    if (this.child && !this.child.killed) {
      this.child.kill();
    }
  }

  private request(method: string, params: JsonObject): Promise<unknown> {
    const id = this.nextID++;
    const payload = { jsonrpc: "2.0", id, method, params };
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`MCP request timed out: ${method}`));
      }, REQUEST_TIMEOUT_MS);
      this.pending.set(id, { resolve, reject, timer });
      this.write(payload, reject);
    });
  }

  private notify(method: string, params: JsonObject): void {
    this.write({ jsonrpc: "2.0", method, params });
  }

  private write(payload: JsonObject, onError?: (error: Error) => void): void {
    const line = `${JSON.stringify(payload)}\n`;
    this.child?.stdin.write(line, (error) => {
      if (error && onError) onError(error);
    });
  }

  private handleLine(line: string): void {
    const trimmed = line.trim();
    if (!trimmed) return;
    let message: JSONRPCResponse;
    try {
      message = JSON.parse(trimmed) as JSONRPCResponse;
    } catch {
      console.warn(`[mcp:${this.name}] ignored non-json stdout line: ${trimmed}`);
      return;
    }
    if (message.id === undefined || message.id === null) {
      return;
    }
    const pending = this.pending.get(message.id);
    if (!pending) {
      return;
    }
    this.pending.delete(message.id);
    clearTimeout(pending.timer);
    if (message.error) {
      pending.reject(new Error(message.error.message ?? `MCP request failed: ${JSON.stringify(message.error)}`));
    } else {
      pending.resolve(message.result);
    }
  }

  private rejectAll(error: Error): void {
    for (const [id, pending] of this.pending) {
      this.pending.delete(id);
      clearTimeout(pending.timer);
      pending.reject(error);
    }
  }
}

function toolDefinition(managedTool: ManagedMCPTool): ToolDefinition {
  const description = managedTool.tool.description ?? `MCP tool ${managedTool.remoteToolName} from server ${managedTool.serverName}`;
  return {
    name: managedTool.exposedToolName,
    label: managedTool.exposedToolName,
    description: `${description}\nOriginal MCP server/tool: ${managedTool.serverName}/${managedTool.remoteToolName}.`,
    promptSnippet: `Call MCP tool ${managedTool.exposedToolName} (${managedTool.serverName}/${managedTool.remoteToolName})`,
    parameters: normalizeInputSchema(managedTool.tool.inputSchema),
    async execute(_toolCallId, params) {
      const result = await managedTool.client.callTool(managedTool.remoteToolName, asJsonObject(params));
      return {
        content: normalizeToolContent(result),
        details: { server: managedTool.serverName, tool: managedTool.remoteToolName, result },
      };
    },
  };
}

function parseMCPConfig(content: string): MCPConfig {
  const parsed = JSON.parse(content) as MCPConfig;
  return parsed && typeof parsed === "object" ? parsed : {};
}

function isMCPTool(value: unknown): value is MCPTool {
  if (!value || typeof value !== "object") return false;
  const tool = value as { name?: unknown };
  return typeof tool.name === "string" && tool.name.trim().length > 0;
}

function exposedToolName(serverName: string, toolName: string): string {
  return `${sanitizeToolName(serverName)}__${sanitizeToolName(toolName)}`.slice(0, 64);
}

function sanitizeToolName(value: string): string {
  const sanitized = value.trim().replace(/[^a-zA-Z0-9]/g, "_").replace(/_+/g, "_");
  return sanitized || "tool";
}

function normalizeInputSchema(schema: JsonObject | undefined): JsonObject {
  if (!schema || typeof schema !== "object") {
    return { type: "object", properties: {}, required: [] };
  }
  const normalized = { ...schema };
  normalized.type = "object";
  if (!normalized.properties || typeof normalized.properties !== "object") {
    normalized.properties = {};
  }
  if (!Array.isArray(normalized.required)) {
    normalized.required = [];
  }
  return normalized;
}

function asJsonObject(value: unknown): JsonObject {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as JsonObject) : {};
}

type ToolContent = { type: "text"; text: string } | { type: "image"; data: string; mimeType: string };

function normalizeToolContent(result: MCPToolCallResult): ToolContent[] {
  const normalized: ToolContent[] = [];
  for (const item of result.content ?? []) {
    if (item.type === "text") {
      normalized.push({ type: "text", text: item.text ?? "" });
    } else if (item.type === "image" && item.data && item.mimeType) {
      normalized.push({ type: "image", data: item.data, mimeType: item.mimeType });
    }
  }
  if (normalized.length > 0) {
    return normalized;
  }
  return [{ type: "text", text: JSON.stringify(result) }];
}
