import { useCallback, useRef, useState } from "react";
import { listWorkspaces } from "../api";
import type { WorkspaceProjection } from "../types";

export function useWorkspaceList() {
  const [workspaces, setWorkspaces] = useState<WorkspaceProjection[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const loadingRef = useRef(false);

  const loadWorkspaces = useCallback(async () => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoading(true);
    setError(null);
    try {
      const result = await listWorkspaces(100, 0);
      setWorkspaces(result.workspaces);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载 workspace 列表失败");
    } finally {
      setLoading(false);
      loadingRef.current = false;
    }
  }, []);

  return { workspaces, loading, error, loadWorkspaces };
}
