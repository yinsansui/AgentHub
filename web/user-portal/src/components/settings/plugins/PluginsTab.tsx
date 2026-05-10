import { useState } from "react";
import type { WorkspacePlugin } from "../../../types";
import { PageShell, SectionCard } from "../layout";
import { PLUGIN_CONFIG_REGISTRY, PluginIcon } from "./registry";

export function PluginsTab(props: {
  plugins: WorkspacePlugin[];
  onSavePlugin: (pluginId: string, config: Record<string, unknown>) => void;
}) {
  const { plugins, onSavePlugin } = props;
  const [detailPluginId, setDetailPluginId] = useState<string | null>(null);

  const detailPlugin = detailPluginId ? plugins.find((p) => p.id === detailPluginId) : null;

  if (detailPlugin) {
    const ConfigComponent = PLUGIN_CONFIG_REGISTRY[detailPlugin.id];
    return (
      <PageShell>
        <SectionCard
          title={detailPlugin.name}
          description={detailPlugin.description}
          actions={
            <button type="button" className="settings-back-button" onClick={() => setDetailPluginId(null)}>
              ← 返回插件列表
            </button>
          }
        >
          <div className="settings-plugin-status">
            <span className={`settings-plugin-badge ${detailPlugin.installed ? "installed" : "not-installed"}`}>
              {detailPlugin.installed ? "已安装" : "未安装"}
            </span>
            {detailPlugin.installed && detailPlugin.updatedAt && (
              <span className="settings-plugin-updated">
                上次更新：{new Date(detailPlugin.updatedAt).toLocaleString("zh-CN")}
              </span>
            )}
          </div>
          {ConfigComponent
            ? <ConfigComponent plugin={detailPlugin} onSave={onSavePlugin} />
            : <p className="empty-copy">该插件暂无可配置项。</p>
          }
        </SectionCard>
      </PageShell>
    );
  }

  return (
    <PageShell>
      <SectionCard title="插件" description="点击插件卡片查看配置。">
        <div className="plugin-grid">
          {plugins.map((plugin) => (
            <button
              key={plugin.id}
              type="button"
              className="plugin-card"
              onClick={() => setDetailPluginId(plugin.id)}
            >
              <div className="plugin-card-icon">
                <PluginIcon icon={plugin.icon} />
              </div>
              <span className="plugin-card-name">{plugin.name}</span>
              <span className={`settings-plugin-badge ${plugin.installed ? "installed" : "not-installed"}`}>
                {plugin.installed ? "已安装" : "未安装"}
              </span>
            </button>
          ))}
          {plugins.length === 0 && <p className="empty-copy">暂无可用插件。</p>}
        </div>
      </SectionCard>
    </PageShell>
  );
}
