import { ApiError } from "../api";

export function sessionStorageKey(workspaceId: string): string {
  return `agenthub:user-portal:${workspaceId}:sessionId`;
}

export function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) return `${fallback}: ${error.body || error.message}`;
  if (error instanceof Error) return `${fallback}: ${error.message}`;
  return fallback;
}

export function replaceBy<T>(items: T[], item: T, getKey: (value: T) => string): T[] {
  const key = getKey(item);
  const index = items.findIndex((candidate) => getKey(candidate) === key);
  if (index < 0) return [...items, item];
  const next = items.slice();
  next[index] = item;
  return next;
}

export function lines(value: string): string[] {
  return value.split("\n").map((line) => line.trim()).filter(Boolean);
}

export function envMap(value: string): Record<string, string> {
  return Object.fromEntries(
    lines(value).map((line) => {
      const index = line.indexOf("=");
      if (index < 0) return [line, ""];
      return [line.slice(0, index).trim(), line.slice(index + 1)];
    })
  );
}
