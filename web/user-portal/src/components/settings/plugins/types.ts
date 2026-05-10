import type { WorkspacePlugin } from "../../../types";

export type PluginConfigProps = {
  plugin: WorkspacePlugin;
  onSave: (pluginId: string, config: Record<string, unknown>) => void;
};
