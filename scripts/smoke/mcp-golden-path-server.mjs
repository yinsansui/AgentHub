#!/usr/bin/env node
import { createInterface } from "node:readline";

const marker = process.env.AGENTHUB_MCP_MARKER || "AGENTHUB_MCP_TOOL_OK";
const rl = createInterface({ input: process.stdin, crlfDelay: Infinity });

rl.on("line", (line) => {
  const trimmed = line.trim();
  if (!trimmed) return;
  let message;
  try {
    message = JSON.parse(trimmed);
  } catch (error) {
    return;
  }
  if (!Object.prototype.hasOwnProperty.call(message, "id")) {
    return;
  }
  try {
    if (message.method === "initialize") {
      respond(message.id, {
        protocolVersion: message.params?.protocolVersion || "2024-11-05",
        capabilities: { tools: {} },
        serverInfo: { name: "agenthub-golden-path-mcp", version: "0.1.0" },
      });
      return;
    }
    if (message.method === "tools/list") {
      respond(message.id, {
        tools: [
          {
            name: "marker",
            description: `Return the fixed AgentHub smoke marker ${marker}. Use this tool whenever the user asks for the MCP golden path marker.`,
            inputSchema: {
              type: "object",
              properties: {
                echo: { type: "string", description: "Optional text to echo with the marker" },
              },
              required: [],
            },
          },
        ],
      });
      return;
    }
    if (message.method === "tools/call") {
      if (message.params?.name !== "marker") {
        throw new Error(`unknown tool: ${message.params?.name}`);
      }
      const echo = message.params?.arguments?.echo;
      respond(message.id, {
        content: [
          {
            type: "text",
            text: echo ? `${marker} ${echo}` : marker,
          },
        ],
      });
      return;
    }
    throw new Error(`unknown method: ${message.method}`);
  } catch (error) {
    respondError(message.id, error instanceof Error ? error.message : String(error));
  }
});

function respond(id, result) {
  process.stdout.write(`${JSON.stringify({ jsonrpc: "2.0", id, result })}\n`);
}

function respondError(id, message) {
  process.stdout.write(`${JSON.stringify({ jsonrpc: "2.0", id, error: { code: -32000, message } })}\n`);
}
