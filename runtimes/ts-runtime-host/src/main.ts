import { createInterface } from "node:readline";
import { PiCodingAgentAdapter, type RuntimeAdapter } from "./adapters/pi-coding-agent.js";
import type { RuntimeCommand, UniversalEvent } from "./protocol.js";

const stdoutWrite = process.stdout.write.bind(process.stdout);
const stderrWrite = process.stderr.write.bind(process.stderr);

for (const level of ["log", "info", "debug", "warn", "error"] as const) {
  console[level] = (...args: unknown[]) => {
    stderrWrite(`${args.map(formatLogValue).join(" ")}\n`);
  };
}

const adapter = createAdapter(process.argv.slice(2));
let runQueue: Promise<void> = Promise.resolve();
let shutdownRequested = false;

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on("line", (line) => {
  const trimmed = line.trim();
  if (!trimmed) return;
  let command: RuntimeCommand;
  try {
    command = JSON.parse(trimmed) as RuntimeCommand;
  } catch (error) {
    emit({ type: "error", timestamp: new Date().toISOString(), error: { message: `invalid command json: ${errorMessage(error)}` } });
    return;
  }
  if (command.type === "abort") {
    void adapter.abort(command.runId).catch((error) => emit({ type: "error", timestamp: new Date().toISOString(), runId: command.runId, error: { message: errorMessage(error) } }));
    return;
  }
  if (command.type === "shutdown") {
    requestShutdown();
    return;
  }
  if (command.type === "run") {
    runQueue = runQueue.then(() => adapter.run(command, emit)).catch((error) => {
      emit({
        type: "error",
        timestamp: new Date().toISOString(),
        workspaceId: command.workspaceId,
        taskId: command.taskId,
        sessionId: command.sessionId,
        runId: command.runId,
        error: { message: errorMessage(error) },
      });
    });
    return;
  }
  emit({ type: "error", timestamp: new Date().toISOString(), error: { message: `unknown command type: ${(command as { type?: string }).type}` } });
});

rl.on("close", () => {
  requestShutdown();
});

process.on("SIGTERM", () => {
  void adapter.shutdown().finally(() => process.exit(0));
});
process.on("SIGINT", () => {
  requestShutdown();
});

function requestShutdown(): void {
  if (shutdownRequested) return;
  shutdownRequested = true;
  runQueue = runQueue.finally(() => adapter.shutdown()).finally(() => process.exit(0));
}

function createAdapter(args: string[]): RuntimeAdapter {
  const adapterIndex = args.indexOf("--adapter");
  const adapterName = adapterIndex >= 0 ? args[adapterIndex + 1] : "pi-coding-agent";
  if (adapterName !== "pi-coding-agent") {
    throw new Error(`unsupported runtime adapter: ${adapterName}`);
  }
  return new PiCodingAgentAdapter();
}

function emit(event: UniversalEvent): void {
  stdoutWrite(`${JSON.stringify(event)}\n`);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function formatLogValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (value instanceof Error) return value.stack ?? value.message;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}
