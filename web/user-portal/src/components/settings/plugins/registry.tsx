import { GitBranch, Package } from "lucide-react";
import type { ComponentType, ReactNode } from "react";
import type { PluginConfigProps } from "./types";
import { RepoPluginConfig } from "./RepoPluginConfig";

const PLUGIN_ICON_MAP: Record<string, ReactNode> = {
  "git-branch": <GitBranch size={24} />,
};

const DEFAULT_PLUGIN_ICON = <Package size={24} />;

export function PluginIcon({ icon }: { icon?: string }) {
  return <>{icon ? (PLUGIN_ICON_MAP[icon] ?? DEFAULT_PLUGIN_ICON) : DEFAULT_PLUGIN_ICON}</>;
}

export const PLUGIN_CONFIG_REGISTRY: Record<string, ComponentType<PluginConfigProps>> = {
  repo: RepoPluginConfig,
};
