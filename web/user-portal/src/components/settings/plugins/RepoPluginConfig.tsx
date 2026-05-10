import { Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import type { RepoEntry, WorkspacePlugin } from "../../../types";
import type { PluginConfigProps } from "./types";

export function repoPluginInitialRepos(plugin: WorkspacePlugin): RepoEntry[] {
  const repos = (plugin.config as { repositories?: RepoEntry[] } | undefined)?.repositories;
  return repos && repos.length > 0 ? repos : [{ repoName: "", repoUrl: "" }];
}

export function RepoPluginConfig({ plugin, onSave }: PluginConfigProps) {
  const [repositories, setRepositories] = useState<RepoEntry[]>(() => repoPluginInitialRepos(plugin));

  useEffect(() => {
    setRepositories(repoPluginInitialRepos(plugin));
  }, [plugin.id, plugin.config]);

  function updateRow(index: number, field: keyof RepoEntry, value: string) {
    setRepositories((prev) => prev.map((r, i) => i === index ? { ...r, [field]: value } : r));
  }

  function addRow() {
    setRepositories((prev) => [...prev, { repoName: "", repoUrl: "" }]);
  }

  function removeRow(index: number) {
    setRepositories((prev) => {
      const next = prev.filter((_, i) => i !== index);
      return next.length > 0 ? next : [{ repoName: "", repoUrl: "" }];
    });
  }

  const canSave = repositories.length > 0 && repositories.every((r) => r.repoName.trim() && r.repoUrl.trim());

  function handleSubmit(event: { preventDefault: () => void }) {
    event.preventDefault();
    onSave(plugin.id, {
      repositories: repositories.map((r) => ({ repoName: r.repoName.trim(), repoUrl: r.repoUrl.trim() }))
    });
  }

  return (
    <form className="settings-form" onSubmit={handleSubmit}>
      <div className="plugin-config-list settings-field-span">
        {repositories.map((row, i) => (
          <div key={i} className="plugin-config-row">
            <input
              value={row.repoName}
              onChange={(e) => updateRow(i, "repoName", e.target.value)}
              placeholder="目录名，例如：my-repo"
              aria-label={`仓库 ${i + 1} 目录名`}
            />
            <input
              value={row.repoUrl}
              onChange={(e) => updateRow(i, "repoUrl", e.target.value)}
              placeholder="Clone URL，例如：https://github.com/org/repo.git"
              aria-label={`仓库 ${i + 1} Clone URL`}
            />
            <button type="button" className="settings-delete-button" aria-label={`移除仓库 ${i + 1}`} onClick={() => removeRow(i)}>
              <Trash2 size={14} />
            </button>
          </div>
        ))}
        <button type="button" className="plugin-config-add-btn" onClick={addRow}>
          <Plus size={13} />添加仓库
        </button>
      </div>
      <div className="settings-form-footer">
        <span>配置仅对新 session 生效。Repo 工具仅负责 clone；clone 后 agent 直接读取文件。</span>
        <button type="submit" className="settings-primary-button" disabled={!canSave}>
          <Save size={14} />{plugin.installed ? "保存配置" : "安装"}
        </button>
      </div>
    </form>
  );
}
