import { useCallback, useRef, useState } from "react";
import { listSessions } from "../api";
import type { SessionProjection } from "../types";

export function useSessionList(workspaceId: string) {
  const [sessionList, setSessionList] = useState<SessionProjection[]>([]);
  const [sessionListOffset, setSessionListOffset] = useState(0);
  const [sessionListHasMore, setSessionListHasMore] = useState(true);
  const loadingRef = useRef(false);

  const loadMoreSessions = useCallback(async (reset = false) => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    const offset = reset ? 0 : sessionListOffset;
    try {
      const result = await listSessions(workspaceId, 20, offset);
      setSessionList((prev) => reset ? result.sessions : [...prev, ...result.sessions]);
      setSessionListOffset(offset + result.sessions.length);
      setSessionListHasMore(result.sessions.length === 20);
    } catch {
      // silently ignore
    } finally {
      loadingRef.current = false;
    }
  }, [workspaceId, sessionListOffset]);

  const resetSessions = useCallback(() => {
    setSessionList([]);
    setSessionListOffset(0);
    setSessionListHasMore(true);
    loadingRef.current = false;
  }, []);

  return { sessionList, sessionListHasMore, loadMoreSessions, resetSessions };
}
