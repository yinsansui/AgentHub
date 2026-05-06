import { useCallback, useRef, useState } from "react";
import { ApiError, createSession, createTurn, getSessionEvents, getSessionState, interruptRun } from "../api";
import { applyEvent } from "../events";
import { sessionStorageKey } from "../lib/utils";
import type { UniversalEvent, WorkbenchState } from "../types";
import type { FormEvent } from "react";

const SSE_EVENTS = [
  "run.started", "run.cancelling", "run.completed", "run.failed", "run.cancelled", "run.timed_out",
  "message.started", "message.completed",
  "text.started", "text.delta", "text.completed",
  "thinking.started", "thinking.delta", "thinking.completed",
  "tool_call.started", "tool_call.delta", "tool_call.completed",
  "error", "session.started", "session.ended"
];

export function useChat(
  workspaceId: string,
  onError: (error: unknown, fallback: string) => void,
  onSessionCreated?: (session: { sessionId: string; taskId: string; workspaceId: string; modelId?: string; title?: string }) => void
) {
  const [workbench, setWorkbench] = useState<WorkbenchState>({
    workspaceId,
    messages: [],
    activeRun: null,
    latestEventId: 0,
    streamStatus: "idle"
  });
  const [messageDraft, setMessageDraft] = useState("");
  const [selectedModelId, setSelectedModelId] = useState("");

  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<number | null>(null);
  const latestEventIdRef = useRef(0);

  const disconnectStream = useCallback(() => {
    eventSourceRef.current?.close();
    eventSourceRef.current = null;
    if (reconnectTimerRef.current) {
      window.clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const connectStream = useCallback(
    (sessionId: string, after: number) => {
      disconnectStream();
      setWorkbench((c) => ({ ...c, streamStatus: "connecting" }));
      const source = new EventSource(`/sessions/${encodeURIComponent(sessionId)}/stream?after=${after}`);
      eventSourceRef.current = source;
      source.onopen = () => setWorkbench((c) => ({ ...c, streamStatus: "open" }));
      source.onerror = () => {
        source.close();
        if (eventSourceRef.current === source) eventSourceRef.current = null;
        setWorkbench((c) => ({ ...c, streamStatus: "reconnecting" }));
        reconnectTimerRef.current = window.setTimeout(async () => {
          try {
            const cursor = latestEventIdRef.current;
            const replay = await getSessionEvents(sessionId, cursor);
            setWorkbench((c) => {
              const replayed = replay.events.reduce((next, e) => applyEvent(next, e.payload, e.id), c);
              latestEventIdRef.current = replay.nextCursor;
              return { ...replayed, latestEventId: replay.nextCursor };
            });
            connectStream(sessionId, replay.nextCursor);
          } catch (error) {
            setWorkbench((c) => ({ ...c, streamStatus: "error" }));
            onError(error, "SSE 重连失败");
          }
        }, 1200);
      };
      const handleMessage = (raw: MessageEvent<string>) => {
        try {
          const event = JSON.parse(raw.data) as UniversalEvent;
          const eventId = Number(raw.lastEventId || 0);
          setWorkbench((c) => {
            const next = applyEvent(c, event, eventId || undefined);
            latestEventIdRef.current = next.latestEventId;
            return next;
          });
        } catch (error) {
          onError(error, "解析事件失败");
        }
      };
      SSE_EVENTS.forEach((type) => source.addEventListener(type, handleMessage as EventListener));
      source.onmessage = handleMessage;
    },
    [disconnectStream, onError]
  );

  const loadSession = useCallback(
    async (sessionId: string) => {
      if (!sessionId.trim()) return;
      try {
        disconnectStream();
        setWorkbench((c) => ({ ...c, streamStatus: "connecting" }));
        const state = await getSessionState(sessionId.trim());
        const next: WorkbenchState = {
          workspaceId,
          sessionId: state.sessionId,
          taskId: state.activeRun?.taskId,
          modelId: undefined,
          messages: state.messages ?? [],
          activeRun: state.activeRun ?? null,
          latestEventId: state.latestEventId ?? 0,
          streamStatus: "connecting"
        };
        latestEventIdRef.current = next.latestEventId;
        setWorkbench(next);
        sessionStorage.setItem(sessionStorageKey(workspaceId), state.sessionId);
        const replay = await getSessionEvents(state.sessionId, next.latestEventId);
        const replayed = replay.events.reduce((c, e) => applyEvent(c, e.payload, e.id), next);
        latestEventIdRef.current = replay.nextCursor;
        setWorkbench({ ...replayed, latestEventId: replay.nextCursor, streamStatus: "connecting" });
        connectStream(state.sessionId, replay.nextCursor);
      } catch (error) {
        setWorkbench((c) => ({ ...c, streamStatus: "error" }));
        onError(error, "加载 session 失败");
      }
    },
    [connectStream, disconnectStream, onError, workspaceId]
  );

  function resetWorkbench() {
    disconnectStream();
    setWorkbench({
      workspaceId,
      messages: [],
      activeRun: null,
      latestEventId: 0,
      streamStatus: "idle"
    });
  }

  async function handleCreateOrTurn(event: FormEvent) {
    event.preventDefault();
    const message = messageDraft.trim();
    if (!message || workbench.activeRun) return;
    try {
      if (!workbench.sessionId) {
        const response = await createSession(workspaceId, {
          modelId: selectedModelId || undefined,
          firstTurn: { message, source: "user" }
        });
        setMessageDraft("");
        setWorkbench((c) => ({
          ...c,
          workspaceId,
          taskId: response.task.taskId,
          sessionId: response.session.sessionId,
          modelId: response.session.modelId,
          activeRun: response.run ?? null
        }));
        sessionStorage.setItem(sessionStorageKey(workspaceId), response.session.sessionId);
        onSessionCreated?.(response.session);
        await loadSession(response.session.sessionId);
      } else {
        const response = await createTurn(workbench.sessionId, message);
        setMessageDraft("");
        setWorkbench((c) => ({ ...c, activeRun: response.run, taskId: response.session.taskId, modelId: response.session.modelId }));
        await loadSession(response.session.sessionId);
      }
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        onError(null, "当前 workspace 未启动或已有 active run，请刷新状态后重试");
      } else {
        onError(error, "发送消息失败");
      }
    }
  }

  async function handleStop() {
    const activeRun = workbench.activeRun;
    if (!workbench.sessionId || !activeRun?.runId || activeRun.status === "cancelling") return;
    try {
      const result = await interruptRun(workbench.sessionId, activeRun.runId);
      if (result.interrupted && result.run) {
        setWorkbench((c) => ({ ...c, activeRun: result.run ?? c.activeRun }));
      } else {
        await loadSession(workbench.sessionId);
      }
      if (result.cancelError) {
        onError(null, `取消请求未完全送达：${result.cancelError}`);
      }
    } catch (error) {
      if (error instanceof ApiError && error.status === 409 && workbench.sessionId) {
        await loadSession(workbench.sessionId);
      } else {
        onError(error, "停止 run 失败");
      }
    }
  }

  return {
    workbench, setWorkbench,
    messageDraft, setMessageDraft,
    selectedModelId, setSelectedModelId,
    disconnectStream, loadSession, resetWorkbench,
    handleCreateOrTurn, handleStop
  };
}
