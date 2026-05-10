import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { existsSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";

type JsonObject = Record<string, unknown>;

type JSONRPCRequest = {
  jsonrpc?: string;
  id?: string | number | null;
  method?: string;
  params?: unknown;
};

type JSONRPCError = {
  code: number;
  message: string;
  data?: unknown;
};

type ToolContent = {
  type: "text";
  text: string;
};

type ToolResult = {
  content: ToolContent[];
  isError?: boolean;
};

type RepositoryConfig = {
  repoName: string;
  repoUrl: string;
};

const PROTOCOL_VERSION = "2024-11-05";
const TOOL_NAME = "clone";
const SCP_LIKE_REPO_URL_PATTERN = /^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._~/-]+$/;

const stdoutWrite = process.stdout.write.bind(process.stdout);
const stderrWrite = process.stderr.write.bind(process.stderr);

for (const level of ["log", "info", "debug", "warn", "error"] as const) {
  console[level] = (...args: unknown[]) => {
    stderrWrite(`${args.map(formatLogValue).join(" ")}\n`);
  };
}

const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });
let lineQueue: Promise<void> = Promise.resolve();

lines.on("line", (line) => {
  const trimmed = line.trim();
  if (!trimmed) return;
  lineQueue = lineQueue.then(() => handleLine(trimmed)).catch((error) => {
    console.error(error);
  });
});

async function handleLine(line: string): Promise<void> {
  let request: JSONRPCRequest;
  try {
    request = JSON.parse(line) as JSONRPCRequest;
  } catch (error) {
    writeError(null, -32700, `Parse error: ${errorMessage(error)}`);
    return;
  }

  if (typeof request.method !== "string") {
    if (request.id !== undefined) {
      writeError(request.id, -32600, "Invalid Request");
    }
    return;
  }

  if (request.id === undefined || request.id === null) {
    handleNotification(request.method);
    return;
  }

  try {
    const result = await handleRequest(request.method, request.params);
    writeResult(request.id, result);
  } catch (error) {
    const rpcError = toJSONRPCError(error);
    writeError(request.id, rpcError.code, rpcError.message, rpcError.data);
  }
}

function handleNotification(method: string): void {
  if (method === "notifications/initialized") {
    return;
  }
  console.warn(`Ignored unsupported notification: ${method}`);
}

async function handleRequest(method: string, params: unknown): Promise<unknown> {
  if (method === "initialize") {
    return {
      protocolVersion: PROTOCOL_VERSION,
      capabilities: { tools: {} },
      serverInfo: { name: "agenthub-repo", version: "0.1.0" },
    };
  }
  if (method === "tools/list") {
    return {
      tools: [
        {
          name: TOOL_NAME,
          description: "Clone the configured repository into this AgentHub task workspace, or return the existing clone path.",
          inputSchema: {
            type: "object",
            properties: {
              repoName: { type: "string", description: "Configured repository name to clone." },
            },
            required: ["repoName"],
          },
        },
      ],
    };
  }
  if (method === "tools/call") {
    return handleToolCall(params);
  }
  throw new RPCError(-32601, `Method not found: ${method}`);
}

async function handleToolCall(params: unknown): Promise<ToolResult> {
  const call = asJsonObject(params);
  if (call.name !== TOOL_NAME) {
    throw new RPCError(-32602, `Unknown tool: ${String(call.name)}`);
  }
  const args = asJsonObject(call.arguments);
  const repoName = validateRequestedRepoName(args.repoName);
  return cloneConfiguredRepo(repoName);
}

async function cloneConfiguredRepo(repoName: string): Promise<ToolResult> {
  const repository = findRepository(repoName);

  const taskRoot = resolve(process.cwd(), "..", "..");
  const reposRoot = join(taskRoot, "repos");
  const targetDir = join(reposRoot, repoName);

  if (existsSync(targetDir)) {
    if (!existsSync(join(targetDir, ".git"))) {
      throw new RPCError(-32000, `repo clone target exists but is not a git worktree: ${targetDir}`);
    }
    return textResult(repoName, targetDir, "existing");
  }

  await mkdir(dirname(targetDir), { recursive: true });
  await runGitClone(repository.repoUrl, targetDir);
  return textResult(repoName, targetDir, "cloned");
}

function findRepository(repoName: string): RepositoryConfig {
  const repositories = readRepositories();
  const repository = repositories.find((candidate) => candidate.repoName === repoName);
  if (!repository) {
    throw new RPCError(-32602, `Unknown repository: ${repoName}`);
  }
  return repository;
}

function readRepositories(): RepositoryConfig[] {
  const raw = process.env.AGENTHUB_REPOSITORIES?.trim();
  if (!raw) {
    throw new RPCError(-32602, "AGENTHUB_REPOSITORIES is required");
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (error) {
    throw new RPCError(-32602, `AGENTHUB_REPOSITORIES must be valid JSON: ${errorMessage(error)}`);
  }

  if (!Array.isArray(parsed) || parsed.length === 0) {
    throw new RPCError(-32602, "AGENTHUB_REPOSITORIES must be a non-empty JSON array");
  }

  const seenRepoNames = new Set<string>();
  return parsed.map((value, index) => {
    const object = asJsonObject(value);
    const repoName = validateConfiguredRepoName(object.repoName, index);
    if (seenRepoNames.has(repoName)) {
      throw new RPCError(-32602, `AGENTHUB_REPOSITORIES contains duplicate repoName: ${repoName}`);
    }
    seenRepoNames.add(repoName);

    const repoUrl = typeof object.repoUrl === "string" ? object.repoUrl.trim() : "";
    validateRepoUrl(repoUrl, `AGENTHUB_REPOSITORIES[${index}].repoUrl`);
    return { repoName, repoUrl };
  });
}

function validateRequestedRepoName(value: unknown): string {
  const repoName = typeof value === "string" ? value.trim() : "";
  if (!repoName) {
    throw new RPCError(-32602, "arguments.repoName must be a non-empty string");
  }
  validateRepoName(repoName, "arguments.repoName");
  return repoName;
}

function validateConfiguredRepoName(value: unknown, index: number): string {
  const repoName = typeof value === "string" ? value.trim() : "";
  if (!repoName) {
    throw new RPCError(-32602, `AGENTHUB_REPOSITORIES[${index}].repoName must be a non-empty string`);
  }
  validateRepoName(repoName, `AGENTHUB_REPOSITORIES[${index}].repoName`);
  return repoName;
}

function validateRepoName(repoName: string, label: string): void {
  if (!/^[A-Za-z0-9._-]{1,80}$/.test(repoName) || repoName === "." || repoName === "..") {
    throw new RPCError(-32602, `${label} must be a safe single path segment`);
  }
}

function validateRepoUrl(repoUrl: string, label: string): void {
  if (!repoUrl) {
    throw new RPCError(-32602, `${label} must be a non-empty string`);
  }
  if (repoUrl.startsWith("-") || /[\u0000-\u001F\u007F]/.test(repoUrl)) {
    throw new RPCError(-32602, `${label} must be a safe Git URL`);
  }
  if (SCP_LIKE_REPO_URL_PATTERN.test(repoUrl)) {
    return;
  }
  let parsed: URL;
  try {
    parsed = new URL(repoUrl);
  } catch {
    throw new RPCError(-32602, `${label} must be an https or ssh Git URL`);
  }
  if (parsed.protocol === "https:") {
    if (parsed.username || parsed.password) {
      throw new RPCError(-32602, `${label} must not include credentials`);
    }
    return;
  }
  if (parsed.protocol === "ssh:") {
    if (parsed.password) {
      throw new RPCError(-32602, `${label} must not include credentials`);
    }
    return;
  }
  throw new RPCError(-32602, `${label} must be an https or ssh Git URL`);
}

function runGitClone(repoUrl: string, targetDir: string): Promise<void> {
  return new Promise((resolvePromise, reject) => {
    const child = spawn("git", ["clone", "--", repoUrl, targetDir], {
      env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
      stdio: ["ignore", "ignore", "pipe"],
    });
    const stderrChunks: Buffer[] = [];
    child.stderr.on("data", (chunk: Buffer) => stderrChunks.push(chunk));
    child.on("error", reject);
    child.on("close", (code, signal) => {
      if (code === 0) {
        resolvePromise();
        return;
      }
      const stderr = Buffer.concat(stderrChunks).toString("utf8").trim();
      const reason = signal ? `signal ${signal}` : `exit code ${code}`;
      reject(new Error(`git clone failed with ${reason}${stderr ? `: ${stderr}` : ""}`));
    });
  });
}

function textResult(repoName: string, path: string, status: "cloned" | "existing"): ToolResult {
  return {
    content: [
      {
        type: "text",
        text: `repo=${repoName}\npath=${path}\nstatus=${status}`,
      },
    ],
  };
}

function asJsonObject(value: unknown): JsonObject {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as JsonObject) : {};
}

function writeResult(id: string | number | null, result: unknown): void {
  writeMessage({ jsonrpc: "2.0", id, result });
}

function writeError(id: string | number | null, code: number, message: string, data?: unknown): void {
  const error: JSONRPCError = { code, message };
  if (data !== undefined) {
    error.data = data;
  }
  writeMessage({ jsonrpc: "2.0", id, error });
}

function writeMessage(message: JsonObject): void {
  stdoutWrite(`${JSON.stringify(message)}\n`);
}

class RPCError extends Error {
  constructor(
    readonly code: number,
    message: string,
    readonly data?: unknown,
  ) {
    super(message);
  }
}

function toJSONRPCError(error: unknown): JSONRPCError {
  if (error instanceof RPCError) {
    return { code: error.code, message: error.message, data: error.data };
  }
  return { code: -32000, message: errorMessage(error) };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function formatLogValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (value instanceof Error) return value.stack ?? value.message;
  try {
    return JSON.stringify(value);
  } catch (error) {
    return `unformattable log value: ${errorMessage(error)}`;
  }
}
