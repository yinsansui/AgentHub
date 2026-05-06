import { useCallback, useRef, useState } from "react";
import { listSessions } from "../api";
import type { SessionProjection } from "../types";

export function useSessionList(workspaceId: string) {
  const [sessionList, setSessionList] = useState<SessionProjection[]>([]);
  const [sessionListOffset, setSessionListOffset] = useState(0);
  const [sessionListHasMore, setSessionListHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const loadingRef = useRef(false);

  const loadMoreSessions = useCallback(async (reset = false) => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoading(true);
    setError(null);
    const offset = reset ? 0 : sessionListOffset;
    try {
      const result = await listSessions(workspaceId, 20, offset);
      setSessionList((prev) => reset ? result.sessions : [...prev, ...result.sessions]);
      setSessionListOffset(offset + result.sessions.length);
      setSessionListHasMore(result.sessions.length === 20);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载 session 列表失败");
    } finally {
      setLoading(false);
      loadingRef.current = false;
    }
  }, [workspaceId, sessionListOffset]);

  const resetSessions = useCallback(() => {
    setSessionList([]);
    setSessionListOffset(0);
    setSessionListHasMore(true);
    setError(null);
    loadingRef.current = false;
  }, []);

  const prependSession = useCallback((session: SessionProjection) => {
    setSessionList((prev) => [session, ...prev.filter((s) => s.sessionId !== session.sessionId)]);
  }, []);

  return { sessionList, sessionListHasMore, loading, error, loadMoreSessions, resetSessions, prependSession };
}
